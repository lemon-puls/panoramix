package sftp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// nopOpener 测试用：不真正打开系统编辑器
type nopOpener struct{}

func (nopOpener) OpenFile(string) error { return nil }

// TestEditorRoundtrip 远程编辑闭环：下载 → 本地修改 → 自动回传
func TestEditorRoundtrip(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	remoteDir := filepath.Join(root, "edit")
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	remotePath := filepath.Join(remoteDir, "app.conf")
	if err := os.WriteFile(remotePath, []byte("port=80\n"), 0o600); err != nil {
		t.Fatalf("seed remote: %v", err)
	}

	em := &captureEmitter{}
	ed := NewEditor(dialSource{cli}, em, nopOpener{}, t.TempDir())
	defer ed.Close()

	info, err := ed.StartEdit("s1", remotePath)
	if err != nil {
		t.Fatalf("start edit: %v", err)
	}
	got, err := os.ReadFile(info.LocalPath)
	if err != nil {
		t.Fatalf("read local copy: %v", err)
	}
	if string(got) != "port=80\n" {
		t.Fatalf("local copy mismatch: %q", got)
	}

	// 模拟编辑器保存
	if err := os.WriteFile(info.LocalPath, []byte("port=8080\n"), 0o600); err != nil {
		t.Fatalf("write local: %v", err)
	}

	waitFor(t, 15*time.Second, func() bool {
		b, err := os.ReadFile(remotePath)
		return err == nil && string(b) == "port=8080\n"
	}, "auto upload after save")

	// 事件在内容写入之后发出，可能与上面的 waitFor 存在极小竞态——等待而非立即断言
	waitFor(t, 5*time.Second, func() bool {
		em.mu.Lock()
		defer em.mu.Unlock()
		return len(em.editEvents) > 0
	}, "edit:state event emitted")
}

// TestEditorConflictDetected 编辑期间远程被改动 → 保存时挂起冲突，不覆盖
func TestEditorConflictDetected(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	remoteDir := filepath.Join(root, "edit")
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	remotePath := filepath.Join(remoteDir, "conf.txt")
	if err := os.WriteFile(remotePath, []byte("v1\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	em := &captureEmitter{}
	ed := NewEditor(dialSource{cli}, em, nopOpener{}, t.TempDir())
	defer ed.Close()

	info, err := ed.StartEdit("s1", remotePath)
	if err != nil {
		t.Fatalf("start edit: %v", err)
	}

	// 他处修改远程（内容 + mtime 变化）
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(remotePath, []byte("remote-changed\n"), 0o600); err != nil {
		t.Fatalf("modify remote: %v", err)
	}

	// 本地保存 → 应检测到冲突且不覆盖
	if err := os.WriteFile(info.LocalPath, []byte("local-change\n"), 0o600); err != nil {
		t.Fatalf("write local: %v", err)
	}
	waitFor(t, 15*time.Second, func() bool {
		em.mu.Lock()
		defer em.mu.Unlock()
		for _, e := range em.editEvents {
			if e["state"] == "conflict" {
				return true
			}
		}
		return false
	}, "conflict event")

	// 远程内容必须保持不变
	b, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if string(b) != "remote-changed\n" {
		t.Fatalf("remote was overwritten despite conflict: %q", b)
	}

	// 用户选择覆盖 → 远程变为本地内容
	if err := ed.ResolveConflict(info.ID, true); err != nil {
		t.Fatalf("resolve overwrite: %v", err)
	}
	b, err = os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("read remote after resolve: %v", err)
	}
	if string(b) != "local-change\n" {
		t.Fatalf("overwrite not applied: %q", b)
	}
}
