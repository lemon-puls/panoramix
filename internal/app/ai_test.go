package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon-puls/panoramix/internal/ai"
	"github.com/lemon-puls/panoramix/internal/db"
	"github.com/lemon-puls/panoramix/internal/store"
)

// ---------- 测试替身 ----------

type recordingEmitter struct {
	mu     sync.Mutex
	events []struct {
		name string
		data []interface{}
	}
}

func (e *recordingEmitter) SetContext(context.Context) {}

func (e *recordingEmitter) Emit(name string, data ...interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, struct {
		name string
		data []interface{}
	}{name, data})
}

func (e *recordingEmitter) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev.name == name {
			n++
		}
	}
	return n
}

type fakeSchema struct {
	tables   []db.TableInfo
	ddls     map[string]string
	views    map[string]bool
	versions string
	err      error
}

func (f *fakeSchema) ListTables(_ store.DBConn, _, _ string) ([]db.TableInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tables, nil
}

func (f *fakeSchema) ShowCreateObject(_ store.DBConn, _, _, table string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	return f.ddls[table], f.views[table], nil
}

func (f *fakeSchema) ServerVersion(_ store.DBConn, _ string) (string, error) {
	return f.versions, nil
}

// fakeClient 可注入的模型客户端：用于不依赖真实 HTTP 的确定性测试。
type fakeClient struct {
	mu      sync.Mutex
	calls   []ai.ChatRequest
	configs []ai.ClientConfig
	resp    string
	err     error
	onDelta string
	started chan struct{}
	release chan struct{}
}

func (f *fakeClient) Generate(ctx context.Context, req ai.ChatRequest, onDelta func(string)) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return "", &ai.ProviderError{Code: ai.CodeCanceled, Message: "canceled"}
		}
	}
	if f.onDelta != "" && onDelta != nil {
		onDelta(f.onDelta)
	}
	return f.resp, f.err
}

func (f *fakeClient) TestConnection(context.Context) error { return f.err }

func (f *fakeClient) lastCall(t *testing.T) ai.ChatRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("fake client was not called")
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeClient) captureConfig(cfg ai.ClientConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs = append(f.configs, cfg)
}

func newAITestService(t *testing.T) (*Service, *memSecrets, *fakeSchema, *recordingEmitter) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sec := newMemSecrets()
	em := &recordingEmitter{}
	svc := NewService(st, sec, em, nil, nil, nil, nil, nil)
	schema := &fakeSchema{
		tables: []db.TableInfo{
			{Name: "users"},
			{Name: "orders"},
			{Name: "v_summary", IsView: true},
		},
		ddls: map[string]string{
			"users":  "CREATE TABLE `users` (`id` bigint NOT NULL AUTO_INCREMENT, `email` varchar(128), PRIMARY KEY (`id`)) ENGINE=InnoDB AUTO_INCREMENT=42",
			"orders": "CREATE TABLE `orders` (`id` bigint, `user_id` bigint, KEY `idx_user` (`user_id`)) ENGINE=InnoDB",
		},
		views:    map[string]bool{},
		versions: "8.0.36",
	}
	svc.ai.schema = schema
	return svc, sec, schema, em
}

func createMySQLConn(t *testing.T, svc *Service, dbName string) store.DBConn {
	t.Helper()
	c, err := svc.SaveDBConn(DBConnInput{DBConn: store.DBConn{
		Name: "demo", Kind: store.DBMySQL, Addr: "127.0.0.1", Port: 3306, DBName: dbName,
	}})
	if err != nil {
		t.Fatalf("save db conn: %v", err)
	}
	return c
}

func saveAIConfig(t *testing.T, svc *Service, baseURL, model string, allowWrite bool, apiKey string) {
	t.Helper()
	err := svc.AISaveConfig(ai.Config{
		Enabled:     true,
		Provider:    ai.ProviderOpenAICompatible,
		BaseURL:     baseURL,
		Model:       model,
		Temperature: 0.1,
		TimeoutSec:  15,
		AllowWrite:  allowWrite,
		Concurrency: 1,
	}, apiKey, false)
	if err != nil {
		t.Fatalf("save ai config: %v", err)
	}
}

