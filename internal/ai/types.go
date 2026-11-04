// Package ai 提供 AI SQL 助手的 provider 客户端与纯函数工具：
// OpenAI 兼容 /chat/completions 请求、schema payload 构造、DDL 脱敏、
// SQL 语句分析与响应解析。
//
// 依赖约束：本包只依赖标准库，禁止 import 任何 internal 包（见 ARCHITECTURE.md）。
// 本包不持有凭据：API key 由 app 层从 keychain 取出后通过 ClientConfig 注入，
// 任何日志/错误信息都不得包含 key、问题文本或模型响应。
package ai

import (
	"context"
	"time"
)

// ProviderKind provider 类型（仅用于预设与展示；协议统一为 OpenAI-compatible）
type ProviderKind string

const (
	// ProviderOllama 本机 Ollama（默认）
	ProviderOllama ProviderKind = "ollama"
	// ProviderLMStudio 本机 LM Studio
	ProviderLMStudio ProviderKind = "lmstudio"
	// ProviderOpenAICompatible 任意 OpenAI 兼容服务（含云端）
	ProviderOpenAICompatible ProviderKind = "openai-compatible"
)

// Mode 助手模式
type Mode string

const (
	// ModeGenerate 根据自然语言生成 SQL
	ModeGenerate Mode = "generate"
	// ModeExplain 解释已有 SQL
	ModeExplain Mode = "explain"
	// ModeFix 根据错误修复 SQL
	ModeFix Mode = "fix"
)

// Scope endpoint 的网络范围。注意：这是基于 URL 字面的提示，不是安全边界
// （DNS 可解析到别处），真正的边界是“默认关闭 + 不跟随重定向 + 用户确认”。
type Scope string

const (
	// ScopeLocal loopback（127.0.0.0/8、::1、localhost）
	ScopeLocal Scope = "local"
	// ScopeLAN 局域网（RFC1918、链路本地、ULA、*.local）
	ScopeLAN Scope = "lan"
	// ScopeCloud 公网
	ScopeCloud Scope = "cloud"
)

// Config AI 非敏感配置（API key 不在此结构中，只存 keychain）
type Config struct {
	Enabled          bool         `json:"enabled"`
	Provider         ProviderKind `json:"provider"`
	BaseURL          string       `json:"baseUrl"`
	Model            string       `json:"model"`
	Temperature      float64      `json:"temperature"`
	TimeoutSec       int          `json:"timeoutSec"`
	AllowWrite       bool         `json:"allowWrite"`
	AllowInsecureTLS bool         `json:"allowInsecureTls"`
	Concurrency      int          `json:"concurrency"`
}

// DefaultConfig 返回默认配置：AI 默认关闭；启用后默认指向本机 Ollama。
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		Provider:    ProviderOllama,
		BaseURL:     "http://127.0.0.1:11434/v1",
		Model:       "",
		Temperature: 0.1,
		TimeoutSec:  120, // 本地 7B 冷启动可能较慢
		AllowWrite:  false,
		Concurrency: 1,
	}
}

// ConfigView 返回给前端的配置视图：附加 endpoint 判定与 key 存在性（不回显 key）
type ConfigView struct {
	Config
	HasKey       bool      `json:"hasKey"`
	Endpoint     string    `json:"endpoint"`
	EndpointHash string    `json:"endpointHash"`
	Scope        Scope     `json:"scope"`
	CloudConsent bool      `json:"cloudConsent"`
	Warnings     []Warning `json:"warnings"`
}

// Request 助手请求。
// 关键约束：本结构没有任何行数据、结果集、凭据字段；schema 只来自元数据。
type Request struct {
	RequestID string   `json:"requestId"` // 前端生成（含 fallback），用于取消与事件关联
	ConnID    string   `json:"connId"`
	Database  string   `json:"database"`
	Mode      Mode     `json:"mode"`
	Question  string   `json:"question"`
	Tables    []string `json:"tables"` // 用户显式选择的表；视图会被排除
	SQL       string   `json:"sql"`    // explain / fix
	Error     string   `json:"error"`  // fix
	Locale    string   `json:"locale"` // zh-CN | en-US，决定解释语言
}

