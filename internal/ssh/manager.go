package ssh

import (
	"errors"
	"fmt"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Manager 管理全部活跃 SSH 会话；是 internal/app 操作 SSH 的唯一入口。
type Manager struct {
	mu             sync.Mutex
	sessions       map[string]*Session
	checker        *HostKeyChecker
	em             Emitter
	knownHostsPath string
	logsDir        string

	// SSH agent 客户端缓存（agent 连接可跨会话复用，避免每次连接泄漏）
	agentMu      sync.Mutex
	agentClient  agent.ExtendedAgent
	agentCleanup func()
	agentErr     error
	agentTried   bool
}

// authFor 返回指定配置可用的认证方法；agent 类型懒加载并缓存 agent 连接
func (m *Manager) authFor(cfg ConnectConfig) ([]gossh.AuthMethod, error) {
	if cfg.AuthType != "agent" {
		return buildAuth(cfg)
	}
	ag, err := m.getAgent()
	if err != nil {
		return nil, err
	}
	return agentAuthMethods(ag), nil
}

func (m *Manager) getAgent() (agent.ExtendedAgent, error) {
	m.agentMu.Lock()
	defer m.agentMu.Unlock()
	if m.agentTried {
		return m.agentClient, m.agentErr
	}
	m.agentTried = true
	ag, cleanup, err := dialAgent()
	if err != nil {
		m.agentErr = fmt.Errorf("connect ssh-agent: %w", err)
		return nil, m.agentErr
	}
	m.agentClient, m.agentCleanup = ag, cleanup
	return ag, nil
}

// Shutdown 释放 Manager 持有的资源（agent 连接）；不影响活跃会话
func (m *Manager) Shutdown() {
	m.agentMu.Lock()
	if m.agentCleanup != nil {
		m.agentCleanup()
		m.agentCleanup = nil
	}
	m.agentMu.Unlock()
}

// NewManager 创建会话管理器（knownHostsPath/logsDir 由 main 依配置目录注入）
func NewManager(checker *HostKeyChecker, em Emitter, knownHostsPath, logsDir string) *Manager {
	return &Manager{
		sessions:       make(map[string]*Session),
		checker:        checker,
		em:             em,
		knownHostsPath: knownHostsPath,
		logsDir:        logsDir,
	}
}

// Start 发起连接：TOFU 失败时返回 *HostKeyError（app 层转结构化结果给前端）。
// 成功返回 sessionId，连接状态经 conn:state 事件推送。
func (m *Manager) Start(cfg ConnectConfig) (string, error) {
	cfg.knownHostsPath = m.knownHostsPath
	cfg.logsDir = m.logsDir

	batcher := newOutputBatcher(16*time.Millisecond, nil)
	s := &Session{
		id:      newID(),
		cfg:     cfg,
		em:      m.em,
		mgr:     m,
		state:   StateConnecting,
		batcher: batcher,
	}
	// flush 里引用 session（写日志 + 推送终端输出）
	batcher.flush = func(data []byte) {
		s.tryLoginScript(data)
		s.writeLog(data)
		s.em.Emit("terminal:output", map[string]interface{}{
			"sessionId": s.id, "dataB64": base64Std(data),
		})
	}

	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()

	s.setState(StateConnecting)
	s.emitState()

	if err := s.connectOnce(); err != nil {
		m.remove(s.id)
		s.batcher.Close()
		s.closeLog()
		return "", err
	}
	return s.id, nil
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// Client 返回会话的底层 SSH 客户端，供 app 层挂载 SFTP 等子系统
// （依赖方向：app → sftp 取 ClientSource 接口 → 此实现，见 ARCHITECTURE.md）。
// 会话不存在或已断开时返回错误。
func (m *Manager) Client(sessionID string) (*gossh.Client, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, errors.New("session not connected")
	}
	return s.client, nil
}

// Run 在指定会话上执行一次性命令并返回输出（监控采集等场景复用会话，
// 每次调用独立 session channel，与终端 PTY 互不干扰）。
func (m *Manager) Run(sessionID, cmd string) (string, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return "", errors.New("session not connected")
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		return string(out), fmt.Errorf("run: %w", err)
	}
	return string(out), nil
}

func (m *Manager) get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return s, nil
}

// Write 向指定会话写入用户键入
func (m *Manager) Write(id, data string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return s.Write(data)
}

// Resize 调整指定会话的终端尺寸
func (m *Manager) Resize(id string, cols, rows int) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return s.Resize(cols, rows)
}

// Close 用户主动断开指定会话
func (m *Manager) Close(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.Close()
	return nil
}