func useFakeClient(svc *Service, f *fakeClient) {
	svc.ai.factory = func(cfg ai.ClientConfig) ai.Client {
		f.captureConfig(cfg)
		return f
	}
}

func aiReq(connID string) ai.Request {
	return ai.Request{
		RequestID: "req-1",
		ConnID:    connID,
		Database:  "demo",
		Mode:      ai.ModeGenerate,
		Question:  "最近 7 天未登录且余额大于 0 的用户，按余额倒序",
		Tables:    []string{"users", "orders"},
		Locale:    "zh-CN",
	}
}

// ---------- 两阶段：预览 = 实发 ----------

func TestAIPreviewEqualsActualSend(t *testing.T) {
	var mu sync.Mutex
	var gotSystem, gotUser, gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		gotModel = body.Model
		gotAuth = r.Header.Get("Authorization")
		if len(body.Messages) == 2 {
			gotSystem = body.Messages[0].Content
			gotUser = body.Messages[1].Content
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": "```sql\nSELECT u.id FROM users u;\n```"}}},
		})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	defer srv.Close()

	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	saveAIConfig(t, svc, srv.URL+"/v1", "qwen2.5-coder:7b", false, "sk-local")

	prepared, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.NeedsConsent {
		t.Fatal("loopback endpoint must not require consent")
	}
	if !strings.Contains(prepared.Payload, "CREATE TABLE `users`") {
		t.Fatalf("payload missing schema:\n%s", prepared.Payload)
	}
	if strings.Contains(prepared.Payload, "AUTO_INCREMENT=42") {
		t.Fatal("DDL not sanitized")
	}
	// 预览后篡改 schema：发送内容必须仍然是预览中的版本
	svc.ai.schema.(*fakeSchema).ddls["users"] = "CREATE TABLE `users` (`leaked` varchar(10))"

	res, err := svc.AIGenerate(prepared.PrepareID, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(res.SQL, "SELECT u.id FROM users u") {
		t.Fatalf("sql = %q", res.SQL)
	}
	if !res.InsertAllowed {
		t.Fatalf("read-only SQL should be insertable, reason=%s", res.InsertBlockReason)
	}

	mu.Lock()
	defer mu.Unlock()
	// 从预览文本还原 system/user（build 时写入的格式）
	parts := strings.SplitN(prepared.Payload, "\n\n[user]\n", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected payload format")
	}
	wantSystem := strings.TrimPrefix(parts[0], "[system]\n")
	wantUser := parts[1]
	if gotSystem != wantSystem || gotUser != wantUser {
		t.Fatal("actual sent prompt differs from preview (preview != send)")
	}
	if strings.Contains(gotUser, "leaked") {
		t.Fatal("payload was rebuilt after preview; cached payload must be sent")
	}
	if gotAuth != "Bearer sk-local" {
		t.Fatalf("api key not sent in Authorization header: %q", gotAuth)
	}
	if gotModel != "qwen2.5-coder:7b" {
		t.Fatalf("model = %q", gotModel)
	}
}

func TestAIPrepareSingleUseAndExpiry(t *testing.T) {
	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	fake := &fakeClient{resp: "```sql\nSELECT 1;\n```"}
	useFakeClient(svc, fake)
	saveAIConfig(t, svc, "https://api.example.com/v1", "m", false, "k")

	prepared, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := svc.AIGenerate(prepared.PrepareID, true); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if _, err := svc.AIGenerate(prepared.PrepareID, true); err == nil || !strings.HasPrefix(err.Error(), "prepare_expired") {
		t.Fatalf("cached payload must be single-use, got %v", err)
	}

	svc.ai.prepareTTL = time.Millisecond
	p2, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare 2: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := svc.AIGenerate(p2.PrepareID, true); err == nil || !strings.HasPrefix(err.Error(), "prepare_expired") {
		t.Fatalf("expired preview must be rejected, got %v", err)
	}
}

// ---------- endpoint 绑定：key / consent ----------

