package sftp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// 传输状态（transfer:progress 事件负载，与前端 stores/transfers 保持一致）
const (
	StatePending        = "pending"
	StateRunning        = "running"
	StateDone           = "done"
	StateSkipped        = "skipped"
	StateCanceled       = "canceled"
	StateFailed         = "failed"
	StateAwaitingPolicy = "awaiting_policy"
)

// 覆盖策略
const (
	PolicyOverwrite = "overwrite"
	PolicySkip      = "skip"
	PolicyRename    = "rename"
)

// 方向
const (
	DirUpload   = "upload"
	DirDownload = "download"
)

var errCanceled = errors.New("transfer canceled")

// Task 传输任务的对外数据载荷（transfer:progress 事件负载 / 快照）。
// 纯数据、可安全拷贝；内部运行态在 taskHandle 中。
type Task struct {
	ID          string  `json:"transferId"`
	BatchID     string  `json:"batchId"`
	SessionID   string  `json:"sessionId"`
	Direction   string  `json:"direction"`
	Name        string  `json:"name"`
	RemotePath  string  `json:"remotePath"`
	LocalPath   string  `json:"localPath"`
	Size        int64   `json:"size"`
	Transferred int64   `json:"transferred"`
	State       string  `json:"state"`
	SpeedBps    float64 `json:"speedBps"`
	Resumed     bool    `json:"resumed,omitempty"` // 从断点续传
	Err         string  `json:"err,omitempty"`
}

// taskHandle 内部运行态：数据载荷 + 锁与控制通道
type taskHandle struct {
	Task
	mu            sync.Mutex
	cancel        chan struct{}
	cancelOnce    sync.Once
	policyCh      chan string
	targetExisted bool
	lastEmit      time.Time
}

func (h *taskHandle) snapshot() Task {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Task
}

func (h *taskHandle) canceled() bool {
	select {
	case <-h.cancel:
		return true
	default:
		return false
	}
}

type spec struct {
	remote string
	local  string
	size   int64
}

// Queue 传输队列：并发默认 3，按入队顺序调度；
// 目录在入队时递归展开为逐文件任务；冲突时任务挂起等待前端策略。
type Queue struct {
	mu    sync.Mutex
	tasks map[string]*taskHandle
	order []string
	sem   chan struct{}
	src   ClientSource
	em    Emitter
	wg    sync.WaitGroup

	batchMu     sync.Mutex
	batchPolicy map[string]string // batchID → "全部应用"记住的策略
}

func NewQueue(src ClientSource, em Emitter) *Queue {
	return &Queue{
		tasks:       make(map[string]*taskHandle),
		sem:         make(chan struct{}, 3),
		src:         src,
		em:          em,
		batchPolicy: make(map[string]string),
	}
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("sftp: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Enqueue 把一个本地路径或远程路径展开为任务入队（目录递归）。
// upload: remotePath 为目标目录、localPath 为本地文件/目录；
// download: remotePath 为远程文件/目录、localPath 为本地目标目录或"另存为"完整路径。
// 返回 batchID（同一次入队产生的任务共享，用于"全部应用"策略）。
func (q *Queue) Enqueue(sessionID, direction, remotePath, localPath string) (string, error) {
	cli, err := NewSftpClient(q.src, sessionID)
	if err != nil {
		return "", err
	}
	defer closeWithTimeout(cli)

	batchID := newID()
	var specs []spec
	switch direction {
	case DirUpload:
		specs, err = walkLocal(localPath, remotePath)
	case DirDownload:
		specs, err = walkRemote(cli, remotePath, localPath)
	default:
		err = fmt.Errorf("unknown direction: %s", direction)
	}
	if err != nil {
		return "", err
	}
	if len(specs) == 0 {
		return "", fmt.Errorf("no files to transfer")
	}

	q.mu.Lock()
	for _, sp := range specs {
		h := &taskHandle{
			Task: Task{
				ID:         newID(),
				BatchID:    batchID,
				SessionID:  sessionID,
				Direction:  direction,
				Name:       filepath.Base(sp.local),
				RemotePath: sp.remote,
				LocalPath:  sp.local,
				Size:       sp.size,
				State:      StatePending,
			},
			cancel:   make(chan struct{}),
			policyCh: make(chan string, 1),
		}
		q.tasks[h.ID] = h
		q.order = append(q.order, h.ID)
	}
	q.mu.Unlock()
	q.pump()
	return batchID, nil
}

// walkLocal 本地 → 远程 目标清单（目录递归展开，目录在传输时按需创建）
func walkLocal(root, remoteDir string) ([]spec, error) {
	root = filepath.Clean(root)
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	var specs []spec
	remoteRoot := path.Join(remoteDir, path.Base(filepath.ToSlash(root)))
	werr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return nil // 目录不产生任务，传输时按需创建
		}
		// 非目录即文件——包括 root 本身就是单个文件的情况（rel == "."）
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		specs = append(specs, spec{
			remote: path.Join(remoteRoot, rel),
			local:  p,
			size:   info.Size(),
		})
		return nil
	})
	return specs, werr
}

