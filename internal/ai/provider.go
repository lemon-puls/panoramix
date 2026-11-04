package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrorCode provider 错误分类（前端据此给中英文可读文案）
type ErrorCode string

const (
	CodeAuth          ErrorCode = "auth"
	CodeForbidden     ErrorCode = "forbidden"
	CodeNotFound      ErrorCode = "not_found"
	CodeRateLimit     ErrorCode = "rate_limit"
	CodeServer        ErrorCode = "server"
	CodeTimeout       ErrorCode = "timeout"
	CodeCanceled      ErrorCode = "canceled"
	CodeConnection    ErrorCode = "connection"
	CodeInvalidResp   ErrorCode = "invalid_response"
	CodeResponseLarge ErrorCode = "response_too_large"
	CodeRedirect      ErrorCode = "redirect"
	CodeConfig        ErrorCode = "config"
	CodeModelNotFound ErrorCode = "model_not_found"
	CodeProvider      ErrorCode = "provider_error"
)

// ProviderError 带分类的模型调用错误。Message 已经过脱敏与长度限制，
// 不包含 API key、请求体或完整 provider 响应。
type ProviderError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *ProviderError) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

// AsProviderError 提取分类错误；未分类错误归为 connection。
func AsProviderError(err error) *ProviderError {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe
	}
	return &ProviderError{Code: CodeConnection, Message: "request failed"}
}

// 响应体硬上限：2MB（防止恶意/异常 provider 灌爆内存）
const maxResponseBytes = 2 << 20

// ClientConfig 一次客户端调用的完整配置（API key 仅在此结构中短暂存在）
type ClientConfig struct {
	BaseURL          string
	Model            string
	APIKey           string
	Temperature      float64
	MaxTokens        int
	Timeout          time.Duration
	AllowInsecureTLS bool
}

// HTTPClient OpenAI 兼容 /chat/completions 客户端。
// 安全约束（总监评审必须修改项 3）：
//   - 禁止跟随重定向（防止本机 endpoint 307 把 schema 转发到公网）
//   - 禁止 URL userinfo；仅允许 http/https
//   - API key 只放 Authorization header，不进 URL/日志/错误
//   - 响应体 io.LimitReader 限流，校验 content-type
//   - 连接/首包/总超时分离；不读取 HTTP_PROXY（Proxy=nil）
type HTTPClient struct {
	baseURL          string
	model            string
	apiKey           string
	temperature      float64
	maxTokens        int
	timeout          time.Duration
	allowInsecureTLS bool
	httpClient       *http.Client
}

