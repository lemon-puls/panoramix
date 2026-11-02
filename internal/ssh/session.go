// Package ssh 提供 SSH 会话全生命周期管理：认证、TOFU 主机密钥校验、
// PTY 终端流、keepalive、断线自动重连与会话日志。
// 本包不感知 Wails（事件经 Emitter 接口注入，依赖方向见 ARCHITECTURE.md）。
package ssh

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Emitter 后端 → 前端事件推送接口（由 main.go 注入 wails 实现）
type Emitter interface {
	Emit(event string, data ...interface{})
}

// 连接状态（conn:state 事件负载；与前端 stores/terminals 的定义保持一致）
const (
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateReconnecting = "reconnecting"
	StateClosed       = "closed"
	StateError        = "error"
)

// ConnectConfig 一次连接的完整参数。Password/Passphrase 由 app 层从
// keychain 解出传入，仅在内存存活，严禁写日志。
type ConnectConfig struct {
	HostID        string
	Name          string
	Addr          string
	Port          int
	Username      string
	AuthType      string // password | key | keyboard_interactive | agent
	KeyPath       string
	Password      string
	Passphrase    string
	InitCommands  []string
	KeepaliveSec  int    // 0 视为默认 30
	AutoReconnect bool   // 断线自动重连（最多 5 次，间隔 3s）
	SessionLog    bool   // 会话日志（按天落文件）
	TrustMode     string // "" | "add" | "replace"，TOFU 二次连接时携带
	// Jumps 跳板链：按顺序建立隧道（每项为跳板主机配置，凭据已由 app 层解出）
	Jumps []ConnectConfig

	// 由 Manager 注入的运行参数
	knownHostsPath string
	logsDir        string
}

func (c *ConnectConfig) hostPort() string {
	return net.JoinHostPort(c.Addr, strconv.Itoa(c.Port))
}

// clientConfigFor 构造某跳连接的 SSH 客户端配置（含主机密钥校验与认证）
func (s *Session) clientConfigFor(cfg *ConnectConfig) (*gossh.ClientConfig, error) {
	auth, err := s.mgr.authFor(*cfg)
	if err != nil {
		return nil, err
	}
	return &gossh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: s.hostKeyCallbackFor(cfg),
		Timeout:         15 * time.Second,
	}, nil
}

// Session 一个活跃的 SSH 终端会话（一个 PTY）
type Session struct {
	id string
	cfg ConnectConfig
	em  Emitter
	mgr *Manager

	mu      sync.Mutex
	client  *gossh.Client
	hops    []*gossh.Client // 跳板链各跳客户端（含最终目标），按建立顺序
	sess    *gossh.Session
	stdin   io.WriteCloser
	state   string
	done    bool // 已终态：用户关闭 / 远端正常退出 / 重连失败，禁止再重连
	logFile *os.File

	batcher *outputBatcher
}

// newID 生成 96bit 随机 hex 会话 ID
func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("ssh: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (s *Session) getState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

func (s *Session) emitState() {
	s.mu.Lock()
	state, hostID, id := s.state, s.cfg.HostID, s.id
	s.mu.Unlock()
	s.em.Emit("conn:state", map[string]interface{}{
		"hostId": hostID, "sessionId": id, "state": state,
	})
}

func (s *Session) isDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func (s *Session) markDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	s.done = true
	return true
}

// isCurrentClient 判断该 client 是否仍是当前连接（防止旧连接的
// keepalive/Wait goroutine 在重连后误触发断线逻辑）
func (s *Session) isCurrentClient(client *gossh.Client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client == client
}

// ---------- 连接 ----------

// dialChain 建立到目标机的 SSH 客户端；配置了跳板链时逐跳打隧道。
// 返回最终客户端与全部跳客户端（用于统一关闭）。
func (s *Session) dialChain() (*gossh.Client, []*gossh.Client, error) {
	var hops []*gossh.Client
	closeAll := func() {
		for i := len(hops) - 1; i >= 0; i-- {
			_ = hops[i].Close()
		}
	}
	var prev *gossh.Client
	for i := range s.cfg.Jumps {
		j := &s.cfg.Jumps[i]
		conn, err := dialHop(prev, j.hostPort())
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("jump %d (%s): %w", i+1, j.hostPort(), err)
		}
		ccfg, err := s.clientConfigFor(j)
		if err != nil {
			_ = conn.Close()
			closeAll()
			return nil, nil, err
		}
		c, chans, reqs, err := gossh.NewClientConn(conn, j.hostPort(), ccfg)
		if err != nil {
			_ = conn.Close()
			closeAll()
			return nil, nil, err
		}
		prev = gossh.NewClient(c, chans, reqs)
		hops = append(hops, prev)
	}

	target := s.cfg.hostPort()
	conn, err := dialHop(prev, target)
	if err != nil {
		closeAll()
		return nil, nil, err
	}
	ccfg, err := s.clientConfigFor(&s.cfg)
	if err != nil {
		_ = conn.Close()
		closeAll()
		return nil, nil, err
	}
	c, chans, reqs, err := gossh.NewClientConn(conn, target, ccfg)
	if err != nil {
		_ = conn.Close()
		closeAll()
		return nil, nil, err
	}
	client := gossh.NewClient(c, chans, reqs)
	hops = append(hops, client)
	return client, hops, nil
}