// walkRemote 远程 → 本地 目标清单（目录递归展开）
func walkRemote(cli *sftp.Client, root, localPath string) ([]spec, error) {
	fi, err := cli.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	var specs []spec
	var walk func(rp, lp string) error
	walk = func(rp, lp string) error {
		rfi, err := cli.Stat(rp)
		if err != nil {
			return err
		}
		if !rfi.IsDir() {
			specs = append(specs, spec{remote: rp, local: lp, size: rfi.Size()})
			return nil
		}
		entries, err := cli.ReadDir(rp)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := walk(path.Join(rp, e.Name()), filepath.Join(lp, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if fi.IsDir() {
		err = walk(root, filepath.Join(localPath, path.Base(root)))
		return specs, err
	}
	// 单文件：localPath 已是目录 → 按目录语义补文件名；
	// 否则视为"另存为"的完整文件路径（支持自定义文件名）
	if st, serr := os.Stat(localPath); serr == nil && st.IsDir() {
		specs = append(specs, spec{
			remote: root,
			local:  filepath.Join(localPath, path.Base(root)),
			size:   fi.Size(),
		})
	} else {
		specs = append(specs, spec{remote: root, local: localPath, size: fi.Size()})
	}
	return specs, err
}

// Tasks 返回任务快照（列表填充/测试用）
func (q *Queue) Tasks() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Task, 0, len(q.order))
	for _, id := range q.order {
		if h, ok := q.tasks[id]; ok {
			out = append(out, h.snapshot())
		}
	}
	return out
}

// closeWithTimeout 关闭 SFTP 客户端。
// pkg/sftp 的 Close 会等对端关闭通道；对端异常时不响应会导致永久阻塞，
// 这里用超时兜底（放弃等待，泄漏的 goroutine 会在会话关闭时自然结束）。
func closeWithTimeout(cli *sftp.Client) {
	done := make(chan struct{})
	go func() {
		_ = cli.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// pump 调度：并发未满且有 pending 任务就启动 worker。
// 选中任务时必须在同一次加锁内同步置为 running——否则 goroutine 尚未
// 启动、循环里它仍是 pending，会被重复调度（同一任务并发跑两份）。
func (q *Queue) pump() {
	for {
		q.mu.Lock()
		running := 0
		var next *taskHandle
		for _, id := range q.order {
			h := q.tasks[id]
			if h == nil {
				continue
			}
			h.mu.Lock()
			st := h.State
			h.mu.Unlock()
			if st == StateRunning || st == StateAwaitingPolicy {
				running++
			} else if st == StatePending && next == nil {
				next = h
			}
		}
		if next != nil && running < cap(q.sem) {
			next.mu.Lock()
			next.State = StateRunning
			next.mu.Unlock()
			running++
		} else {
			next = nil
		}
		q.mu.Unlock()
		if next == nil {
			return
		}
		q.wg.Add(1)
		go func(h *taskHandle) {
			q.sem <- struct{}{}
			defer func() { <-q.sem; q.wg.Done() }()
			q.runTask(h)
		}(next)
	}
}

func (q *Queue) runTask(h *taskHandle) {
	h.mu.Lock()
	h.State = StateRunning
	h.mu.Unlock()
	q.emit(h)

	err := q.execute(h)

	h.mu.Lock()
	switch {
	case h.State == StateCanceled || h.State == StateSkipped:
		// execute 内部已定态（策略跳过/取消清理路径）
	case errors.Is(err, errCanceled):
		h.State = StateCanceled
	case err != nil:
		h.State = StateFailed
		h.Err = err.Error()
	default:
		h.State = StateDone
	}
	h.mu.Unlock()
	q.emit(h)
	q.pump()
}

// execute 单文件传输全流程：冲突策略 → 建流 → 带取消的拷贝 → 校验与清理
func (q *Queue) execute(h *taskHandle) error {
	cli, err := NewSftpClient(q.src, h.SessionID)
	if err != nil {
		return err
	}
	defer cli.Close()

	h.mu.Lock()
	direction := h.Direction
	localPath := h.LocalPath
	remotePath := h.RemotePath
	want := h.Size
	h.mu.Unlock()

	remoteExists := func() bool {
		fi, serr := cli.Stat(remotePath)
		return serr == nil && !fi.IsDir()
	}
	localExists := func() bool {
		_, serr := os.Stat(localPath)
		return serr == nil
	}
	existed := false
	if direction == DirDownload {
		existed = localExists()
	} else {
		existed = remoteExists()
	}
	h.mu.Lock()
	h.targetExisted = existed
	h.mu.Unlock()

	// ---- 断点续传检测（无显式批次策略时）----
	// 目标存在且比源小 → 视为断点续传（按 size 续，业界通行做法）；
	// 等大或更大 → 进冲突流程由用户决策（内容可能不同，静默跳过有丢数据风险）。
	resumeFrom := int64(0)
	if existed && q.policyFor(h.BatchID) == "" && want > 0 {
		var targetSize int64
		if direction == DirDownload {
			if fi, serr := os.Stat(localPath); serr == nil {
				targetSize = fi.Size()
			}
		} else {
			if fi, serr := cli.Stat(remotePath); serr == nil && !fi.IsDir() {
				targetSize = fi.Size()
			}
		}
		if targetSize > 0 && targetSize < want {
			resumeFrom = targetSize
			existed = false // 视为新传输（不进冲突流程、取消时不清理目标）
			h.mu.Lock()
			h.Resumed = true
			h.mu.Unlock()
		}
	}

	// ---- 冲突策略 ----
	if existed {
		policy := q.policyFor(h.BatchID)
		if policy == "" {
			h.mu.Lock()
			h.State = StateAwaitingPolicy
			h.mu.Unlock()
			q.emit(h)
			q.em.Emit("transfer:conflict", map[string]interface{}{
				"transferId": h.ID, "batchId": h.BatchID,
				"sessionId": h.SessionID, "direction": direction,
				"name": h.Name, "path": targetPath(direction, localPath, remotePath),
				"size": want,
			})
			select {
			case p := <-h.policyCh:
				policy = p
			case <-h.cancel:
				h.mu.Lock()
				h.State = StateCanceled
				h.mu.Unlock()
				return errCanceled
			}
			h.mu.Lock()
			h.State = StateRunning
			h.mu.Unlock()
			if direction == DirDownload {
				existed = localExists()
			} else {
				existed = remoteExists()
			}
		}
		switch policy {
		case PolicySkip:
			if existed {
				h.mu.Lock()
				h.State = StateSkipped
				h.mu.Unlock()
				return nil
			}
		case PolicyRename:
			if existed {
				if direction == DirDownload {
					newPath := renameLocalPath(localPath)
					h.mu.Lock()
					h.LocalPath = newPath
					localPath = newPath
					h.mu.Unlock()
				} else {
					newPath := renameRemotePath(cli, remotePath)
					h.mu.Lock()
					h.RemotePath = newPath
					remotePath = newPath
					h.mu.Unlock()
				}
				existed = false // 新目标不存在：取消时可安全清理半成品
			}
		}
	}

	// ---- 建流与拷贝 ----
	if direction == DirDownload {
		if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
			return fmt.Errorf("mkdir local: %w", err)
		}
		rf, err := cli.Open(remotePath)
		if err != nil {
			return fmt.Errorf("open remote: %w", err)
		}
		defer rf.Close()
		lfFlags := os.O_CREATE | os.O_WRONLY
		if resumeFrom == 0 {
			lfFlags |= os.O_TRUNC
		}
		lf, err := os.OpenFile(localPath, lfFlags, 0o600)
		if err != nil {
			return fmt.Errorf("create local: %w", err)
		}
		if resumeFrom > 0 {
			// 双端都 Seek 到断点（本地文件与远程流）
			if _, serr := lf.Seek(resumeFrom, io.SeekStart); serr != nil {
				return fmt.Errorf("seek local: %w", serr)
			}
			if _, serr := rf.Seek(resumeFrom, io.SeekStart); serr != nil {
				return fmt.Errorf("seek remote: %w", serr)
			}
			h.mu.Lock()
			h.Transferred = resumeFrom
			h.mu.Unlock()
		}
		transferred, cerr := q.copyWithProgress(h, rf, lf)
		_ = lf.Close()
		return finishTransfer(h, direction, localPath, remotePath, existed, cli, resumeFrom, transferred, cerr)
	}

	if err := sftpMkdirAll(cli, path.Dir(remotePath)); err != nil {
		return fmt.Errorf("mkdir remote: %w", err)
	}
	lf, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local: %w", err)
	}
	defer lf.Close()
	rfFlags := os.O_WRONLY | os.O_CREATE
	if resumeFrom == 0 {
		rfFlags |= os.O_TRUNC
	}
	rf, err := cli.OpenFile(remotePath, rfFlags)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	defer rf.Close()
	if resumeFrom > 0 {
		if _, serr := lf.Seek(resumeFrom, io.SeekStart); serr != nil {
			return fmt.Errorf("seek local: %w", serr)
		}
		if _, serr := rf.Seek(resumeFrom, io.SeekStart); serr != nil {
			return fmt.Errorf("seek remote: %w", serr)
		}
		h.mu.Lock()
		h.Transferred = resumeFrom
		h.mu.Unlock()
	}
	transferred, cerr := q.copyWithProgress(h, lf, rf)
	_ = rf.Close()
	return finishTransfer(h, direction, localPath, remotePath, existed, cli, resumeFrom, transferred, cerr)
}

// finishTransfer 校验 size（含续传基数）、处理取消清理（不动用户已存在的原文件）。
// existed 为"拷贝开始时目标是否存在"——续传/重命名后为 false，取消时才清理该目标。
func finishTransfer(h *taskHandle, direction, localPath, remotePath string, existed bool, cli *sftp.Client, base, transferred int64, cerr error) error {
	if cerr == nil {
		h.mu.Lock()
		h.Transferred = base + transferred
		want := h.Size
		h.mu.Unlock()
		if base+transferred != want {
			return fmt.Errorf("size mismatch: sent %d want %d", base+transferred, want)
		}
		return nil
	}
	if !errors.Is(cerr, errCanceled) {
		return cerr
	}
	h.mu.Lock()
	h.State = StateCanceled
	want := h.Size
	h.mu.Unlock()
	if want != 0 || transferred != 0 {
		if !existed {
			if direction == DirDownload {
				_ = os.Remove(localPath)
			} else {
				_ = cli.Remove(remotePath)
			}
		}
	}
	return errCanceled
}

func targetPath(direction, localPath, remotePath string) string {
	if direction == DirDownload {
		return localPath
	}
	return remotePath
}

// renameLocalPath 本地重命名：name.ext → name (1).ext → name (2).ext …
func renameLocalPath(p string) string {
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
}

// renameRemotePath 远程重命名（cli 在手，逐候选探测存在性）
func renameRemotePath(cli *sftp.Client, p string) string {
	dir := path.Dir(p)
	base := path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		cand := path.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := cli.Stat(cand); err != nil {
			return cand
		}
	}
}

