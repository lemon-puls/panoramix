package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	gossh "golang.org/x/crypto/ssh"
)

// captureEmitter 捕获 terminal:output 事件载荷，还原原始字节流
type captureEmitter struct {
	mu  sync.Mutex
	buf []byte
}

func (e *captureEmitter) Emit(event string, data ...interface{}) {
	if event != "terminal:output" || len(data) == 0 {
		return
	}
	m, ok := data[0].(map[string]interface{})
	if !ok {
		return
	}
	b64, _ := m["dataB64"].(string)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return
	}
	e.mu.Lock()
	e.buf = append(e.buf, raw...)
	e.mu.Unlock()
}

func (e *captureEmitter) Bytes() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]byte(nil), e.buf...)
}

// startBuiltinSSHD 进程内 SSH 服务端：pty-req 后用真实 PTY 跑 top，
// 字节双向透传（等价于 OpenSSH 对 shell 会话的行为）
func startBuiltinSSHD(t *testing.T) (port int, closeFn func()) {
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
					go handleSessionCh(ch, chanReqs)
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

func handleSessionCh(ch gossh.Channel, reqs <-chan *gossh.Request) {
	defer ch.Close()
	var ptmx interface{ Close() error }
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)
		case "shell":
			if ptmx != nil {
				req.Reply(false, nil)
				break
			}
			req.Reply(true, nil)
			cmd := exec.Command("top", "-d", "1")
			cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")
			f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
			if err != nil {
				lastShellStartErr = fmt.Errorf("pty start: %w", err)
				break
			}
			ptmx = f
			go func() { _, _ = ioCopy(ch, f) }()
			go func() { _, _ = ioCopy(f, ch) }()
		case "window-change":
			req.Reply(true, nil)
		case "keepalive@openssh.com":
			req.Reply(true, nil)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
}

var lastShellStartErr error

// ioCopy 简化命名，避免测试文件内与 io 包观感混淆
func ioCopy(dst interface{ Write([]byte) (int, error) }, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			wn, werr := dst.Write(buf[:n])
			total += int64(wn)
			if werr != nil {
				return total, werr
			}
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

// TestLiveTopRedraw 端到端：走真实 Manager 连接进程内 sshd 跑 top，
// 验证输出字节流中存在光标定位序列（原地重画的必要条件）。
// Linux PTY 环境专用；无 top 或无 PTY 时跳过。
func TestLiveTopRedraw(t *testing.T) {
	if _, err := exec.LookPath("top"); err != nil {
		t.Skip("top not available")
	}
	port, closeSrv := startBuiltinSSHD(t)
	defer closeSrv()

	dir := t.TempDir()
	checker := NewHostKeyChecker(dir + "/known_hosts")
	em := &captureEmitter{}
	mgr := NewManager(checker, em, dir+"/known_hosts", dir+"/logs")

	id, err := mgr.Start(ConnectConfig{
		HostID:        "t1",
		Name:          "local-top",
		Addr:          "127.0.0.1",
		Port:          port,
		Username:      "test",
		AuthType:      "password",
		Password:      "test",
		KeepaliveSec:  30,
		AutoReconnect: false,
		SessionLog:    false,
		TrustMode:     "add", // 内置服务端密钥直接信任
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer func() { _ = mgr.Close(id) }()

	// top -d 1：跑 ~3.5s 应有 ≥2 帧输出
	time.Sleep(3500 * time.Millisecond)

	data := string(em.Bytes())
	if len(data) == 0 {
		t.Fatalf("no terminal output captured (shellStartErr=%v)", lastShellStartErr)
	}
	if len(data) < 200 {
		t.Fatalf("suspiciously small output (%d bytes): %q (shellStartErr=%v)",
			len(data), data, lastShellStartErr)
	}

	frames := strings.Count(data, "load average")
	cursorSeq := regexp.MustCompile(`\x1b\[H|\x1b\[\d+;\d+H`)
	cursorHits := len(cursorSeq.FindAllString(data, -1))

	t.Logf("bytes=%d frames=%d cursorSeq=%d", len(data), frames, cursorHits)

	// 可选：导出原始字节流供 headless xterm 渲染层验证（PANORAMIX_TOP_DUMP=<path>）
	if dumpPath := os.Getenv("PANORAMIX_TOP_DUMP"); dumpPath != "" {
		if err := os.WriteFile(dumpPath, em.Bytes(), 0o600); err != nil {
			t.Logf("dump failed: %v", err)
		}
	}

	if frames < 2 {
		t.Fatalf("expected >=2 top frames, got %d (bytes=%d)", frames, len(data))
	}
	if cursorHits == 0 {
		// 这正是“top 每次刷新追加”的直接原因候选：字节流里没有光标定位
		t.Fatalf("NO cursor-addressing sequences in stream — this is the append symptom (bytes=%d)", len(data))
	}
}
