package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// ErrEndpointUserInfo endpoint 中不允许出现 userinfo（user:pass@host）。
// 这是防止凭据被 URL 携带进日志/重定向的真实攻击面。
var ErrEndpointUserInfo = errors.New("endpoint must not contain userinfo")

// ErrEndpointScheme 只允许 http / https
var ErrEndpointScheme = errors.New("endpoint must use http or https")

// Endpoint 规范化后的模型服务地址。
// 规范化规则（与总监评审一致）：协议 + 主机 + 端口 + 路径；去掉 userinfo / query / fragment。
type Endpoint struct {
	Normalized string // 例：http://127.0.0.1:11434/v1（默认端口省略）
	Hash       string // sha256(normalized) 前 16 位，用于绑定 key / consent
	Scheme     string
	Host       string // 不含端口
	Port       int
	Path       string
	Scope      Scope
}

// NormalizeEndpoint 解析并规范化 base URL。
func NormalizeEndpoint(raw string) (Endpoint, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Endpoint{}, errors.New("endpoint is empty")
	}
	u, err := url.Parse(s)
	if err != nil {
		return Endpoint{}, fmt.Errorf("invalid endpoint: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return Endpoint{}, ErrEndpointScheme
	}
	if u.User != nil {
		return Endpoint{}, ErrEndpointUserInfo
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Endpoint{}, errors.New("endpoint host is empty")
	}
	port := 0
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Endpoint{}, errors.New("invalid endpoint port")
		}
		port = n
	}
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	// 路径：清理重复斜杠/尾部斜杠；根路径统一为空
	p := path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	// EscapedPath 可能带转义；这里只用于展示/绑定，保持可读形式
	if unescaped, err := url.PathUnescape(p); err == nil {
		p = unescaped
	}
	if p == "/" || p == "." {
		p = ""
	}
	p = strings.TrimSuffix(p, "/")

	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	hostForURL := host
	if strings.Contains(host, ":") { // IPv6 需要方括号
		hostForURL = "[" + host + "]"
	}
	normalized := scheme + "://" + hostPort + p
	if (scheme == "http" && port == 80) || (scheme == "https" && port == 443) {
		// 默认端口省略，避免 http://x:80 与 http://x 被视为两个 endpoint
		normalized = scheme + "://" + hostForURL + p
	}
	sum := sha256.Sum256([]byte(normalized))
	return Endpoint{
		Normalized: normalized,
		Hash:       hex.EncodeToString(sum[:])[:16],
		Scheme:     scheme,
		Host:       host,
		Port:       port,
		Path:       p,
		Scope:      classifyHost(host),
	}, nil
}

// classifyHost 按 URL 字面判定网络范围（提示用途，不是安全边界）。
func classifyHost(host string) Scope {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return ScopeLocal
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsLoopback() {
			return ScopeLocal
		}
		if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return ScopeLAN
		}
		return ScopeCloud
	}
	// 常见内网域名后缀按局域网处理；其余主机名保守地按公网处理
	if strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".internal") {
		return ScopeLAN
	}
	return ScopeCloud
}

// ChatCompletionsURL 由 base URL 推导 /chat/completions 地址。
// 支持 base URL 直接写完整端点（以 /chat/completions 结尾）。
func ChatCompletionsURL(raw string) (string, error) {
	ep, err := NormalizeEndpoint(raw)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(ep.Path, "/chat/completions") {
		return ep.Normalized, nil
	}
	return ep.Normalized + "/chat/completions", nil
}

// ModelsURL 推导 /models 地址（测试连接用）。
func ModelsURL(raw string) (string, error) {
	ep, err := NormalizeEndpoint(raw)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(ep.Path, "/chat/completions") {
		trimmed := strings.TrimSuffix(ep.Normalized, "/chat/completions")
		return trimmed + "/models", nil
	}
	return ep.Normalized + "/models", nil
}

// KeyRef 返回 API key 在 keychain 中的引用键。
// 绑定到规范化 endpoint（而非仅 providerId），换 endpoint 不会复用旧 key。
func KeyRef(provider ProviderKind, endpointHash string) string {
	return "ai:" + string(provider) + ":" + endpointHash + ":api_key"
}

// PresetBaseURL 返回 provider 的默认地址（设置弹窗预设）。
func PresetBaseURL(kind ProviderKind) string {
	switch kind {
	case ProviderLMStudio:
		return "http://127.0.0.1:1234/v1"
	case ProviderOpenAICompatible:
		return "https://api.openai.com/v1"
	default:
		return "http://127.0.0.1:11434/v1"
	}
}

// NormalizeConfig 填充默认值并规范化配置，返回 endpoint 与提示性警告。
func NormalizeConfig(cfg Config) (Config, Endpoint, []Warning, error) {
	def := DefaultConfig()
	out := cfg
	if out.Provider == "" {
		out.Provider = def.Provider
	}
	switch out.Provider {
	case ProviderOllama, ProviderLMStudio, ProviderOpenAICompatible:
	default:
		return out, Endpoint{}, nil, fmt.Errorf("unsupported provider: %s", out.Provider)
	}
	out.BaseURL = strings.TrimSpace(out.BaseURL)
	out.Model = strings.TrimSpace(out.Model)
	if out.BaseURL == "" {
		out.BaseURL = PresetBaseURL(out.Provider)
	}
	if out.Temperature < 0 || out.Temperature > 2 {
		return out, Endpoint{}, nil, errors.New("temperature must be between 0 and 2")
	}
	if out.Temperature == 0 {
		out.Temperature = def.Temperature
	}
	if out.TimeoutSec == 0 {
		out.TimeoutSec = def.TimeoutSec
	}
	if out.TimeoutSec < 15 || out.TimeoutSec > 180 {
		return out, Endpoint{}, nil, errors.New("timeout must be between 15 and 180 seconds")
	}
	if out.Concurrency == 0 {
		out.Concurrency = def.Concurrency
	}
	if out.Concurrency < 1 || out.Concurrency > 2 {
		return out, Endpoint{}, nil, errors.New("concurrency must be 1 or 2")
	}
	ep, err := NormalizeEndpoint(out.BaseURL)
	if err != nil {
		return out, Endpoint{}, nil, err
	}
	var warnings []Warning
	if ep.Path == "" {
		warnings = append(warnings, Warning{Code: WarnMissingV1Path})
	}
	if out.AllowInsecureTLS {
		warnings = append(warnings, Warning{Code: WarnInsecureTLS})
	}
	if ep.Scope == ScopeCloud {
		warnings = append(warnings, Warning{Code: WarnPublicEndpoint})
	}
	return out, ep, warnings, nil
}