// copyWithProgress 拷贝循环：128KB 分块、非阻塞取消检查、200ms 节流进度与限速统计
func (q *Queue) copyWithProgress(h *taskHandle, src io.Reader, dst io.Writer) (int64, error) {
	buf := make([]byte, 128*1024)
	var total int64
	last := time.Now()
	var lastBytes int64
	for {
		if h.canceled() {
			return total, errCanceled
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, fmt.Errorf("write: %w", werr)
			}
			total += int64(n)
			now := time.Now()
			h.mu.Lock()
			h.Transferred = total
			h.mu.Unlock()
			if now.Sub(last) >= 200*time.Millisecond {
				dt := now.Sub(last).Seconds()
				h.mu.Lock()
				if dt > 0 {
					h.SpeedBps = float64(total-lastBytes) / dt
				}
				h.mu.Unlock()
				q.emit(h)
				last = now
				lastBytes = total
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, fmt.Errorf("read: %w", rerr)
		}
	}
}

func (q *Queue) emit(h *taskHandle) {
	h.mu.Lock()
	payload := h.Task // 纯数据拷贝
	h.lastEmit = time.Now()
	h.mu.Unlock()
	q.em.Emit("transfer:progress", payload)
}

// policyFor 取"全部应用"记住的批次策略（未记住返回 ""）
func (q *Queue) policyFor(batchID string) string {
	q.batchMu.Lock()
	defer q.batchMu.Unlock()
	return q.batchPolicy[batchID]
}

