package sftp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/sftp"
)

// Opener 用系统默认程序打开本地文件（由 main 注入，internal/* 不感知平台）
type Opener interface {
	OpenFile(path string) error
}

// EditInfo 编辑会话的对外快照（前端列表/冲突提示用）
type EditInfo struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
	Name       string `json:"name"`
	Conflict   bool   `json:"conflict"` // 上传时检测到远程已被他处修改
	UploadedAt int64  `json:"uploadedAt"`
}

// editSession 内部状态：本地临时文件 + 远程指纹 + 监听器
type editSession struct {
	EditInfo
	remoteMtime time.Time
	remoteSize  int64

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	debounce *time.Timer
	closed   bool
}

// Editor 管理"远程编辑"会话：下载到临时目录 → 系统编辑器打开 →
// 本地保存后自动回传（上传前校验远程指纹，发现他处修改则挂起等待用户决定）。
type Editor struct {
	mu      sync.Mutex
	edits   map[string]*editSession
	src     ClientSource
	em      Emitter
	opener  Opener
	tempDir string
	wg      sync.WaitGroup
}

// NewEditor 创建编辑器管理器；tempDir 为空时用系统临时目录
func NewEditor(src ClientSource, em Emitter, opener Opener, tempDir string) *Editor {
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "panoramix-edit")
	}
	return &Editor{
		edits:   map[string]*editSession{},
		src:     src,
		em:      em,
		opener:  opener,
		tempDir: tempDir,
	}
}

// StartEdit 下载远程文件到临时目录并用系统编辑器打开，返回编辑会话信息
func (e *Editor) StartEdit(sessionID, remotePath string) (EditInfo, error) {
	cli, err := NewSftpClient(e.src, sessionID)
	if err != nil {
		return EditInfo{}, fmt.Errorf("sftp open: %w", err)
	}
	defer closeWithTimeout(cli)

	rfi, err := cli.Stat(remotePath)
	if err != nil {
		return EditInfo{}, fmt.Errorf("stat remote: %w", err)
	}
	if rfi.IsDir() {
		return EditInfo{}, fmt.Errorf("cannot edit a directory")
	}
	if rfi.Size() > 16*1024*1024 {
		return EditInfo{}, fmt.Errorf("file too large to edit (max 16MB)")
	}

	id := randHex(8)
	dir := filepath.Join(e.tempDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return EditInfo{}, fmt.Errorf("create temp dir: %w", err)
	}
	localPath := filepath.Join(dir, path.Base(remotePath))

	if err := downloadFile(cli, remotePath, localPath); err != nil {
		return EditInfo{}, err
	}

	s := &editSession{
		EditInfo: EditInfo{
			ID:         id,
			SessionID:  sessionID,
			RemotePath: remotePath,
			LocalPath:  localPath,
			Name:       path.Base(remotePath),
		},
		remoteMtime: rfi.ModTime(),
		remoteSize:  rfi.Size(),
	}

	w, werr := fsnotify.NewWatcher()
	if werr == nil {
		if err := w.Add(localPath); err == nil {
			s.watcher = w
			e.wg.Add(1)
			go e.watchLoop(s)
		} else {
			_ = w.Close()
		}
	}

	e.mu.Lock()
	e.edits[id] = s
	e.mu.Unlock()

	if e.opener != nil {
		if err := e.opener.OpenFile(localPath); err != nil {
			log.Printf("[editor] open file failed: %v", err)
			// 打开失败不阻断：用户可自行用编辑器打开 localPath
		}
	}
	log.Printf("[editor] edit session started: %s -> %s", remotePath, localPath)
	return s.EditInfo, nil
}

// watchLoop 监听本地保存事件（编辑器写文件常产生多次事件，500ms 去抖）
func (e *Editor) watchLoop(s *editSession) {
	defer e.wg.Done()
	for {
		select {
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			s.mu.Lock()
			if s.debounce != nil {
				s.debounce.Stop()
			}
			s.debounce = time.AfterFunc(600*time.Millisecond, func() {
				e.upload(s)
			})
			s.mu.Unlock()
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[editor] watcher error: %v", err)
		}
	}
}