func TestAIConsentBoundToEndpoint(t *testing.T) {
	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	fake := &fakeClient{resp: "```sql\nSELECT 1;\n```"}
	useFakeClient(svc, fake)

	saveAIConfig(t, svc, "https://api.example.com/v1", "m", false, "k")
	p1, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !p1.NeedsConsent || p1.Scope != ai.ScopeCloud {
		t.Fatalf("cloud endpoint should require consent: %+v", p1)
	}
	if _, err := svc.AIGenerate(p1.PrepareID, false); err == nil || !strings.HasPrefix(err.Error(), "consent_required") {
		t.Fatalf("expected consent_required, got %v", err)
	}

	p2, _ := svc.AIPrepare(aiReq(conn.ID))
	if _, err := svc.AIGenerate(p2.PrepareID, true); err != nil {
		t.Fatalf("consented generate: %v", err)
	}
	p3, _ := svc.AIPrepare(aiReq(conn.ID))
	if p3.NeedsConsent {
		t.Fatal("consent must be remembered per endpoint")
	}

	// 换 endpoint：需要重新授权
	saveAIConfig(t, svc, "https://api.other.com/v1", "m", false, "k2")
	p4, _ := svc.AIPrepare(aiReq(conn.ID))
	if !p4.NeedsConsent {
		t.Fatal("changing endpoint must invalidate consent")
	}
	if err := svc.AIClearConsents(); err != nil {
		t.Fatalf("clear consents: %v", err)
	}
	p5, _ := svc.AIPrepare(aiReq(conn.ID))
	if !p5.NeedsConsent {
		t.Fatal("consent should remain cleared")
	}
}

func TestAIKeyBoundToEndpointAndDeletedOnSwitch(t *testing.T) {
	svc, sec, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	fake := &fakeClient{resp: "```sql\nSELECT 1;\n```"}
	useFakeClient(svc, fake)

	epA, _ := ai.NormalizeEndpoint("https://api.a.com/v1")
	refA := ai.KeyRef(ai.ProviderOpenAICompatible, epA.Hash)
	saveAIConfig(t, svc, epA.Normalized, "m", false, "key-A")
	if got, _ := sec.GetSecret(refA); got != "key-A" {
		t.Fatalf("key A not stored: %q", got)
	}
	view, err := svc.AIGetConfig()
	if err != nil || !view.HasKey {
		t.Fatalf("expected HasKey=true, view=%+v err=%v", view, err)
	}

	// 切到 B：旧 key 必须被删除，且新请求不能带上 A 的 key
	epB, _ := ai.NormalizeEndpoint("https://api.b.com/v1")
	saveAIConfig(t, svc, epB.Normalized, "m", false, "")
	if _, err := sec.GetSecret(refA); err == nil {
		t.Fatal("old endpoint key must be deleted after switching")
	}
	viewB, _ := svc.AIGetConfig()
	if viewB.HasKey {
		t.Fatal("new endpoint must not inherit old key")
	}
	p, _ := svc.AIPrepare(aiReq(conn.ID))
	if _, err := svc.AIGenerate(p.PrepareID, true); err != nil {
		t.Fatalf("generate B: %v", err)
	}
	fake.mu.Lock()
	cfg := fake.configs[len(fake.configs)-1]
	fake.mu.Unlock()
	if cfg.APIKey != "" {
		t.Fatalf("old endpoint key leaked to new endpoint: %q", cfg.APIKey)
	}

	// 保存 key 后显式删除
	saveAIConfig(t, svc, epB.Normalized, "m", false, "key-B")
	if err := svc.AIDeleteKey(); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	refB := ai.KeyRef(ai.ProviderOpenAICompatible, epB.Hash)
	if _, err := sec.GetSecret(refB); err == nil {
		t.Fatal("AIDeleteKey must remove keyring entry")
	}
}

func TestAIConfigPersistsWithoutKeyInSQLite(t *testing.T) {
	svc, sec, _, _ := newAITestService(t)
	saveAIConfig(t, svc, "https://api.example.com/v1", "m", true, "super-secret-key")

	// key 只在 keychain
	found := false
	for _, v := range sec.m {
		if v == "super-secret-key" {
			found = true
		}
	}
	if !found {
		t.Fatal("api key not stored in keychain")
	}
	// 所有非敏感 settings 都不应包含 key
	for _, k := range []string{
		settingAIEnabled, settingAIProvider, settingAIBaseURL, settingAIModel,
		settingAITemperature, settingAITimeout, settingAIAllowWrite,
		settingAIAllowInsecureTLS, settingAIConcurrency, settingAICloudConsent,
	} {
		if v, _, _ := svc.store.GetSetting(k); strings.Contains(v, "super-secret-key") {
			t.Fatalf("settings %s leaked the api key", k)
		}
	}
	// 配置持久化
	view, _ := svc.AIGetConfig()
	if !view.Enabled || view.Model != "m" || !view.AllowWrite {
		t.Fatalf("config not persisted: %+v", view)
	}
}