// SetTransferPolicy 对批次决策覆盖策略；applyAll=true 记住策略并对该批次
// 全部挂起任务生效，否则只唤醒第一个挂起任务（逐个决策模式）。
func (q *Queue) SetTransferPolicy(batchID, mode string, applyAll bool) error {
	switch mode {
	case PolicyOverwrite, PolicySkip, PolicyRename:
	default:
		return fmt.Errorf("unknown policy: %s", mode)
	}
	if applyAll {
		q.batchMu.Lock()
		q.batchPolicy[batchID] = mode
		q.batchMu.Unlock()
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range q.order {
		h := q.tasks[id]
		if h == nil || h.BatchID != batchID {
			continue
		}
		h.mu.Lock()
		st := h.State
		h.mu.Unlock()
		if st != StateAwaitingPolicy {
			continue
		}
		select {
		case h.policyCh <- mode:
		default:
		}
		if !applyAll {
			return nil // 逐个决策：只唤醒第一个
		}
	}
	return nil
}

// Cancel 取消任务（排队中直接置态；运行中经 cancel 通道中止并清理半成品）
func (q *Queue) Cancel(taskID string) error {
	q.mu.Lock()
	h, ok := q.tasks[taskID]
	q.mu.Unlock()
	if !ok {
		return fmt.Errorf("transfer not found: %s", taskID)
	}
	h.mu.Lock()
	st := h.State
	h.mu.Unlock()
	switch st {
	case StatePending, StateAwaitingPolicy:
		h.cancelOnce.Do(func() { close(h.cancel) })
		h.mu.Lock()
		h.State = StateCanceled
		h.mu.Unlock()
		q.emit(h)
		q.pump()
	case StateRunning:
		h.cancelOnce.Do(func() { close(h.cancel) }) // 拷贝循环负责置态/清理
	default:
		return fmt.Errorf("transfer already finished: %s", taskID)
	}
	return nil
}

// Retry 重试失败/取消的任务（复用原 ID，重置进度重新入队）
func (q *Queue) Retry(taskID string) error {
	q.mu.Lock()
	h, ok := q.tasks[taskID]
	q.mu.Unlock()
	if !ok {
		return fmt.Errorf("transfer not found: %s", taskID)
	}
	h.mu.Lock()
	st := h.State
	h.mu.Unlock()
	if st != StateFailed && st != StateCanceled {
		return fmt.Errorf("only failed/canceled transfers can retry")
	}
	h.mu.Lock()
	h.State = StatePending
	h.Transferred = 0
	h.Err = ""
	h.cancel = make(chan struct{})
	h.cancelOnce = sync.Once{}
	h.mu.Unlock()
	q.pump()
	return nil
}

// sftpMkdirAll 逐级创建远程目录（已存在按成功处理）
func sftpMkdirAll(cli *sftp.Client, dir string) error {
	if dir == "" || dir == "/" {
		return nil
	}
	if fi, err := cli.Stat(dir); err == nil {
		if fi.IsDir() {
			return nil
		}
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	parent := path.Dir(dir)
	if parent != dir {
		if err := sftpMkdirAll(cli, parent); err != nil {
			return err
		}
	}
	if err := cli.Mkdir(dir); err != nil {
		// 并发/幂等场景：已存在视为成功
		if _, serr := cli.Stat(dir); serr == nil {
			return nil
		}
		return err
	}
	return nil
}
