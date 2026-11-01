package sftp

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/pkg/sftp"
)

// ---------- 进程内 SSH+SFTP 服务端（仅测试） ----------

func startSftpSSHD(t *testing.T) (port int, closeFn func()) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(conn gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if conn.User() == "test" && string(password) == "test" {
				return &gossh.Permissions{}, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port

	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				close(done)
				return
			}
			_, chans, reqs, err := gossh.NewServerConn(c, cfg)
			if err != nil {
				continue
			}
			go gossh.DiscardRequests(reqs)
			go func(chans <-chan gossh.NewChannel) {
				for newCh := range chans {
					if newCh.ChannelType() != "session" {
						_ = newCh.Reject(gossh.UnknownChannelType, "no")
						continue
					}
					ch, chanReqs, _ := newCh.Accept()
					go handleSftpSession(ch, chanReqs)
				}
			}(chans)
		}
	}()

	return port, func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}

type subsystemMsg struct {
	Name string
}

var lastServeErr error

func handleSftpSession(ch gossh.Channel, reqs <-chan *gossh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "subsystem":
			var m subsystemMsg
			if err := gossh.Unmarshal(req.Payload, &m); err != nil || m.Name != "sftp" {
				req.Reply(false, nil)
				continue
			}
			req.Reply(true, nil)
			// pkg/sftp 纯 Go 服务端：服务真实文件系统（测试只用专属 /tmp 路径）
			srv, err := sftp.NewServer(ch)
			if err != nil {
				lastServeErr = fmt.Errorf("new server: %w", err)
				continue
			}
			lastServeErr = srv.Serve()
			// Serve 退出后立即关通道：让客户端 recv 端立刻 EOF 解除阻塞
			_ = ch.Close()
		case "keepalive@openssh.com":
			req.Reply(true, nil)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// ---------- 测试用 ClientSource / Emitter ----------

// dialSource 测试专用：单一预连接客户端
type dialSource struct{ client *gossh.Client }

func (s dialSource) Client(string) (*gossh.Client, error) { return s.client, nil }

type captureEmitter struct {
	mu        sync.Mutex
	conflicts []map[string]interface{}
	progress  int
}

func (e *captureEmitter) Emit(event string, data ...interface{}) {
	if len(data) == 0 {
		return
	}
	m, _ := data[0].(map[string]interface{})
	e.mu.Lock()
	defer e.mu.Unlock()
	switch event {
	case "transfer:conflict":
		e.conflicts = append(e.conflicts, m)
	case "transfer:progress":
		e.progress++
	}
}

func dialTestClient(t *testing.T, port int) *gossh.Client {
	t.Helper()
	cli, err := gossh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &gossh.ClientConfig{
		User:            "test",
		Auth:            []gossh.AuthMethod{gossh.Password("test")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), // 测试专用
		Timeout:         10 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return cli
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ---------- 用例 ----------

func TestSftpQueueRoundtrip(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir() // “远程”目录
	local := t.TempDir()
	em := &captureEmitter{}
	q := NewQueue(dialSource{cli}, em)

	// 1. 上传 → 远程出现同内容文件
	big := randBytes(t, 3*1024*1024)
	bigPath := filepath.Join(local, "big.bin")
	if err := os.WriteFile(bigPath, big, 0o600); err != nil {
		t.Fatalf("write local: %v", err)
	}
	remoteDir := filepath.Join(root, "up")
	batchID, err := q.Enqueue("s1", DirUpload, remoteDir, bigPath)
	if err != nil {
		t.Fatalf("enqueue upload: %v", err)
	}
	if batchID == "" {
		t.Fatal("empty batch id")
	}
	remoteBig := filepath.Join(remoteDir, "big.bin")
	uploadDone := false
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !uploadDone {
		for _, tk := range q.Tasks() {
			if tk.State == StateDone {
				uploadDone = true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !uploadDone {
		t.Logf("lastServeErr: %v", lastServeErr)
		em.mu.Lock()
		t.Logf("conflicts: %+v", em.conflicts)
		t.Logf("progress events: %d", em.progress)
		em.mu.Unlock()
		for _, tk := range q.Tasks() {
			t.Logf("task dump: %+v", tk)
		}
		t.Fatal("upload did not finish (dump above)")
	}

	sc, err := sftp.NewClient(cli)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	rfi, err := sc.Stat(remoteBig)
	if err != nil {
		t.Fatalf("remote stat: %v", err)
	}
	if rfi.Size() != int64(len(big)) {
		t.Fatalf("remote size %d != %d", rfi.Size(), len(big))
	}
	_ = sc.Close()

	// 2. 下载 → 内容逐字节一致
	dlDir := t.TempDir()
	if _, err := q.Enqueue("s1", DirDownload, remoteBig, dlDir); err != nil {
		t.Fatalf("enqueue download: %v", err)
	}
	dlPath := filepath.Join(dlDir, "big.bin")
	waitFor(t, 30*time.Second, func() bool {
		_, err := os.Stat(dlPath)
		return err == nil && func() bool {
			for _, tk := range q.Tasks() {
				if tk.LocalPath == dlPath && tk.State == StateDone {
					return true
				}
			}
			return false
		}()
	}, "download done")
	got, err := os.ReadFile(dlPath)
	if err != nil {
		t.Fatalf("read downloaded: %v", err)
	}
	if sha(got) != sha(big) {
		t.Fatal("downloaded content mismatch")
	}
}

func TestSftpQueueCancelCleansPartial(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	local := t.TempDir()
	em := &captureEmitter{}
	q := NewQueue(dialSource{cli}, em)

	// 100MB 模式化内容（构造快、传得久，来得及取消）
	big := make([]byte, 100*1024*1024)
	for i := range big {
		big[i] = byte(i % 251)
	}
	bigPath := filepath.Join(local, "huge.bin")
	if err := os.WriteFile(bigPath, big, 0o600); err != nil {
		t.Fatalf("write local: %v", err)
	}
	remoteDir := filepath.Join(root, "up")
	batchID, err := q.Enqueue("s1", DirUpload, remoteDir, bigPath)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_ = batchID
	remoteBig := filepath.Join(remoteDir, "huge.bin")

	// 等它进入 running 再取消
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateRunning {
				return true
			}
		}
		return false
	}, "task running")
	var taskID string
	for _, tk := range q.Tasks() {
		taskID = tk.ID
	}
	if err := q.Cancel(taskID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitFor(t, 15*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.ID == taskID && tk.State == StateCanceled {
				return true
			}
		}
		return false
	}, "task canceled")

	// 半成品应被清理（目标原本不存在）
	if _, err := sftpWaitStat(cli, remoteBig, 2*time.Second); err == nil {
		t.Fatal("partial remote file should be removed after cancel")
	}
}

func TestSftpQueueConflictPolicySkip(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	local := t.TempDir()
	em := &captureEmitter{}
	q := NewQueue(dialSource{cli}, em)

	remoteDir := filepath.Join(root, "up")
	// 预置远程同名文件（内容 A）
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rf, err := os.OpenFile(filepath.Join(remoteDir, "f.txt"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("seed remote: %v", err)
	}
	_, _ = rf.Write([]byte("AAAA"))
	_ = rf.Close()

	// 本地内容 B
	fPath := filepath.Join(local, "f.txt")
	if err := os.WriteFile(fPath, []byte("BBBB"), 0o600); err != nil {
		t.Fatalf("write local: %v", err)
	}

	batchID, err := q.Enqueue("s1", DirUpload, remoteDir, fPath)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// 应挂起等待策略，并发出 conflict 事件
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateAwaitingPolicy {
				return true
			}
		}
		return false
	}, "awaiting policy")
	em.mu.Lock()
	n := len(em.conflicts)
	em.mu.Unlock()
	if n == 0 {
		t.Fatal("no conflict event emitted")
	}

	// 决策 skip → 远程内容保持 A
	if err := q.SetTransferPolicy(batchID, PolicySkip, false); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateSkipped {
				return true
			}
		}
		return false
	}, "skipped")

	got, err := os.ReadFile(filepath.Join(remoteDir, "f.txt"))
	if err != nil {
		t.Fatalf("read remote via fs: %v", err)
	}
	if string(got) != "AAAA" {
		t.Fatalf("remote content changed by skip: %q", got)
	}

	// 决策 overwrite（单任务）→ 覆盖为 B
	if _, err := q.Enqueue("s1", DirUpload, remoteDir, fPath); err != nil {
		t.Fatalf("enqueue again: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateAwaitingPolicy {
				return true
			}
		}
		return false
	}, "awaiting policy 2")
	em.mu.Lock()
	last := em.conflicts[len(em.conflicts)-1]
	em.mu.Unlock()
	cb, _ := last["batchId"].(string)
	if err := q.SetTransferPolicy(cb, PolicyOverwrite, false); err != nil {
		t.Fatalf("set overwrite: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool {
		got, err := os.ReadFile(filepath.Join(remoteDir, "f.txt"))
		return err == nil && string(got) == "BBBB"
	}, "overwritten")
}