// NewClient 构造客户端。base URL 的合法性在 Generate/TestConnection 时再次校验。
func NewClient(cfg ClientConfig) *HTTPClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	tr := &http.Transport{
		// 明确不使用环境代理：避免 HTTP_PROXY 把请求带离本机/内网
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, // 首包；本地 7B 冷启动可能较慢
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: cfg.AllowInsecureTLS, // 仅显式开启（LAN 自签名证书）
		},
	}
	return &HTTPClient{
		baseURL:          strings.TrimSpace(cfg.BaseURL),
		model:            strings.TrimSpace(cfg.Model),
		apiKey:           cfg.APIKey,
		temperature:      cfg.Temperature,
		maxTokens:        maxTokens,
		timeout:          timeout,
		allowInsecureTLS: cfg.AllowInsecureTLS,
		httpClient: &http.Client{
			Transport: tr,
			// 禁止自动跟随重定向：3xx 原样返回，由调用方判为错误
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatBody struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

type providerErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *providerErrorBody `json:"error"`
}

// Generate 发送一次流式对话请求并返回完整文本。
func (c *HTTPClient) Generate(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error) {
	if c.model == "" {
		return "", &ProviderError{Code: CodeConfig, Message: "model is not configured"}
	}
	if req.SystemPrompt == "" || req.UserPrompt == "" {
		return "", &ProviderError{Code: CodeConfig, Message: "empty prompt"}
	}
	url, err := ChatCompletionsURL(c.baseURL)
	if err != nil {
		return "", &ProviderError{Code: CodeConfig, Message: err.Error()}
	}
	body := chatBody{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
		Temperature: c.temperature,
		MaxTokens:   c.maxTokens,
		Stream:      true,
	}
	content, streamed, err := c.doChat(ctx, url, body, onDelta)
	if err != nil {
		return "", err
	}
	if streamed {
		return content, nil
	}
	// 非流式回退：provider 忽略了 stream 参数
	return content, nil
}

// doChat 发起一次 chat/completions 请求。stream=true 时解析 SSE；
// 若 provider 返回 JSON（忽略 stream），按非流式解析。
func (c *HTTPClient) doChat(ctx context.Context, url string, body chatBody, onDelta func(string)) (string, bool, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", false, &ProviderError{Code: CodeConfig, Message: "encode request failed"}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", false, &ProviderError{Code: CodeConfig, Message: "build request failed"}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if c.apiKey != "" {
		// key 只在 header 中，绝不进 URL / 日志
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", false, mapTransportError(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", false, &ProviderError{Code: CodeRedirect, Message: fmt.Sprintf("provider returned redirect %d (not followed)", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, statusError(resp)
	}

	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	switch {
	case strings.Contains(ct, "text/event-stream"):
		text, err := readSSE(resp.Body, onDelta)
		return text, true, err
	case ct == "" || strings.Contains(ct, "application/json") || strings.Contains(ct, "text/plain"):
		text, err := readJSONResponse(resp.Body)
		return text, false, err
	default:
		return "", false, &ProviderError{Code: CodeInvalidResp, Message: "unexpected content-type from provider"}
	}
}

func readSSE(r io.Reader, onDelta func(string)) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var sb strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // event:/id:/空行等
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk chatResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", &ProviderError{Code: CodeInvalidResp, Message: "malformed stream chunk"}
		}
		if chunk.Error != nil {
			return "", &ProviderError{Code: CodeProvider, Message: redactProviderMessage(chunk.Error.Error.Message)}
		}
		delta := ""
		if len(chunk.Choices) > 0 {
			delta = chunk.Choices[0].Delta.Content
			if delta == "" {
				delta = chunk.Choices[0].Message.Content
			}
		}
		if delta == "" {
			continue
		}
		sb.WriteString(delta)
		if utf8.RuneCountInString(sb.String()) > MaxResponseRunes {
			return "", &ProviderError{Code: CodeResponseLarge, Message: fmt.Sprintf("response exceeds %d characters", MaxResponseRunes)}
		}
		if onDelta != nil {
			onDelta(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", mapTransportError(context.Background(), err)
	}
	if sb.Len() == 0 {
		return "", &ProviderError{Code: CodeInvalidResp, Message: "empty response from provider"}
	}
	return sb.String(), nil
}

func readJSONResponse(r io.Reader) (string, error) {
	data, err := readLimited(r, maxResponseBytes)
	if err != nil {
		return "", err
	}
	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", &ProviderError{Code: CodeInvalidResp, Message: "malformed JSON response"}
	}
	if out.Error != nil {
		return "", &ProviderError{Code: CodeProvider, Message: redactProviderMessage(out.Error.Error.Message)}
	}
	if len(out.Choices) == 0 {
		return "", &ProviderError{Code: CodeInvalidResp, Message: "response contains no choices"}
	}
	content := out.Choices[0].Message.Content
	if content == "" {
		content = out.Choices[0].Delta.Content
	}
	if utf8.RuneCountInString(content) > MaxResponseRunes {
		return "", &ProviderError{Code: CodeResponseLarge, Message: fmt.Sprintf("response exceeds %d characters", MaxResponseRunes)}
	}
	if content == "" {
		return "", &ProviderError{Code: CodeInvalidResp, Message: "response contains no content"}
	}
	return content, nil
}

// readLimited 读取至多 limit 字节；超限返回 response_too_large。
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, mapTransportError(context.Background(), err)
	}
	if int64(len(data)) > limit {
		return nil, &ProviderError{Code: CodeResponseLarge, Message: "provider response is too large"}
	}
	return data, nil
}

