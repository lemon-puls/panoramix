// Package app 是前端唯一的数据入口层：所有 Wails 绑定方法在此定义，
// 事件推送通过 Emitter 接口注入（协议见 ARCHITECTURE.md）。
// 本包禁止 import wails —— 与 WebView 的耦合只允许存在于 main.go。
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lemon-puls/panoramix/internal/ssh"
	"github.com/lemon-puls/panoramix/internal/store"
)

// Emitter 后端 → 前端事件推送接口；由 main.go 注入 Wails 实现
type Emitter interface {
	SetContext(ctx context.Context)
	Emit(event string, data ...interface{})
}

// Service Wails 绑定服务：前端唯一入口
type Service struct {
	store   store.Store
	secrets store.SecretStore
	emitter Emitter
	ssh     *ssh.Manager
}

// NewService 组装服务层
func NewService(st store.Store, sec store.SecretStore, em Emitter, mgr *ssh.Manager) *Service {
	return &Service{store: st, secrets: sec, emitter: em, ssh: mgr}
}

// Startup 由 Wails 在应用启动时回调
func (s *Service) Startup(ctx context.Context) {
	if s.emitter != nil {
		s.emitter.SetContext(ctx)
	}
	log.Printf("[app] service started")
}

// Shutdown 由 Wails 在应用退出时回调
func (s *Service) Shutdown(ctx context.Context) {
	log.Printf("[app] service shutdown")
}

// Ping 桥接自检：前端启动时调用，验证 Go ↔ WebView 通道
func (s *Service) Ping() string {
	return "pong"
}

// ---------- SSH 会话 ----------

// ConnectHostResult ConnectHost 的结构化返回（TOFU/凭据问题都要给前端可判定信息）
type ConnectHostResult struct {
	Ok          bool   `json:"ok"`
	SessionId   string `json:"sessionId"`
	ErrorCode   string `json:"errorCode"` // "" | host_key_unknown | host_key_changed | auth_failed | connect_failed
	Fingerprint string `json:"fingerprint"`
	Message     string `json:"message"`
}

// ConnectHost 连接主机并打开终端会话。
// trustMode: ""（首次尝试）| "add"（用户确认未知指纹）| "replace"（用户确认指纹变更）。
func (s *Service) ConnectHost(hostID string, trustMode string) ConnectHostResult {
	hosts, err := s.store.ListHosts()
	if err != nil {
		return ConnectHostResult{ErrorCode: "connect_failed", Message: err.Error()}
	}
	var h *store.Host
	for i := range hosts {
		if hosts[i].ID == hostID {
			h = &hosts[i]
			break
		}
	}
	if h == nil {
		return ConnectHostResult{ErrorCode: "connect_failed", Message: "host not found"}
	}

	cfg := ssh.ConnectConfig{
		HostID:        h.ID,
		Name:          h.Name,
		Addr:          h.Addr,
		Port:          h.Port,
		Username:      h.Username,
		AuthType:      string(h.AuthType),
		KeyPath:       h.KeyPath,
		InitCommands:  h.InitCommands,
		KeepaliveSec:  h.KeepaliveSec,
		AutoReconnect: h.AutoReconnect,
		SessionLog:    s.sessionLogEnabled(),
		TrustMode:     trustMode,
	}
	// 凭据在连接瞬间从 keychain 解出；Start 返回后立即置空，防止残留引用
	switch cfg.AuthType {
	case "password", "keyboard_interactive":
		pw, err := s.secrets.GetSecret(store.HostPasswordRef(h.ID))
		if err != nil {
			if errors.Is(err, store.ErrSecretNotFound) {
				return ConnectHostResult{ErrorCode: "auth_failed",
					Message: "no password stored for this host"}
			}
			return ConnectHostResult{ErrorCode: "auth_failed",
				Message: "read credential from keychain failed"}
		}
		cfg.Password = pw
	case "key":
		// passphrase 可选：不存在按无加密私钥处理
		if pp, err := s.secrets.GetSecret(store.HostPassphraseRef(h.ID)); err == nil {
			cfg.Passphrase = pp
		}
	}

	if s.ssh == nil {
		return ConnectHostResult{ErrorCode: "connect_failed", Message: "ssh manager not ready"}
	}
	sessionID, startErr := s.ssh.Start(cfg)
	cfg.Password, cfg.Passphrase = "", ""

	if startErr != nil {
		var hke *ssh.HostKeyError
		if errors.As(startErr, &hke) {
			code := "host_key_unknown"
			if hke.Kind == "changed" {
				code = "host_key_changed"
			}
			// 只记主机与指纹，不记任何凭据
			log.Printf("[app] host key verification required: %s %s", hke.HostPort, hke.Fingerprint)
			return ConnectHostResult{ErrorCode: code, Fingerprint: hke.Fingerprint}
		}
		log.Printf("[app] connect failed: %v", startErr)
		return ConnectHostResult{ErrorCode: "connect_failed", Message: startErr.Error()}
	}

	if terr := s.store.TouchHost(h.ID, time.Now()); terr != nil {
		log.Printf("[app] touch host failed: %v", terr)
	}
	log.Printf("[app] session started: id=%s host=%s", sessionID, h.Name)
	return ConnectHostResult{Ok: true, SessionId: sessionID}
}