// TestSftpQueueResume 断点续传：远程预置半截文件 → 上传应从断点续传
//（Resumed=true、不再从头），且最终内容与源一致；同 size 视为已完成。
func TestSftpQueueResume(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	local := t.TempDir()
	em := &captureEmitter{}
	q := NewQueue(dialSource{cli}, em)

	// 源文件 10MB
	full := make([]byte, 10*1024*1024)
	for i := range full {
		full[i] = byte(i * 7 % 253)
	}
	srcPath := filepath.Join(local, "resume.bin")
	if err := os.WriteFile(srcPath, full, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	remoteDir := filepath.Join(root, "up")
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	remotePath := filepath.Join(remoteDir, "resume.bin")

	// 远程预置前 4MB（模拟上次传了一半被打断）
	half := full[:4*1024*1024]
	rf, err := os.OpenFile(remotePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("seed remote: %v", err)
	}
	_, _ = rf.Write(half)
	_ = rf.Close()

	// 续传上传
	if _, err := q.Enqueue("s1", DirUpload, remoteDir, srcPath); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		tasks := q.Tasks()
		for _, tk := range tasks {
			if tk.State == StateDone && tk.Resumed {
				return true
			}
		}
		return false
	}, "resumed upload done")

	got, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if sha(got) != sha(full) {
		t.Fatal("resumed remote content mismatch")
	}

	// 再传一次：目标恰好等大（内容可能不同）→ 进冲突流程由用户决策
	batchID, err := q.Enqueue("s1", DirUpload, remoteDir, srcPath)
	if err != nil {
		t.Fatalf("enqueue again: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.BatchID == batchID && tk.State == StateAwaitingPolicy {
				return true
			}
		}
		return false
	}, "same-size conflict dialog")
	if err := q.SetTransferPolicy(batchID, PolicySkip, true); err != nil {
		t.Fatalf("set skip all: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.BatchID == batchID && tk.State == StateSkipped {
				return true
			}
		}
		return false
	}, "same-size skipped")
}