// dialHop 直连（prev 为空）或经前一跳客户端隧道拨号
func dialHop(prev *gossh.Client, addr string) (net.Conn, error) {
	if prev == nil {
		return net.DialTimeout("tcp", addr, 15*time.Second)
	}
	return prev.Dial("tcp", addr)
}

func (s *Session) connectOnce() error {
	client, hops, err := s.dialChain()
	if err != nil {
		var hke *HostKeyError
		if errors.As(err, &hke) {
			return hke
		}
		return fmt.Errorf("dial ssh: %w", err)
	}

	sess, err := client.NewSession()
	if err != nil {
		closeHops(hops)
		return fmt.Errorf("new session: %w", err)
	}
	// stdout/stderr 合并写入 batcher（终端惯例：合并展示）
	sess.Stdout = s.batcher
	sess.Stderr = s.batcher

	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		closeHops(hops)
		return fmt.Errorf("stdin pipe: %w", err)
	}

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.ICANON:        1,
		gossh.ISIG:          1,
		gossh.TTY_OP_ISPEED: 115200,
		gossh.TTY_OP_OSPEED: 115200,
	}
	// 注意参数顺序：RequestPty(term, rows, cols)——此前传反导致 PTY 被建成
	// 80 行×24 列，top 按错误的屏幕模型重画（每帧交错下沉的根因）
	if err := sess.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		_ = sess.Close()
		closeHops(hops)
		return fmt.Errorf("request pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		_ = sess.Close()
		closeHops(hops)
		return fmt.Errorf("start shell: %w", err)
	}

	s.mu.Lock()
	s.client = client
	s.hops = hops
	s.sess = sess
	s.stdin = stdin
	s.state = StateConnected
	s.mu.Unlock()

	s.openLog()
	s.emitState()

	// 初始命令按序执行（稍等 shell 就绪，避免首条命令被吞）
	if len(s.cfg.InitCommands) > 0 {
		go func() {
			time.Sleep(300 * time.Millisecond)
			for _, cmd := range s.cfg.InitCommands {
				if s.isDone() {
					return
				}
				_, _ = stdin.Write([]byte(cmd + "\n"))
				time.Sleep(150 * time.Millisecond)
			}
		}()
	}

	go s.waitLoop(sess)
	go s.keepaliveWatch(client)
	return nil
}

// hostKeyCallbackFor TOFU 逻辑：unknown + TrustMode=add 时写入条目放行；
// changed + TrustMode=replace 时整行替换放行；其余情况中止握手并抛 HostKeyError。
// 跳板链每一跳都独立校验（错误里的 HostPort 指明是哪一台）。
func (s *Session) hostKeyCallbackFor(cfg *ConnectConfig) gossh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
		hostPort := HostPort(hostname, cfg.Port)
		err := s.mgr.checker.Verify(hostPort, key)
		if err == nil {
			return nil
		}
		var hke *HostKeyError
		if !errors.As(err, &hke) {
			return err
		}
		switch {
		case hke.Kind == "unknown" && s.cfg.TrustMode == "add":
			if terr := s.mgr.checker.Trust(hostPort, key); terr != nil {
				return terr
			}
			return nil
		case hke.Kind == "changed" && s.cfg.TrustMode == "replace":
			if terr := s.mgr.checker.Replace(hostPort, key); terr != nil {
				return terr
			}
			return nil
		default:
			return hke
		}
	}
}

// ---------- 断线/退出 ----------

func (s *Session) waitLoop(sess *gossh.Session) {
	err := sess.Wait()
	if !s.isCurrentSession(sess) {
		return // 已被重连取代，旧会话的退出不再处理
	}
	var code int
	if ee, ok := err.(*gossh.ExitError); ok {
		code = ee.ExitStatus()
		s.finish(StateClosed, strconv.Itoa(code), "exited")
		return
	}
	// 非 ExitError（EOF/连接丢失等）走断线逻辑
	s.handleDisconnect(err)
}

func (s *Session) isCurrentSession(sess *gossh.Session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sess == sess
}

