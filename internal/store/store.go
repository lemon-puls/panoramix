package store

import (
	"errors"
	"time"
)

// ErrSecretNotFound 表示 keychain 中不存在指定凭据
var ErrSecretNotFound = errors.New("secret not found")

// Store 配置持久化接口（SQLite 实现）。
// 敏感凭据（密码/passphrase）不经过本接口，统一走 SecretStore。
// 约定：Save* 方法在 ID 为空时新建（并回填 ID/时间戳），否则更新。
type Store interface {
	Close() error

	ListGroups() ([]Group, error)
	SaveGroup(g *Group) error
	DeleteGroup(id string) error

	ListHosts() ([]Host, error)
	SaveHost(h *Host) error
	DeleteHost(id string) error
	// TouchHost 刷新主机最近连接时间（M2 由 SSH 层调用）
	TouchHost(id string, at time.Time) error

	ListDBConns() ([]DBConn, error)
	SaveDBConn(c *DBConn) error
	DeleteDBConn(id string) error

	ListSnippets() ([]Snippet, error)
	SaveSnippet(s *Snippet) error
	DeleteSnippet(id string) error

	GetSetting(key string) (string, bool, error)
	SetSetting(key, value string) error
	// DeleteSettingsByPrefix 删除以 prefix 开头的所有设置（如 grid.columns.<connId>.）
	DeleteSettingsByPrefix(prefix string) error
}

// SecretStore 系统凭据存取接口（OS keychain 实现；
// M4 补充 keychain 不可用时的主密码 AES-GCM 回退实现）
type SecretStore interface {
	SaveSecret(ref, secret string) error
	// GetSecret 不存在时返回 ErrSecretNotFound
	GetSecret(ref string) (string, error)
	DeleteSecret(ref string) error
}