// upload 把本地内容回传远程；先做指纹校验，冲突则挂起等 ResolveConflict
func (e *Editor) upload(s *editSession) {
	cli, err := NewSftpClient(e.src, s.SessionID)
	if err != nil {
		e.emitState(s, "error", err.Error())
		return
	}
	defer closeWithTimeout(cli)

	// 冲突检测：远程 mtime/size 与打开时不同 → 他处已修改
	if rfi, serr := cli.Stat(s.RemotePath); serr == nil {
		if !rfi.ModTime().Equal(s.remoteMtime) || rfi.Size() != s.remoteSize {
			s.mu.Lock()
			s.Conflict = true
			s.mu.Unlock()
			e.emitState(s, "conflict", "remote file changed since opened")
			return
		}
	}

	if err := uploadFile(cli, s.LocalPath, s.RemotePath); err != nil {
		e.emitState(s, "error", err.Error())
		return
	}
	// 更新指纹（避免下次保存误判冲突）
	if rfi, serr := cli.Stat(s.RemotePath); serr == nil {
		s.mu.Lock()
		s.remoteMtime = rfi.ModTime()
		s.remoteSize = rfi.Size()
		s.UploadedAt = time.Now().UnixMilli()
		s.Conflict = false
		s.mu.Unlock()
	}
	e.emitState(s, "uploaded", "")
	log.Printf("[editor] uploaded: %s", s.RemotePath)
}

// ResolveConflict 冲突处理：overwrite=true 强制以本地内容覆盖远程；
// false 放弃本次本地修改（重新下载远程版本到本地）
func (e *Editor) ResolveConflict(editID string, overwrite bool) error {
	e.mu.Lock()
	s := e.edits[editID]
	e.mu.Unlock()
	if s == nil {
		return fmt.Errorf("edit session not found: %s", editID)
	}
	cli, err := NewSftpClient(e.src, s.SessionID)
	if err != nil {
		return fmt.Errorf("sftp open: %w", err)
	}
	defer closeWithTimeout(cli)

	if overwrite {
		if err := uploadFile(cli, s.LocalPath, s.RemotePath); err != nil {
			return fmt.Errorf("overwrite remote: %w", err)
		}
	} else {
		if err := downloadFile(cli, s.RemotePath, s.LocalPath); err != nil {
			return fmt.Errorf("reload remote: %w", err)
		}
	}
	if rfi, serr := cli.Stat(s.RemotePath); serr == nil {
		s.mu.Lock()
		s.remoteMtime = rfi.ModTime()
		s.remoteSize = rfi.Size()
		s.Conflict = false
		s.UploadedAt = time.Now().UnixMilli()
		s.mu.Unlock()
	}
	e.emitState(s, "resolved", "")
	return nil
}

// FinishEdit 结束编辑会话（关闭监听、删除临时文件）
func (e *Editor) FinishEdit(editID string) error {
	e.mu.Lock()
	s := e.edits[editID]
	delete(e.edits, editID)
	e.mu.Unlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.watcher != nil {
		_ = s.watcher.Close()
	}
	if s.debounce != nil {
		s.debounce.Stop()
	}
	if !s.closed {
		s.closed = true
	}
	s.mu.Unlock()
	_ = os.RemoveAll(filepath.Dir(s.LocalPath))
	return nil
}

// ListEdits 返回全部编辑会话快照
func (e *Editor) ListEdits() []EditInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]EditInfo, 0, len(e.edits))
	for _, s := range e.edits {
		s.mu.Lock()
		out = append(out, s.EditInfo)
		s.mu.Unlock()
	}
	return out
}

// Close 关闭全部编辑会话（应用退出时）
func (e *Editor) Close() {
	e.mu.Lock()
	ids := make([]string, 0, len(e.edits))
	for id := range e.edits {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	for _, id := range ids {
		_ = e.FinishEdit(id)
	}
}

func (e *Editor) emitState(s *editSession, state, message string) {
	s.mu.Lock()
	info := s.EditInfo
	s.mu.Unlock()
	e.em.Emit("edit:state", map[string]interface{}{
		"id": info.ID, "sessionId": info.SessionID,
		"remotePath": info.RemotePath, "name": info.Name,
		"localPath": info.LocalPath, "state": state, "message": message,
	})
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("sftp: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// downloadFile 拉取单个远程文件到本地（覆盖）
func downloadFile(cli *sftp.Client, remotePath, localPath string) error {
	rf, err := cli.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote: %w", err)
	}
	defer rf.Close()
	lf, err := os.OpenFile(localPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create local: %w", err)
	}
	defer lf.Close()
	if _, err := rf.WriteTo(lf); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}

// uploadFile 推送单个本地文件覆盖远程
func uploadFile(cli *sftp.Client, localPath, remotePath string) error {
	if err := sftpMkdirAll(cli, path.Dir(remotePath)); err != nil {
		return fmt.Errorf("mkdir remote: %w", err)
	}
	lf, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local: %w", err)
	}
	defer lf.Close()
	rf, err := cli.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	defer rf.Close()
	if _, err := lf.WriteTo(rf); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return rf.Close()
}