// Warning 结构化警告（code 由前端 i18n 翻译，detail 为可读补充）
type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// 警告代码
const (
	WarnViewExcluded         = "view_excluded"
	WarnForeignKeyUnselected = "foreign_key_unselected"
	WarnWriteOperation       = "write_operation"
	WarnMultiStatement       = "multi_statement"
	WarnDangerousStatement   = "dangerous_statement"
	WarnUnknownTable         = "unknown_table"
	WarnUncertainStatement   = "uncertain_statement"
	WarnInsecureTLS          = "insecure_tls"
	WarnMissingV1Path        = "missing_v1_path"
	WarnPublicEndpoint       = "public_endpoint"
	WarnInvalidEndpoint      = "invalid_endpoint"
	WarnNoContext            = "no_context"
)

// TablePreview 单表上下文摘要（前端展示字符数 / 排除原因）
type TablePreview struct {
	Name     string `json:"name"`
	Chars    int    `json:"chars"`
	Excluded bool   `json:"excluded"`
	Reason   string `json:"reason,omitempty"`
}

// Prepared 两阶段协议的第一阶段结果：
// payload 已构建并缓存；AIGenerate 只发送这份缓存原文，保证“预览 = 实发”。
type Prepared struct {
	PrepareID    string         `json:"prepareId"`
	RequestID    string         `json:"requestId"`
	Payload      string         `json:"payload"`     // 完整发送内容（system + user）
	PayloadHash  string         `json:"payloadHash"` // sha256 前 16 位，前端展示一致性
	PromptChars  int            `json:"promptChars"`
	SchemaChars  int            `json:"schemaChars"`
	Tables       []TablePreview `json:"tables"`
	Warnings     []Warning      `json:"warnings"`
	Endpoint     string         `json:"endpoint"`
	EndpointHash string         `json:"endpointHash"`
	Scope        Scope          `json:"scope"`
	Provider     ProviderKind   `json:"provider"`
	Model        string         `json:"model"`
	NeedsConsent bool           `json:"needsConsent"` // 云端且该 endpoint 尚未授权
	ExpiresInSec int            `json:"expiresInSec"`
	AllowWrite   bool           `json:"allowWrite"`
	Mode         Mode           `json:"mode"`
}

// Result 模型返回的解析结果（已做多语句/写操作判定）
type Result struct {
	SQL               string       `json:"sql"`
	Explanation       string       `json:"explanation"`
	Warnings          []Warning    `json:"warnings"`
	Provider          ProviderKind `json:"provider"`
	Model             string       `json:"model"`
	DurationMs        int64        `json:"durationMs"`
	PromptChars       int          `json:"promptChars"`
	ResponseChars     int          `json:"responseChars"`
	IsWrite           bool         `json:"isWrite"`
	MultiStatement    bool         `json:"multiStatement"`
	Dangerous         bool         `json:"dangerous"`
	Uncertain         bool         `json:"uncertain"`
	InsertAllowed     bool         `json:"insertAllowed"`
	InsertBlockReason string       `json:"insertBlockReason,omitempty"`
}

// ChatRequest 一次模型调用的输入（由 Prepared 缓存快照生成）
type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
}

// Client 模型客户端接口；app.NewService 注入，测试用 httptest 假 provider。
type Client interface {
	// Generate 发起一次对话；onDelta 在流式响应中逐段回调（可为 nil）。
	// 返回完整文本。错误使用 *ProviderError 便于映射。
	Generate(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error)

	// TestConnection 校验 base URL / model / key 是否可用。
	TestConnection(ctx context.Context) error
}

// prepareTTLDefault 两阶段协议的缓存有效期
const prepareTTLDefault = 5 * time.Minute

// TTL 暴露默认有效期（app 层测试可覆盖）
const PrepareTTL = prepareTTLDefault