// TestSftpQueueDownloadResume 下载方向对称：本地预置半截文件 → 续传补全。
func TestSftpQueueDownloadResume(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	local := t.TempDir()
	q := NewQueue(dialSource{cli}, &captureEmitter{})

	// 远程源文件 6MB
	full := make([]byte, 6*1024*1024)
	for i := range full {
		full[i] = byte(i % 249)
	}
	remoteDir := filepath.Join(root, "down")
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	remotePath := filepath.Join(remoteDir, "dl.bin")
	rf, err := os.OpenFile(remotePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("seed remote: %v", err)
	}
	_, _ = rf.Write(full)
	_ = rf.Close()

	// 本地预置前 1MB（模拟上次下载一半）
	dlPath := filepath.Join(local, "dl.bin")
	lf, err := os.OpenFile(dlPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("seed local: %v", err)
	}
	_, _ = lf.Write(full[:1*1024*1024])
	_ = lf.Close()

	// 下载：localPath 是"另存为"完整路径（存在但非目录 → 走完整路径语义）
	if _, err := q.Enqueue("s1", DirDownload, remotePath, dlPath); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateDone && tk.Resumed {
				return true
			}
		}
		return false
	}, "resumed download done")

	got, err := os.ReadFile(dlPath)
	if err != nil {
		t.Fatalf("read local: %v", err)
	}
	if sha(got) != sha(full) {
		t.Fatal("resumed download content mismatch")
	}
}
func sftpWaitStat(cli *gossh.Client, target string, timeout time.Duration) (os.FileInfo, error) {
	sc, err := sftp.NewClient(cli)
	if err != nil {
		return nil, err
	}
	defer sc.Close()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fi, err := sc.Stat(target)
		if err != nil {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
		_ = fi
	}
	return nil, fmt.Errorf("timeout")
}