func statusError(resp *http.Response) error {
	// 只读取错误信息用于可读提示；限制 8KB，且脱敏后最多 200 字符
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var pe providerErrorBody
	if err := json.Unmarshal(data, &pe); err == nil && pe.Error.Message != "" {
		msg := redactProviderMessage(pe.Error.Message)
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return &ProviderError{Code: CodeAuth, Message: msg}
		case resp.StatusCode == http.StatusForbidden:
			return &ProviderError{Code: CodeForbidden, Message: msg}
		case resp.StatusCode == http.StatusNotFound:
			return &ProviderError{Code: CodeNotFound, Message: msg}
		}
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &ProviderError{Code: CodeAuth, Message: "invalid API key"}
	case resp.StatusCode == http.StatusForbidden:
		return &ProviderError{Code: CodeForbidden, Message: "access denied"}
	case resp.StatusCode == http.StatusNotFound:
		return &ProviderError{Code: CodeNotFound, Message: "endpoint or model not found"}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &ProviderError{Code: CodeRateLimit, Message: "rate limited by provider"}
	case resp.StatusCode == http.StatusBadRequest:
		return &ProviderError{Code: CodeProvider, Message: "provider rejected the request"}
	case resp.StatusCode >= 500:
		return &ProviderError{Code: CodeServer, Message: fmt.Sprintf("provider server error %d", resp.StatusCode)}
	default:
		return &ProviderError{Code: CodeProvider, Message: fmt.Sprintf("provider returned status %d", resp.StatusCode)}
	}
}

func mapTransportError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return &ProviderError{Code: CodeCanceled, Message: "request canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &ProviderError{Code: CodeTimeout, Message: "request timed out"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &ProviderError{Code: CodeTimeout, Message: "request timed out"}
	}
	return &ProviderError{Code: CodeConnection, Message: "cannot connect to provider"}
}

var (
	bearerRe  = regexp.MustCompile(`(?i)(bearer\s+)[^\s"',}]+`)
	skKeyRe   = regexp.MustCompile(`sk-[A-Za-z0-9_-]{6,}`)
	apiKeyRe  = regexp.MustCompile(`(?i)(api[_-]?key["'\s:=]+)[^\s"',}]+`)
	newlineRe = regexp.MustCompile(`[\r\n]+`)
)

// redactProviderMessage 截断并脱敏 provider 返回的错误文字，
// 防止把 key 片段/HTML/超长内容回显到前端或日志。
func redactProviderMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = newlineRe.ReplaceAllString(msg, " ")
	msg = bearerRe.ReplaceAllString(msg, "${1}[REDACTED]")
	msg = skKeyRe.ReplaceAllString(msg, "sk-[REDACTED]")
	msg = apiKeyRe.ReplaceAllString(msg, "${1}[REDACTED]")
	msg = stripControlChars(msg, false)
	if utf8.RuneCountInString(msg) > 200 {
		r := []rune(msg)
		msg = string(r[:200]) + "…"
	}
	return msg
}

// TestConnection 校验 base URL / model / key。
// 优先 GET /models（轻量）；provider 不支持时回退到 max_tokens=1 的最小 completion。
func (c *HTTPClient) TestConnection(ctx context.Context) error {
	if c.model == "" {
		return &ProviderError{Code: CodeConfig, Message: "model is not configured"}
	}
	modelsURL, err := ModelsURL(c.baseURL)
	if err != nil {
		return &ProviderError{Code: CodeConfig, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return &ProviderError{Code: CodeConfig, Message: "build request failed"}
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return mapTransportError(ctx, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		data, err := readLimited(resp.Body, 512<<10)
		if err != nil {
			return err
		}
		var list struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &list); err != nil {
			return &ProviderError{Code: CodeInvalidResp, Message: "malformed models response"}
		}
		if len(list.Data) == 0 {
			return nil // 无法枚举时接受（部分本地 gateway 返回空列表）
		}
		for _, m := range list.Data {
			if m.ID == c.model {
				return nil
			}
		}
		return &ProviderError{Code: CodeModelNotFound, Message: "model not found at provider"}
	case http.StatusUnauthorized, http.StatusForbidden:
		return statusError(resp)
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		// 回退最小 completion
		return c.testCompletion(ctx)
	default:
		return statusError(resp)
	}
}

func (c *HTTPClient) testCompletion(ctx context.Context) error {
	url, err := ChatCompletionsURL(c.baseURL)
	if err != nil {
		return &ProviderError{Code: CodeConfig, Message: err.Error()}
	}
	body := chatBody{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: "Health check. Reply with OK."},
			{Role: "user", Content: "ping"},
		},
		Temperature: 0,
		MaxTokens:   1,
		Stream:      false,
	}
	_, _, err = c.doChat(ctx, url, body, nil)
	return err
}