// keepaliveWatch 周期发送 keepalive，失败视为链路断开
func (s *Session) keepaliveWatch(client *gossh.Client) {
	interval := time.Duration(s.cfg.KeepaliveSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		}
		if !s.isCurrentClient(client) {
			return
		}
		// 标准 OpenSSH keepalive；wantReply=true 使失败可被检测
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			if s.isCurrentClient(client) {
				s.handleDisconnect(fmt.Errorf("keepalive: %w", err))
			}
			return
		}
	}
}

func (s *Session) handleDisconnect(cause error) {
	if s.isDone() {
		return
	}
	s.dropTransport()

	if s.cfg.AutoReconnect {
		s.setState(StateReconnecting)
		s.emitState()
		s.reconnectLoop()
		return
	}
	s.finish(StateError, "-1", cause.Error())
}

func (s *Session) reconnectLoop() {
	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if s.isDone() {
			return
		}
		time.Sleep(3 * time.Second)
		if s.isDone() {
			return
		}
		if err := s.connectOnce(); err == nil {
			return // connectOnce 内部已置 connected 并发事件
		}
	}
	s.finish(StateError, "-1", "reconnect failed after 5 attempts")
}

// finish 进入终态：刷残余输出、发 exit 事件、清理资源并从 Manager 摘除
func (s *Session) finish(state, code, reason string) {
	if !s.markDone() {
		return
	}
	s.setState(state)
	s.batcher.Close()
	s.closeLog()
	s.dropTransport()
	s.em.Emit("terminal:exit", map[string]interface{}{
		"sessionId": s.id, "code": code, "reason": reason,
	})
	s.emitState()
	s.mgr.remove(s.id)
}

// Close 用户主动断开（关闭标签页）
func (s *Session) Close() {
	if !s.markDone() {
		return
	}
	s.setState(StateClosed)
	s.batcher.Close()
	s.closeLog()
	s.dropTransport()
	s.em.Emit("terminal:exit", map[string]interface{}{
		"sessionId": s.id, "code": "-1", "reason": "closed by user",
	})
	s.emitState()
	s.mgr.remove(s.id)
}

func (s *Session) dropTransport() {
	s.mu.Lock()
	sess, hops := s.sess, s.hops
	s.client = nil
	s.sess = nil
	s.stdin = nil
	s.hops = nil
	s.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
	closeHops(hops)
}

// closeHops 逆序关闭跳板链（先关最终目标，再逐跳回收）
func closeHops(hops []*gossh.Client) {
	for i := len(hops) - 1; i >= 0; i-- {
		_ = hops[i].Close()
	}
}

// ---------- IO ----------

// Write 写入用户键入数据（来自前端 xterm onData）
func (s *Session) Write(data string) error {
	s.mu.Lock()
	stdin := s.stdin
	s.mu.Unlock()
	if stdin == nil {
		return errors.New("session not connected")
	}
	_, err := stdin.Write([]byte(data))
	return err
}

// Resize 通知远端终端尺寸变化
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		return errors.New("session not connected")
	}
	// WindowChange(h, w)：行在前，同样不能传反
	return sess.WindowChange(rows, cols)
}

// ---------- 会话日志 ----------

var logNameRe = regexp.MustCompile(`[^A-Za-z0-9_\-]+`)

func (s *Session) openLog() {
	if !s.cfg.SessionLog || s.cfg.logsDir == "" {
		return
	}
	if err := os.MkdirAll(s.cfg.logsDir, 0o700); err != nil {
		return // 日志失败不影响会话
	}
	safe := logNameRe.ReplaceAllString(s.cfg.Name, "_")
	name := fmt.Sprintf("%s_%s.log", time.Now().Format("2006-01-02"), safe)
	f, err := os.OpenFile(filepath.Join(s.cfg.logsDir, name),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.logFile = f
	s.mu.Unlock()
	_, _ = f.WriteString(fmt.Sprintf("===== session %s opened %s =====\n",
		s.id, time.Now().Format(time.RFC3339)))
}

func (s *Session) writeLog(data []byte) {
	s.mu.Lock()
	f := s.logFile
	s.mu.Unlock()
	if f == nil {
		return
	}
	_, _ = f.WriteString(time.Now().Format("15:04:05 ") + string(data) + "\n")
}

func (s *Session) closeLog() {
	s.mu.Lock()
	f := s.logFile
	s.logFile = nil
	s.mu.Unlock()
	if f != nil {
		_, _ = f.WriteString(fmt.Sprintf("===== session %s closed %s =====\n",
			s.id, time.Now().Format(time.RFC3339)))
		_ = f.Close()
	}
}

// base64Std 编码输出负载（二进制安全）
func base64Std(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