// ---------- 权限 / 范围 ----------

func TestAIRedisAndMissingDatabaseRejected(t *testing.T) {
	svc, _, _, _ := newAITestService(t)
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", false, "")
	redisConn, err := svc.SaveDBConn(DBConnInput{DBConn: store.DBConn{
		Name: "cache", Kind: store.DBRedis, Addr: "127.0.0.1", Port: 6379, DBName: "0",
	}})
	if err != nil {
		t.Fatalf("save redis: %v", err)
	}
	req := aiReq(redisConn.ID)
	if _, err := svc.AIPrepare(req); err == nil || !strings.HasPrefix(err.Error(), "redis_unsupported") {
		t.Fatalf("redis must be rejected, got %v", err)
	}

	emptyDB := createMySQLConn(t, svc, "")
	req = aiReq(emptyDB.ID)
	req.Database = ""
	if _, err := svc.AIPrepare(req); err == nil || !strings.HasPrefix(err.Error(), "database_required") {
		t.Fatalf("missing database must be rejected, got %v", err)
	}
}

func TestAIContextTooLargeListsTables(t *testing.T) {
	svc, _, schema, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", false, "")
	schema.ddls["users"] = "CREATE TABLE `users` (" + strings.Repeat("x", 7000) + ")"
	schema.ddls["orders"] = "CREATE TABLE `orders` (" + strings.Repeat("y", 7000) + ")"
	_, err := svc.AIPrepare(aiReq(conn.ID))
	if err == nil || !strings.HasPrefix(err.Error(), "context_too_large") {
		t.Fatalf("expected context_too_large, got %v", err)
	}
	if !strings.Contains(err.Error(), "users=") || !strings.Contains(err.Error(), "orders=") {
		t.Fatalf("error should list per-table sizes: %v", err)
	}
}

func TestAIViewsExcludedFromContext(t *testing.T) {
	svc, _, schema, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", false, "")
	schema.views["v_summary"] = true
	schema.ddls["v_summary"] = "CREATE DEFINER=`admin`@`10.0.0.1` VIEW `v_summary` AS SELECT 1"
	req := aiReq(conn.ID)
	req.Tables = []string{"users", "v_summary"}
	p, err := svc.AIPrepare(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if strings.Contains(p.Payload, "DEFINER") || strings.Contains(p.Payload, "admin") {
		t.Fatal("view DDL/definer must not be sent")
	}
	hasViewWarning := false
	for _, w := range p.Warnings {
		if w.Code == ai.WarnViewExcluded && w.Detail == "v_summary" {
			hasViewWarning = true
		}
	}
	if !hasViewWarning {
		t.Fatalf("missing view_excluded warning: %+v", p.Warnings)
	}
}

// ---------- 响应判定 ----------

func TestAIAutoMatchDoesNotSendUnrequestedSchema(t *testing.T) {
	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", false, "")

	// 问题没有命中任何表：不发 schema，给出 no_context 警告
	req := aiReq(conn.ID)
	req.Tables = nil
	req.Question = "帮我写一个报表"
	p, err := svc.AIPrepare(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if strings.Contains(p.Payload, "CREATE TABLE") {
		t.Fatal("no table matched but schema was sent anyway")
	}
	hasNoContext := false
	for _, w := range p.Warnings {
		if w.Code == ai.WarnNoContext {
			hasNoContext = true
		}
	}
	if !hasNoContext {
		t.Fatalf("expected no_context warning: %+v", p.Warnings)
	}

	// 问题提到 users：只自动带 users，不带无关表
	req.Question = "查询 users 表里最近登录的用户"
	p2, err := svc.AIPrepare(req)
	if err != nil {
		t.Fatalf("prepare 2: %v", err)
	}
	if !strings.Contains(p2.Payload, "CREATE TABLE `users`") {
		t.Fatalf("matched table missing:\n%s", p2.Payload)
	}
	if strings.Contains(p2.Payload, "CREATE TABLE `orders`") {
		t.Fatal("unmatched table should not be auto-sent")
	}
}

func TestAIGenerateRechecksConsentAtSend(t *testing.T) {
	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	fake := &fakeClient{resp: "```sql\nSELECT 1;\n```"}
	useFakeClient(svc, fake)
	saveAIConfig(t, svc, "https://api.example.com/v1", "m", false, "k")

	// 先授权一次，让后续 Prepare 的 NeedsConsent 快照为 false
	p1, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare 1: %v", err)
	}
	if _, err := svc.AIGenerate(p1.PrepareID, true); err != nil {
		t.Fatalf("generate 1: %v", err)
	}
	p2, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare 2: %v", err)
	}
	if p2.NeedsConsent {
		t.Fatal("expected consent to be remembered at prepare time")
	}

	// 预览后清除授权：旧预览不得继续发送
	if err := svc.AIClearConsents(); err != nil {
		t.Fatalf("clear consents: %v", err)
	}
	fake.mu.Lock()
	before := len(fake.calls)
	fake.mu.Unlock()
	if _, err := svc.AIGenerate(p2.PrepareID, false); err == nil || !strings.HasPrefix(err.Error(), "consent_required") {
		t.Fatalf("stale preview must be rejected after consent cleared, got %v", err)
	}
	fake.mu.Lock()
	after := len(fake.calls)
	fake.mu.Unlock()
	if after != before {
		t.Fatal("rejected request must not reach the provider")
	}

	// 用户在新预览上确认后可以重新授权
	p3, _ := svc.AIPrepare(aiReq(conn.ID))
	if !p3.NeedsConsent {
		t.Fatal("new prepare should require consent again")
	}
	if _, err := svc.AIGenerate(p3.PrepareID, true); err != nil {
		t.Fatalf("generate with fresh consent: %v", err)
	}
}