// WriteSession 向会话写入用户键入（前端 xterm onData 原样转发）
func (s *Service) WriteSession(sessionID, data string) error {
	return s.ssh.Write(sessionID, data)
}

// ResizeSession 调整指定会话的终端尺寸
func (s *Service) ResizeSession(sessionID string, cols, rows int) error {
	return s.ssh.Resize(sessionID, cols, rows)
}

// CloseSession 用户主动断开（关闭终端标签时调用）
func (s *Service) CloseSession(sessionID string) error {
	log.Printf("[app] session closing: id=%s", sessionID)
	return s.ssh.Close(sessionID)
}

// ---------- 会话日志开关 ----------

const settingSessionLog = "session_log_enabled"

// IsSessionLogEnabled 会话日志全局开关（settings 表持久化）
func (s *Service) IsSessionLogEnabled() (bool, error) {
	v, ok, err := s.store.GetSetting(settingSessionLog)
	if err != nil || !ok {
		return false, err
	}
	return v == "1", nil
}

// SetSessionLogEnabled 开关会话日志（对新开会话生效）
func (s *Service) SetSessionLogEnabled(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	log.Printf("[app] session log enabled=%v", on)
	return s.store.SetSetting(settingSessionLog, v)
}

func (s *Service) sessionLogEnabled() bool {
	on, err := s.IsSessionLogEnabled()
	return err == nil && on
}

// ---------- 分组 ----------

// ListGroups 返回全部分组（前端按 parentId 组装树）
func (s *Service) ListGroups() ([]store.Group, error) {
	groups, err := s.store.ListGroups()
	if err != nil {
		log.Printf("[app] list groups failed: %v", err)
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, nil
}

// SaveGroup 新建（ID 为空）或更新分组
func (s *Service) SaveGroup(g store.Group) (store.Group, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return g, fmt.Errorf("group name is required")
	}
	if err := s.store.SaveGroup(&g); err != nil {
		log.Printf("[app] save group failed: %v", err)
		return g, fmt.Errorf("save group: %w", err)
	}
	log.Printf("[app] group saved: id=%s name=%q", g.ID, g.Name)
	return g, nil
}

// DeleteGroup 删除分组；分组下仍有主机时拒绝（前端提示先移动主机）
func (s *Service) DeleteGroup(id string) error {
	hosts, err := s.store.ListHosts()
	if err != nil {
		return fmt.Errorf("list hosts: %w", err)
	}
	for _, h := range hosts {
		if h.GroupID == id {
			return fmt.Errorf("group still contains hosts, move them first")
		}
	}
	if err := s.store.DeleteGroup(id); err != nil {
		log.Printf("[app] delete group failed: %v", err)
		return fmt.Errorf("delete group: %w", err)
	}
	log.Printf("[app] group deleted: id=%s", id)
	return nil
}

// ---------- 主机 ----------

// HostInput 新建/编辑主机的入参。
// Password / Passphrase 仅用于传输：写入 keychain 后立即清空，
// 绝不落库、绝不入日志（String/GoString 已脱敏并有测试保障）。
type HostInput struct {
	store.Host
	Password   string `json:"password"`
	Passphrase string `json:"passphrase"`
}

// String 脱敏输出，保证 %v 不泄露凭据
func (in HostInput) String() string {
	return fmt.Sprintf("{host:%s addr:%s:%d user:%s auth:%s password:*** passphrase:***}",
		in.Name, in.Addr, in.Port, in.Username, in.AuthType)
}

// GoString 脱敏输出，保证 %#v 不泄露凭据
func (in HostInput) GoString() string { return in.String() }

