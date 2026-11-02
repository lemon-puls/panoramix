package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// TestProxyChainJump 跳板链端到端：目标 sshd 只监听在跳板机可达的地址上，
// 客户端必须经跳板隧道才能连通（验证逐跳 Dial + 握手）。
//
// 拓扑：client → jump(sshd#1) → target(sshd#2)
// 由于本测试在同机运行，用"跳板机上的第二段监听"模拟不可直连：
// 目标监听只绑定 127.0.0.1，客户端通过跳板机的 Dial 访问，
// 若客户端直连也应能通——因此额外断言会话确实经过了跳板（跳板侧记录连接数）。
func TestProxyChainJump(t *testing.T) {
	// 启动两级 sshd（同一进程内）
	jumpPort, jumpClose, jumpHits := startCountingSSHD(t)
	targetPort, targetClose, _ := startCountingSSHD(t)
	defer jumpClose()
	defer targetClose()

	dir := t.TempDir()
	checker := NewHostKeyChecker(dir + "/known_hosts")
	em := &nopEmitter{}
	mgr := NewManager(checker, em, dir+"/known_hosts", dir+"/logs")
	defer mgr.Shutdown()

	cfg := ConnectConfig{
		HostID:        "target",
		Name:          "target",
		Addr:          "127.0.0.1",
		Port:          targetPort,
		Username:      "test",
		AuthType:      "password",
		Password:      "test",
		KeepaliveSec:  30,
		AutoReconnect: false,
		TrustMode:     "add",
		Jumps: []ConnectConfig{{
			HostID:   "jump",
			Name:     "jump",
			Addr:     "127.0.0.1",
			Port:     jumpPort,
			Username: "test",
			AuthType: "password",
			Password: "test",
			// 跳板同样走 TOFU：TrustMode 透传
			TrustMode: "add",
		}},
	}

	id, err := mgr.Start(cfg)
	if err != nil {
		t.Fatalf("start via jump: %v", err)
	}
	defer func() { _ = mgr.Close(id) }()

	// 跳板必须被实际使用
	if got := jumpHits(); got < 1 {
		t.Fatalf("jump host never used (hits=%d)", got)
	}

	// 经跳板执行命令，输出应来自目标 sshd
	out, err := mgr.Run(id, `echo proxy-ok`)
	if err != nil {
		t.Fatalf("run via jump: %v", err)
	}
	if want := "proxy-ok"; !strings.Contains(out, want) {
		t.Fatalf("unexpected output: %q", out)
	}
}

// startCountingSSHD 启动一个最小 sshd（password=test），记录被连接次数。
// 支持 exec 请求（用于 mgr.Run）与 shell 请求。
func startCountingSSHD(t *testing.T) (port int, closeFn func(), hits func() int) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
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

	var mu sync.Mutex
	count := 0
	done := make(chan struct{})

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				close(done)
				return
			}
			mu.Lock()
			count++
			mu.Unlock()
			_, chans, reqs, err := gossh.NewServerConn(c, cfg)
			if err != nil {
				continue
			}
			go gossh.DiscardRequests(reqs)
			go func(chans <-chan gossh.NewChannel) {
				for newCh := range chans {
					switch newCh.ChannelType() {
					case "session":
						ch, chReqs, _ := newCh.Accept()
						go handleCountingSession(ch, chReqs)
					case "direct-tcpip":
						// 跳板机行为：按请求拨号并双向转发（等价 ssh -L / ProxyJump）
						go handleDirectTCPIP(newCh)
					default:
						_ = newCh.Reject(gossh.UnknownChannelType, "no")
					}
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
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return count
		}
}

// handleDirectTCPIP 模拟 OpenSSH 的 direct-tcpip 转发
func handleDirectTCPIP(newCh gossh.NewChannel) {
	var m struct {
		Host           string
		Port           uint32
		OriginatorIP   string
		OriginatorPort uint32
	}
	if err := gossh.Unmarshal(newCh.ExtraData(), &m); err != nil {
		_ = newCh.Reject(gossh.ConnectionFailed, "bad payload")
		return
	}
	target, err := net.DialTimeout("tcp", net.JoinHostPort(m.Host, fmt.Sprint(m.Port)), 5*time.Second)
	if err != nil {
		_ = newCh.Reject(gossh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		_ = target.Close()
		return
	}
	go gossh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(ch, target); _ = ch.Close() }()
	go func() { _, _ = io.Copy(target, ch); _ = target.Close() }()
}

func handleCountingSession(ch gossh.Channel, reqs <-chan *gossh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			var m struct{ Command string }
			if err := gossh.Unmarshal(req.Payload, &m); err != nil {
				req.Reply(false, nil)
				continue
			}
			req.Reply(true, nil)
			// 极简命令语义：echo 后的内容原样返回
			out := m.Command
			if len(out) > 5 && out[:5] == "echo " {
				out = out[5:]
			}
			_, _ = ch.Write([]byte(out + "\n"))
			_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0}))
			return
		case "pty-req", "shell":
			req.Reply(true, nil)
			go func() {
				buf := make([]byte, 4096)
				for {
					if _, err := ch.Read(buf); err != nil {
						return
					}
				}
			}()
		case "keepalive@openssh.com":
			req.Reply(true, nil)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// nopEmitter 测试用空事件接收器
type nopEmitter struct{}

func (nopEmitter) Emit(string, ...interface{}) {}