func TestAIGateResizeKeepsInFlightToken(t *testing.T) {
	g := newAIGate(2)
	if !g.tryAcquire() {
		t.Fatal("first acquire should succeed")
	}
	// 有请求在途时缩容：不得丢失在途计数
	g.resize(1)
	g.release() // 不应阻塞
	g.resize(1) // 空闲后缩容生效

	if !g.tryAcquire() {
		t.Fatal("first acquire after resize should succeed")
	}
	if g.tryAcquire() {
		t.Fatal("concurrency limit should be 1 after resize")
	}
	g.release()
}

func TestAIGateResizeWakesWaiter(t *testing.T) {
	g := newAIGate(1)
	if !g.tryAcquire() {
		t.Fatal("first acquire should succeed")
	}
	done := make(chan error, 1)
	go func() { done <- g.acquire(context.Background()) }()
	time.Sleep(30 * time.Millisecond) // 让 waiter 入队
	g.resize(2)                       // 扩容应立即唤醒 waiter
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter acquire: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resize should wake a queued waiter")
	}
	g.release()
	g.release()
}

func TestAIGateAcquireCanceledLeavesNoWaiter(t *testing.T) {
	g := newAIGate(1)
	if !g.tryAcquire() {
		t.Fatal("first acquire should succeed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.acquire(ctx); err == nil {
		t.Fatal("canceled acquire should fail")
	}
	g.release()
	if !g.tryAcquire() {
		t.Fatal("slot should be available after release")
	}
	g.release()
}

func TestAIWriteAndMultiStatementBlocked(t *testing.T) {
	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	fake := &fakeClient{resp: "```sql\nUPDATE users SET email = 'x';\n```"}
	useFakeClient(svc, fake)
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", false, "")

	p, _ := svc.AIPrepare(aiReq(conn.ID))
	res, err := svc.AIGenerate(p.PrepareID, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.InsertAllowed || res.InsertBlockReason != ai.WarnWriteOperation {
		t.Fatalf("write must be blocked when allowWrite=false: %+v", res)
	}

	// 开启允许写操作后：仍带警告，但允许插入（前端仍需人工确认）
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", true, "")
	p2, _ := svc.AIPrepare(aiReq(conn.ID))
	res2, err := svc.AIGenerate(p2.PrepareID, false)
	if err != nil {
		t.Fatalf("generate 2: %v", err)
	}
	if !res2.InsertAllowed || !res2.IsWrite {
		t.Fatalf("write should be insertable when allowWrite=true: %+v", res2)
	}
	hasWriteWarning := false
	for _, w := range res2.Warnings {
		if w.Code == ai.WarnWriteOperation {
			hasWriteWarning = true
		}
	}
	if !hasWriteWarning {
		t.Fatal("write warning must always be present")
	}

	fake.resp = "```sql\nSELECT 1;\nSELECT 2;\n```"
	p3, _ := svc.AIPrepare(aiReq(conn.ID))
	res3, _ := svc.AIGenerate(p3.PrepareID, false)
	if res3.InsertAllowed || res3.InsertBlockReason != ai.WarnMultiStatement {
		t.Fatalf("multi-statements must be blocked: %+v", res3)
	}
}

// ---------- 并发闸门 ----------

func TestAIGlobalConcurrencyLimit(t *testing.T) {
	var inFlight, maxInFlight int32
	var mu sync.Mutex
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		mu.Lock()
		if cur > maxInFlight {
			maxInFlight = cur
		}
		mu.Unlock()
		<-release
		atomic.AddInt32(&inFlight, -1)
		w.Header().Set("Content-Type", "text/event-stream")
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": "```sql\nSELECT 1;\n```"}}},
		})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	defer srv.Close()

	svc, _, _, _ := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	saveAIConfig(t, svc, srv.URL+"/v1", "m", false, "")

	p1, err := svc.AIPrepare(aiReq(conn.ID))
	if err != nil {
		t.Fatalf("prepare 1: %v", err)
	}
	req2 := aiReq(conn.ID)
	req2.RequestID = "req-2" // 不同标签/请求使用独立 ID，不能互相取消
	p2, err := svc.AIPrepare(req2)
	if err != nil {
		t.Fatalf("prepare 2: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		_, err := svc.AIGenerate(p1.PrepareID, false)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := svc.AIGenerate(p2.PrepareID, false)
		errs <- err
	}()
	// 给两个请求时间进入闸门，然后放行
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxInFlight != 1 {
		t.Fatalf("max in-flight requests = %d, want 1", maxInFlight)
	}
}

// ---------- 事件 ----------

func TestAIEmitsStatusAndDelta(t *testing.T) {
	svc, _, _, em := newAITestService(t)
	conn := createMySQLConn(t, svc, "demo")
	fake := &fakeClient{resp: "```sql\nSELECT 1;\n```", onDelta: "SELECT "}
	useFakeClient(svc, fake)
	saveAIConfig(t, svc, "http://127.0.0.1:11434/v1", "m", false, "")

	p, _ := svc.AIPrepare(aiReq(conn.ID))
	if _, err := svc.AIGenerate(p.PrepareID, false); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if em.count("ai:delta") != 1 {
		t.Fatalf("expected one ai:delta event, got %d", em.count("ai:delta"))
	}
	// context(1) + request + parse + done
	if em.count("ai:status") < 3 {
		t.Fatalf("expected status events, got %d", em.count("ai:status"))
	}
}

// ---------- 日志脱敏 ----------

func TestRedactSQLError(t *testing.T) {
	err := fmt.Errorf("exec: Error 1062: Duplicate entry 'alice@example.com' for key 'users.email'")
	got := redactSQLError(err)
	if strings.Contains(got, "alice@example.com") {
		t.Fatalf("row value leaked into log string: %s", got)
	}
	if !strings.Contains(got, "Duplicate entry") {
		t.Fatalf("error class should remain readable: %s", got)
	}
}

func TestAITestConnectionUsesDraftConfig(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		fmt.Fprint(w, `{"data":[{"id":"draft-model"}]}`)
	}))
	defer srv.Close()
	svc, _, _, _ := newAITestService(t)
	// 未保存配置，直接测试草稿
	err := svc.AITestConnection(ai.Config{
		Enabled: true, Provider: ai.ProviderOpenAICompatible,
		BaseURL: srv.URL + "/v1", Model: "draft-model", Temperature: 0.1, TimeoutSec: 15,
	}, "")
	if err != nil {
		t.Fatalf("draft test connection: %v", err)
	}
	if atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("expected one /models request, got %d", requests)
	}
}
