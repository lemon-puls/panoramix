package ssh

import (
	"errors"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Manager 管理全部活跃 SSH 会话；是 internal/app 操作 SSH 的唯一入口。
type Manager struct {
	mu             sync.Mutex
	sessions       map[string]*Session
	checker        *HostKeyChecker
	em             Emitter
	knownHostsPath string
	logsDir        string
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
