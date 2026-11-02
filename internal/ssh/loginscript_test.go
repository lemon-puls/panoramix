package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// TestLoginScriptExpectSend 服务端在 shell 启动后提示 "Password:"，
// 客户端配置登录脚本应自动应答，且应答内容到达服务端。
func TestLoginScriptExpectSend(t *testing.T) {
	port, closeSrv, gotInput := startPromptingSSHD(t)
	defer closeSrv()

	dir := t.TempDir()
	checker := NewHostKeyChecker(dir + "/known_hosts")
	em := &nopEmitter{}
	mgr := NewManager(checker, em, dir+"/known_hosts", dir+"/logs")
	defer mgr.Shutdown()

	id, err := mgr.Start(ConnectConfig{
		HostID:       "h",
		Addr:         "127.0.0.1",
		Port:         port,
		Username:     "test",
		AuthType:     "password",
		Password:     "test",
		TrustMode:    "add",
		KeepaliveSec: 30,
		LoginScript: []LoginStep{
			{Expect: `Password:\s*$`, Send: "secret-answer"},
			{Expect: `Welcome`, Send: "echo ready"},
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = mgr.Close(id) }()

	waitForInput(t, gotInput, "secret-answer", 10*time.Second)
}

func waitForInput(t *testing.T, got func() string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(got(), want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server never received %q; got %q", want, got())
}

// startPromptingSSHD 启动 sshd：shell 建立后发送 "Password: " 提示，
// 并记录客户端随后写入的内容。
func startPromptingSSHD(t *testing.T) (port int, closeFn func(), gotInput func() string) {
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
	var input strings.Builder
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
					ch, chReqs, _ := newCh.Accept()
					go func(ch gossh.Channel, reqs <-chan *gossh.Request) {
						defer ch.Close()
						for req := range reqs {
							switch req.Type {
							case "pty-req":
								req.Reply(true, nil)
							case "shell":
								req.Reply(true, nil)
								// 提示二次认证
								_, _ = ch.Write([]byte("Password: "))
								go func() {
									buf := make([]byte, 1024)
									for {
										n, rerr := ch.Read(buf)
										if n > 0 {
											mu.Lock()
											input.Write(buf[:n])
											mu.Unlock()
										}
										if rerr != nil {
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
					}(ch, chReqs)
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
	}, func() string {
		mu.Lock()
		defer mu.Unlock()
		return input.String()
	}
}