// ListHosts 返回全部主机（HasSecret 由 keychain 实时判定）
func (s *Service) ListHosts() ([]store.Host, error) {
	hosts, err := s.store.ListHosts()
	if err != nil {
		log.Printf("[app] list hosts failed: %v", err)
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	for i := range hosts {
		hosts[i].HasSecret = s.hasAnySecret(hosts[i].ID)
	}
	return hosts, nil
}

// SaveHost 新建（ID 为空）或更新主机。
// 凭据语义：Password/Passphrase 非空则覆盖 keychain；留空表示不修改已存凭据。
// 删除凭据走 DeleteHostSecret。
func (s *Service) SaveHost(in HostInput) (store.Host, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Addr = strings.TrimSpace(in.Addr)
	if in.Name == "" || in.Addr == "" {
		return in.Host, fmt.Errorf("host name and address are required")
	}

	// 新主机先定 ID，保证凭据引用键正确
	if in.ID == "" {
		in.ID = store.NewID()
	}

	// 凭据只进 keychain
	if in.Password != "" {
		if err := s.secrets.SaveSecret(store.HostPasswordRef(in.ID), in.Password); err != nil {
			log.Printf("[app] save host password failed: %v", err)
			return in.Host, fmt.Errorf("save password: %w", err)
		}
	}
	if in.Passphrase != "" {
		if err := s.secrets.SaveSecret(store.HostPassphraseRef(in.ID), in.Passphrase); err != nil {
			log.Printf("[app] save host passphrase failed: %v", err)
			return in.Host, fmt.Errorf("save passphrase: %w", err)
		}
	}
	// 传输完成立即清空，防止后续误引用
	in.Password, in.Passphrase = "", ""

	h := in.Host
	if err := s.store.SaveHost(&h); err != nil {
		log.Printf("[app] save host failed: %v", err)
		return h, fmt.Errorf("save host: %w", err)
	}
	h.HasSecret = s.hasAnySecret(h.ID)
	log.Printf("[app] host saved: id=%s name=%q addr=%s:%d auth=%s",
		h.ID, h.Name, h.Addr, h.Port, h.AuthType)
	return h, nil
}

// DeleteHost 删除主机并清理其全部凭据
func (s *Service) DeleteHost(id string) error {
	if err := s.store.DeleteHost(id); err != nil {
		log.Printf("[app] delete host failed: %v", err)
		return fmt.Errorf("delete host: %w", err)
	}
	// 凭据清理失败不影响删除结果（keychain 已不存在的条目按成功处理）
	_ = s.secrets.DeleteSecret(store.HostPasswordRef(id))
	_ = s.secrets.DeleteSecret(store.HostPassphraseRef(id))
	log.Printf("[app] host deleted: id=%s", id)
	return nil
}

// SetHostSecret 单独覆盖某类凭据（kind: password | passphrase）
func (s *Service) SetHostSecret(hostID, kind, secret string) error {
	ref, err := secretRef(hostID, kind)
	if err != nil {
		return err
	}
	if secret == "" {
		return fmt.Errorf("secret value is required")
	}
	if err := s.secrets.SaveSecret(ref, secret); err != nil {
		log.Printf("[app] set host secret failed: %v", err)
		return fmt.Errorf("set host secret: %w", err)
	}
	return nil
}

// DeleteHostSecret 删除某类凭据（kind: password | passphrase）
func (s *Service) DeleteHostSecret(hostID, kind string) error {
	ref, err := secretRef(hostID, kind)
	if err != nil {
		return err
	}
	return s.secrets.DeleteSecret(ref)
}

func secretRef(hostID, kind string) (string, error) {
	switch kind {
	case "password":
		return store.HostPasswordRef(hostID), nil
	case "passphrase":
		return store.HostPassphraseRef(hostID), nil
	default:
		return "", fmt.Errorf("unknown secret kind: %s", kind)
	}
}

func (s *Service) hasAnySecret(hostID string) bool {
	for _, ref := range []string{store.HostPasswordRef(hostID), store.HostPassphraseRef(hostID)} {
		_, err := s.secrets.GetSecret(ref)
		if err == nil {
			return true
		}
		if !errors.Is(err, store.ErrSecretNotFound) {
			// keychain 本身不可用时按“无凭据”处理，不让列表页炸掉
			log.Printf("[app] read secret state failed: %v", err)
		}
	}
	return false
}

// ---------- 命令片段 ----------

// ListSnippets 返回全部片段
func (s *Service) ListSnippets() ([]store.Snippet, error) {
	snippets, err := s.store.ListSnippets()
	if err != nil {
		log.Printf("[app] list snippets failed: %v", err)
		return nil, fmt.Errorf("list snippets: %w", err)
	}
	return snippets, nil
}

// SaveSnippet 新建（ID 为空）或更新片段
func (s *Service) SaveSnippet(sn store.Snippet) (store.Snippet, error) {
	sn.Name = strings.TrimSpace(sn.Name)
	sn.Content = strings.TrimSpace(sn.Content)
	if sn.Name == "" || sn.Content == "" {
		return sn, fmt.Errorf("snippet name and content are required")
	}
	if err := s.store.SaveSnippet(&sn); err != nil {
		log.Printf("[app] save snippet failed: %v", err)
		return sn, fmt.Errorf("save snippet: %w", err)
	}
	return sn, nil
}

// DeleteSnippet 删除片段
func (s *Service) DeleteSnippet(id string) error {
	if err := s.store.DeleteSnippet(id); err != nil {
		log.Printf("[app] delete snippet failed: %v", err)
		return fmt.Errorf("delete snippet: %w", err)
	}
	return nil
}
