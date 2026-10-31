// Package store 是唯一的持久化层：配置数据存 SQLite，敏感凭据存 OS keychain。
// 本包不感知 Wails / WebView（依赖方向见 ARCHITECTURE.md）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Group 连接分组（树形，ParentID 为空表示顶级分组）
type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AuthType SSH 认证方式
type AuthType string

const (
	// AuthPassword 密码认证
	AuthPassword AuthType = "password"
	// AuthKey 私钥认证（KeyPath 引用私钥文件，不复制私钥内容）
	AuthKey AuthType = "key"
	// AuthKeyboardInteractive 键盘交互认证（2FA 场景）
	AuthKeyboardInteractive AuthType = "keyboard_interactive"
)

// Host 一台 SSH 主机。
// 密码 / passphrase 不在本结构中，只存系统 keychain；
// HasSecret 仅为展示标记（keychain 中是否存在凭据），由 service 层填充。
type Host struct {
	ID              string    `json:"id"`
	GroupID         string    `json:"groupId"`
	Name            string    `json:"name"`
	Addr            string    `json:"addr"`
	Port            int       `json:"port"`
	Username        string    `json:"username"`
	AuthType        AuthType  `json:"authType"`
	KeyPath         string    `json:"keyPath"`      // 私钥文件路径（引用而非复制）
	HasSecret       bool      `json:"hasSecret"`    // keychain 中是否已存凭据（只读）
	InitCommands    []string  `json:"initCommands"` // 登录成功后按序执行的初始命令
	KeepaliveSec    int       `json:"keepaliveSec"` // keepalive 间隔秒数，默认 30
	AutoReconnect   bool      `json:"autoReconnect"`
	LastConnectedAt time.Time `json:"lastConnectedAt"`
	Sort            int       `json:"sort"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Snippet 命令片段（点击发送到当前终端；Content 支持 {host} 占位符）
type Snippet struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// HostPasswordRef / HostPassphraseRef 返回主机凭据在 keychain 中的引用键。
// 引用键是唯一的凭据地址，禁止把凭据内容拼进任何日志。
func HostPasswordRef(hostID string) string { return "host:" + hostID + ":password" }

func HostPassphraseRef(hostID string) string { return "host:" + hostID + ":passphrase" }

// NewID 生成 128bit 随机 hex 标识符（后端生成，避免前端伪造/冲突）
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 随机源不可用属于系统级故障，无法安全降级
		panic("store: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
