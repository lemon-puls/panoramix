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

	"panoramix/internal/store"
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
}

// NewService 组装服务层
func NewService(st store.Store, sec store.SecretStore, em Emitter) *Service {
	return &Service{store: st, secrets: sec, emitter: em}
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
