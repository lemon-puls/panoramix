package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lemon-puls/panoramix/internal/ai"
	"github.com/lemon-puls/panoramix/internal/db"
	"github.com/lemon-puls/panoramix/internal/store"
)

// aiRuntime AI 助手运行时状态。
// 与 Service 分离，避免把同步原语和缓存混进主服务结构。
type aiRuntime struct {
	schema dbSchema

	factory func(ai.ClientConfig) ai.Client
	now     func() time.Time
	gate    *aiGate

	preparedMu sync.Mutex
	prepared   map[string]*preparedAI
	prepareTTL time.Duration

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	consentMu sync.Mutex
}

// dbSchema 是 app 层从 db.Manager 用到的只读 schema 能力。
// 抽成接口便于确定性测试（无需真实 MySQL）。
type dbSchema interface {
	ListTables(c store.DBConn, password, database string) ([]db.TableInfo, error)
	ShowCreateObject(c store.DBConn, password, database, table string) (string, bool, error)
	ServerVersion(c store.DBConn, password string) (string, error)
}

// preparedAI 两阶段协议缓存项：AIGenerate 只读取这里的 prompt 原文发送，
// 保证“预览 = 实发”。缓存单次使用、5 分钟过期。
type preparedAI struct {
	prepared   *ai.Prepared
	cfg        ai.Config
	endpoint   ai.Endpoint
	keyRef     string
	system     string
	user       string
	requestID  string
	allowWrite bool
	createdAt  time.Time
}

type consentRecord struct {
	Endpoint  string    `json:"endpoint"`
	GrantedAt time.Time `json:"grantedAt"`
}

const (
	settingAIEnabled          = "ai.enabled"
	settingAIProvider         = "ai.provider"
	settingAIBaseURL          = "ai.base_url"
	settingAIModel            = "ai.model"
	settingAITemperature      = "ai.temperature"
	settingAITimeout          = "ai.timeout_sec"
	settingAIAllowWrite       = "ai.allow_write"
	settingAIAllowInsecureTLS = "ai.allow_insecure_tls"
	settingAIConcurrency      = "ai.concurrency"
	settingAICloudConsent     = "ai.cloud_consent"
)

func newAIRuntime(schema dbSchema) *aiRuntime {
	return &aiRuntime{
		schema:     schema,
		factory:    func(cfg ai.ClientConfig) ai.Client { return ai.NewClient(cfg) },
		now:        time.Now,
		gate:       newAIGate(1),
		prepared:   map[string]*preparedAI{},
		prepareTTL: ai.PrepareTTL,
		cancels:    map[string]context.CancelFunc{},
	}
}

// ---------- 配置读写 ----------

// rawAIConfig 读取非敏感配置（不读取 API key）。
func (s *Service) rawAIConfig() (ai.Config, error) {
	cfg := ai.DefaultConfig()
	if v, ok, err := s.store.GetSetting(settingAIEnabled); err != nil {
		return cfg, err
	} else if ok {
		cfg.Enabled = v == "1"
	}
	if v, ok, _ := s.store.GetSetting(settingAIProvider); ok {
		cfg.Provider = ai.ProviderKind(v)
	}
	if v, ok, _ := s.store.GetSetting(settingAIBaseURL); ok && strings.TrimSpace(v) != "" {
		cfg.BaseURL = v
	}
	if v, ok, _ := s.store.GetSetting(settingAIModel); ok {
		cfg.Model = v
	}
	if v, ok, _ := s.store.GetSetting(settingAITemperature); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temperature = f
		}
	}
	if v, ok, _ := s.store.GetSetting(settingAITimeout); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TimeoutSec = n
		}
	}
	if v, ok, _ := s.store.GetSetting(settingAIAllowWrite); ok {
		cfg.AllowWrite = v == "1"
	}
	if v, ok, _ := s.store.GetSetting(settingAIAllowInsecureTLS); ok {
		cfg.AllowInsecureTLS = v == "1"
	}
	if v, ok, _ := s.store.GetSetting(settingAIConcurrency); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Concurrency = n
		}
	}
	return cfg, nil
}

// persistAIConfig 写入非敏感配置（API key 绝不经过这里）。
func (s *Service) persistAIConfig(cfg ai.Config) error {
	pairs := [][2]string{
		{settingAIEnabled, boolSetting(cfg.Enabled)},
		{settingAIProvider, string(cfg.Provider)},
		{settingAIBaseURL, cfg.BaseURL},
		{settingAIModel, cfg.Model},
		{settingAITemperature, strconv.FormatFloat(cfg.Temperature, 'f', -1, 64)},
		{settingAITimeout, strconv.Itoa(cfg.TimeoutSec)},
		{settingAIAllowWrite, boolSetting(cfg.AllowWrite)},
		{settingAIAllowInsecureTLS, boolSetting(cfg.AllowInsecureTLS)},
		{settingAIConcurrency, strconv.Itoa(cfg.Concurrency)},
	}
	for _, p := range pairs {
		if err := s.store.SetSetting(p[0], p[1]); err != nil {
			return fmt.Errorf("save ai setting %s: %w", p[0], err)
		}
	}
	return nil
}

func boolSetting(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// AIGetConfig 返回非敏感配置视图（含 endpoint 判定 / 是否已存 key）。
func (s *Service) AIGetConfig() (*ai.ConfigView, error) {
	raw, err := s.rawAIConfig()
	if err != nil {
		return nil, fmt.Errorf("read ai config: %w", err)
	}
	cfg, ep, warnings, nerr := ai.NormalizeConfig(raw)
	if nerr != nil {
		// 配置损坏时仍返回可编辑的配置，让用户能进设置页修复
		return &ai.ConfigView{
			Config:   raw,
			HasKey:   false,
			Warnings: []ai.Warning{{Code: ai.WarnInvalidEndpoint, Detail: nerr.Error()}},
		}, nil
	}
	keyRef := ai.KeyRef(cfg.Provider, ep.Hash)
	_, kerr := s.secrets.GetSecret(keyRef)
	return &ai.ConfigView{
		Config:       cfg,
		HasKey:       kerr == nil,
		Endpoint:     ep.Normalized,
		EndpointHash: ep.Hash,
		Scope:        ep.Scope,
		CloudConsent: s.hasCloudConsent(ep.Hash),
		Warnings:     warnings,
	}, nil
}

// AISaveConfig 保存配置。apiKey 为空表示不修改；clearKey=true 删除该 endpoint 的 key。
// 切换 endpoint / provider 时会删除旧 endpoint 的 key（总监评审必须修改项 2）。
func (s *Service) AISaveConfig(cfg ai.Config, apiKey string, clearKey bool) error {
	if apiKey != "" && clearKey {
		return fmt.Errorf("cannot set and clear api key at the same time")
	}
	normalized, ep, _, err := ai.NormalizeConfig(cfg)
	if err != nil {
		return err
	}
	// 旧配置：用于清理旧 endpoint 的 key，避免“A 的 key 被发到 B”
	prevRaw, _ := s.rawAIConfig()
	prevEp, prevEpErr := ai.NormalizeEndpoint(prevRaw.BaseURL)
	prevRef := ""
	if prevEpErr == nil {
		prevRef = ai.KeyRef(prevRaw.Provider, prevEp.Hash)
	}
	newRef := ai.KeyRef(normalized.Provider, ep.Hash)
	if prevRef != "" && prevRef != newRef {
		if derr := s.secrets.DeleteSecret(prevRef); derr != nil {
			log.Printf("[ai] delete old api key failed: %v", derr)
		}
	}
	if clearKey {
		if derr := s.secrets.DeleteSecret(newRef); derr != nil {
			return fmt.Errorf("delete api key: %w", derr)
		}
	}
	if apiKey != "" {
		if serr := s.secrets.SaveSecret(newRef, apiKey); serr != nil {
			// 不能静默降级到 SQLite；keychain 不可用要让用户看到
			return fmt.Errorf("save api key to keychain: %w", serr)
		}
	}
	if perr := s.persistAIConfig(normalized); perr != nil {
		return perr
	}
	s.ai.gate.resize(normalized.Concurrency)
	log.Printf("[ai] config saved: provider=%s model=%q scope=%s allow_write=%v",
		normalized.Provider, normalized.Model, ep.Scope, normalized.AllowWrite)
	return nil
}

// AIDeleteKey 删除当前配置 endpoint 对应的 API key（keyring 中不留残留）。
func (s *Service) AIDeleteKey() error {
	raw, err := s.rawAIConfig()
	if err != nil {
		return err
	}
	ep, err := ai.NormalizeEndpoint(raw.BaseURL)
	if err != nil {
		return err
	}
	if err := s.secrets.DeleteSecret(ai.KeyRef(raw.Provider, ep.Hash)); err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	log.Printf("[ai] api key deleted for endpoint scope=%s", ep.Scope)
	return nil
}

// AITestConnection 用草稿配置 + 草稿 key 测试连通性（不回显 key）。
func (s *Service) AITestConnection(cfg ai.Config, apiKey string) error {
	normalized, ep, _, err := ai.NormalizeConfig(cfg)
	if err != nil {
		return err
	}
	if normalized.Model == "" {
		return aiError("model_required", "model is not configured")
	}
	key := apiKey
	if key == "" {
		// 草稿 endpoint 与已保存 endpoint 一致时复用已存 key
		key = s.aiKeyFor(ai.KeyRef(normalized.Provider, ep.Hash))
	}
	client := s.ai.factory(ai.ClientConfig{
		BaseURL:          normalized.BaseURL,
		Model:            normalized.Model,
		APIKey:           key,
		Temperature:      normalized.Temperature,
		Timeout:          time.Duration(normalized.TimeoutSec) * time.Second,
		AllowInsecureTLS: normalized.AllowInsecureTLS,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.TestConnection(ctx); err != nil {
		log.Printf("[ai] test connection failed: code=%s", aiErrorCode(err))
		return err
	}
	log.Printf("[ai] test connection ok: scope=%s", ep.Scope)
	return nil
}

// ---------- 两阶段协议 ----------

// AIPrepare 第一阶段：读取 schema、构建 payload、缓存并返回完整预览。
// 不调用模型；AIGenerate 只发送这里缓存的 prompt 原文。
func (s *Service) AIPrepare(req ai.Request) (*ai.Prepared, error) {
	raw, err := s.rawAIConfig()
	if err != nil {
		return nil, fmt.Errorf("read ai config: %w", err)
	}
	if !raw.Enabled {
		return nil, aiError("ai_disabled", "AI assistant is disabled")
	}
	cfg, ep, _, err := ai.NormalizeConfig(raw)
	if err != nil {
		return nil, aiError("invalid_config", err.Error())
	}
	if cfg.Model == "" {
		return nil, aiError("model_required", "model is not configured")
	}
	if s.ai.schema == nil {
		return nil, aiError("schema_unavailable", "database manager is not ready")
	}
	switch req.Mode {
	case ai.ModeGenerate, ai.ModeExplain, ai.ModeFix:
	default:
		return nil, aiError("invalid_mode", "unsupported AI mode")
	}
	if req.RequestID == "" {
		req.RequestID = store.NewID()
	}
	if len(req.Tables) > ai.MaxContextTables {
		return nil, aiError("too_many_tables", fmt.Sprintf("select at most %d tables", ai.MaxContextTables))
	}
	s.emitAIStatus(req.RequestID, "context")

	conn, pw, err := s.dbConnAndPassword(req.ConnID)
	if err != nil {
		return nil, aiError("connection_not_found", "database connection not found")
	}
	if conn.Kind != store.DBMySQL {
		return nil, aiError("redis_unsupported", "AI assistant only supports MySQL connections")
	}
	database := strings.TrimSpace(req.Database)
	if database == "" {
		database = conn.DBName
	}
	if database == "" {
		return nil, aiError("database_required", "select a database first")
	}

	allTables, err := s.ai.schema.ListTables(conn, pw, database)
	if err != nil {
		return nil, aiError("schema_unavailable", "failed to read table list")
	}
	byName := make(map[string]db.TableInfo, len(allTables))
	for _, t := range allTables {
		byName[strings.ToLower(t.Name)] = t
	}

	var warnings []ai.Warning
	chosen, selWarnings := selectAITables(req, allTables, byName)
	warnings = append(warnings, selWarnings...)
	if len(chosen) == 0 {
		warnings = append(warnings, ai.Warning{Code: ai.WarnNoContext, Detail: database})
	}

	schemas := make([]ai.TableSchema, 0, len(chosen))
	previews := make([]ai.TablePreview, 0, len(chosen))
	for _, t := range chosen {
		ddl, isView, derr := s.ai.schema.ShowCreateObject(conn, pw, database, t.Name)
		if derr != nil {
			return nil, aiError("schema_unavailable", "failed to read DDL for "+t.Name)
		}
		if isView {
			warnings = append(warnings, ai.Warning{Code: ai.WarnViewExcluded, Detail: t.Name})
			previews = append(previews, ai.TablePreview{Name: t.Name, Excluded: true, Reason: "view"})
			continue
		}
		schemas = append(schemas, ai.TableSchema{Name: t.Name, DDL: ddl})
	}

	version, _ := s.ai.schema.ServerVersion(conn, pw)

	built, berr := ai.BuildPayload(ai.BuildInput{
		Mode:         req.Mode,
		Database:     database,
		MySQLVersion: version,
		Question:     req.Question,
		SQL:          req.SQL,
		Error:        req.Error,
		Tables:       schemas,
		Locale:       req.Locale,
	})
	if berr != nil {
		var tooLarge *ai.ErrContextTooLarge
		if errors.As(berr, &tooLarge) {
			parts := make([]string, 0, len(tooLarge.Tables))
			for _, t := range tooLarge.Tables {
				parts = append(parts, fmt.Sprintf("%s=%d", t.Name, t.Chars))
			}
			return nil, aiError("context_too_large",
				fmt.Sprintf("%d/%d chars (%s)", tooLarge.Total, tooLarge.Budget, strings.Join(parts, ", ")))
		}
		return nil, aiError("invalid_request", berr.Error())
	}
	warnings = append(warnings, built.Warnings...)
	warnings = dedupeAIWarnings(warnings)

	// 被排除的视图放在预览里（Excluded=true），让用户知道为什么没进上下文
	previews = append(previews, built.Tables...)
	sort.SliceStable(previews, func(i, j int) bool { return previews[i].Name < previews[j].Name })

	payload := "[system]\n" + built.SystemPrompt + "\n\n[user]\n" + built.UserPrompt
	prepared := &ai.Prepared{
		PrepareID:    store.NewID(),
		RequestID:    req.RequestID,
		Payload:      payload,
		PayloadHash:  ai.PayloadHash(payload),
		PromptChars:  built.PromptChars,
		SchemaChars:  built.SchemaChars,
		Tables:       previews,
		Warnings:     warnings,
		Endpoint:     ep.Normalized,
		EndpointHash: ep.Hash,
		Scope:        ep.Scope,
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		NeedsConsent: ep.Scope != ai.ScopeLocal && !s.hasCloudConsent(ep.Hash),
		ExpiresInSec: int(s.ai.prepareTTL / time.Second),
		AllowWrite:   cfg.AllowWrite,
		Mode:         req.Mode,
	}
	entry := &preparedAI{
		prepared:   prepared,
		cfg:        cfg,
		endpoint:   ep,
		keyRef:     ai.KeyRef(cfg.Provider, ep.Hash),
		system:     built.SystemPrompt,
		user:       built.UserPrompt,
		requestID:  req.RequestID,
		allowWrite: cfg.AllowWrite,
		createdAt:  s.ai.now(),
	}
	s.storePrepared(entry)
	log.Printf("[ai] prepared: id=%s mode=%s scope=%s tables=%d schema_chars=%d",
		req.RequestID, req.Mode, ep.Scope, len(schemas), built.SchemaChars)
	return prepared, nil
}

// selectAITables 按用户显式选择或问题自动匹配选择上下文表。
// 显式选择时视图会被排除并给出警告；自动匹配时静默跳过视图。
func selectAITables(req ai.Request, all []db.TableInfo, byName map[string]db.TableInfo) ([]db.TableInfo, []ai.Warning) {
	var warnings []ai.Warning
	var out []db.TableInfo
	if len(req.Tables) > 0 {
		if len(req.Tables) > ai.MaxContextTables {
			return nil, []ai.Warning{{Code: "too_many_tables"}}
		}
		for _, name := range req.Tables {
			t, ok := byName[strings.ToLower(strings.TrimSpace(name))]
			if !ok {
				warnings = append(warnings, ai.Warning{Code: ai.WarnUnknownTable, Detail: name})
				continue
			}
			if t.IsView {
				warnings = append(warnings, ai.Warning{Code: ai.WarnViewExcluded, Detail: t.Name})
				continue
			}
			out = append(out, t)
		}
		return out, warnings
	}

	// 自动匹配：问题中出现表名则优先；没有命中时不发送任何 schema，
	// 提示用户显式选表（比“猜一组表发出去”更符合最小披露原则）
	q := strings.ToLower(req.Question)
	var matched []db.TableInfo
	for _, t := range all {
		if t.IsView {
			continue
		}
		if q != "" && strings.Contains(q, strings.ToLower(t.Name)) {
			matched = append(matched, t)
		}
	}
	if len(matched) > ai.MaxContextTables {
		matched = matched[:ai.MaxContextTables]
	}
	return matched, warnings
}

// AIGenerate 第二阶段：只发送 AIPrepare 缓存的 prompt 原文。
// consent=true 表示用户已确认向该非本机 endpoint 发送。
func (s *Service) AIGenerate(prepareID string, consent bool) (*ai.Result, error) {
	entry, err := s.takePrepared(prepareID)
	if err != nil {
		return nil, err
	}
	// 发送前复核当前配置：endpoint / model 变了必须重新预览
	raw, rerr := s.rawAIConfig()
	if rerr != nil {
		return nil, fmt.Errorf("read ai config: %w", rerr)
	}
	if !raw.Enabled {
		return nil, aiError("ai_disabled", "AI assistant is disabled")
	}
	cur, curEp, _, nerr := ai.NormalizeConfig(raw)
	if nerr != nil {
		return nil, aiError("invalid_config", nerr.Error())
	}
	if curEp.Hash != entry.endpoint.Hash || cur.Model != entry.cfg.Model {
		return nil, aiError("config_changed", "configuration changed; preview again")
	}
	if entry.prepared.NeedsConsent && !consent {
		return nil, aiError("consent_required", entry.endpoint.Normalized)
	}
	if consent && entry.endpoint.Scope != ai.ScopeLocal {
		s.grantCloudConsent(entry.endpoint.Hash, entry.endpoint.Normalized)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ai.cancelMu.Lock()
	if old := s.ai.cancels[entry.requestID]; old != nil {
		old()
	}
	s.ai.cancels[entry.requestID] = cancel
	s.ai.cancelMu.Unlock()
	defer func() {
		s.ai.cancelMu.Lock()
		delete(s.ai.cancels, entry.requestID)
		s.ai.cancelMu.Unlock()
	}()

	if !s.ai.gate.tryAcquire() {
		s.emitAIStatus(entry.requestID, "queued")
		if err := s.ai.gate.acquire(ctx); err != nil {
			s.emitAIStatus(entry.requestID, "error")
			return nil, aiError("canceled", "request canceled while queued")
		}
	}
	defer s.ai.gate.release()

	s.emitAIStatus(entry.requestID, "request")
	start := s.ai.now()
	key := s.aiKeyFor(entry.keyRef)
	client := s.ai.factory(ai.ClientConfig{
		BaseURL:          entry.cfg.BaseURL,
		Model:            entry.cfg.Model,
		APIKey:           key,
		Temperature:      entry.cfg.Temperature,
		Timeout:          time.Duration(entry.cfg.TimeoutSec) * time.Second,
		AllowInsecureTLS: entry.cfg.AllowInsecureTLS,
	})
	rawResp, genErr := client.Generate(ctx,
		ai.ChatRequest{SystemPrompt: entry.system, UserPrompt: entry.user},
		func(delta string) { s.emitAIDelta(entry.requestID, delta) },
	)
	if genErr != nil {
		s.emitAIStatus(entry.requestID, "error")
		log.Printf("[ai] request failed: id=%s code=%s", entry.requestID, aiErrorCode(genErr))
		return nil, genErr
	}
	s.emitAIStatus(entry.requestID, "parse")

	sqlText, explanation := ai.ParseResponse(rawResp)
	analysis := ai.Analyze(sqlText)
	warnings := append([]ai.Warning{}, entry.prepared.Warnings...)
	if analysis.MultiStatement {
		warnings = append(warnings, ai.Warning{Code: ai.WarnMultiStatement})
	}
	if analysis.IsWrite {
		warnings = append(warnings, ai.Warning{Code: ai.WarnWriteOperation})
	}
	if analysis.Dangerous {
		warnings = append(warnings, ai.Warning{Code: ai.WarnDangerousStatement})
	}
	if analysis.Uncertain {
		warnings = append(warnings, ai.Warning{Code: ai.WarnUncertainStatement})
	}
	selected := map[string]bool{}
	for _, t := range entry.prepared.Tables {
		if !t.Excluded {
			selected[strings.ToLower(t.Name)] = true
		}
	}
	for _, name := range analysis.Tables {
		if !selected[strings.ToLower(name)] {
			warnings = append(warnings, ai.Warning{Code: ai.WarnUnknownTable, Detail: name})
		}
	}
	warnings = dedupeAIWarnings(warnings)

	allowed, blockReason := analysis.InsertPolicy(entry.allowWrite)
	if strings.TrimSpace(sqlText) == "" {
		allowed = false
		blockReason = "no_sql"
	}

	result := &ai.Result{
		SQL:               sqlText,
		Explanation:       explanation,
		Warnings:          warnings,
		Provider:          entry.cfg.Provider,
		Model:             entry.cfg.Model,
		DurationMs:        s.ai.now().Sub(start).Milliseconds(),
		PromptChars:       entry.prepared.PromptChars,
		ResponseChars:     utf8.RuneCountInString(rawResp),
		IsWrite:           analysis.IsWrite,
		MultiStatement:    analysis.MultiStatement,
		Dangerous:         analysis.Dangerous,
		Uncertain:         analysis.Uncertain,
		InsertAllowed:     allowed,
		InsertBlockReason: blockReason,
	}
	s.emitAIStatus(entry.requestID, "done")
	log.Printf("[ai] response parsed: id=%s duration_ms=%d write=%v multi=%v allowed=%v",
		entry.requestID, result.DurationMs, result.IsWrite, result.MultiStatement, result.InsertAllowed)
	return result, nil
}

// AICancel 取消进行中的请求（幂等；未找到也返回 nil）。
func (s *Service) AICancel(requestID string) error {
	s.ai.cancelMu.Lock()
	cancel := s.ai.cancels[requestID]
	s.ai.cancelMu.Unlock()
	if cancel != nil {
		cancel()
		log.Printf("[ai] cancel requested: id=%s", requestID)
	}
	return nil
}

// ---------- 预览缓存 ----------

func (s *Service) storePrepared(entry *preparedAI) {
	s.ai.preparedMu.Lock()
	defer s.ai.preparedMu.Unlock()
	now := s.ai.now()
	for id, e := range s.ai.prepared {
		if now.Sub(e.createdAt) > s.ai.prepareTTL {
			delete(s.ai.prepared, id)
		}
	}
	s.ai.prepared[entry.prepared.PrepareID] = entry
}

func (s *Service) takePrepared(prepareID string) (*preparedAI, error) {
	s.ai.preparedMu.Lock()
	entry, ok := s.ai.prepared[prepareID]
	delete(s.ai.prepared, prepareID) // 单次使用
	s.ai.preparedMu.Unlock()
	if !ok {
		return nil, aiError("prepare_expired", "preview not found or already used")
	}
	if s.ai.now().Sub(entry.createdAt) > s.ai.prepareTTL {
		return nil, aiError("prepare_expired", "preview expired; preview again")
	}
	return entry, nil
}

// ---------- endpoint 授权 ----------

func (s *Service) loadConsents() map[string]consentRecord {
	out := map[string]consentRecord{}
	v, ok, err := s.store.GetSetting(settingAICloudConsent)
	if err != nil || !ok || v == "" {
		return out
	}
	_ = json.Unmarshal([]byte(v), &out)
	return out
}

func (s *Service) hasCloudConsent(endpointHash string) bool {
	_, ok := s.loadConsents()[endpointHash]
	return ok
}

func (s *Service) grantCloudConsent(endpointHash, endpoint string) {
	s.ai.consentMu.Lock()
	defer s.ai.consentMu.Unlock()
	m := s.loadConsents()
	m[endpointHash] = consentRecord{Endpoint: endpoint, GrantedAt: s.ai.now()}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := s.store.SetSetting(settingAICloudConsent, string(data)); err != nil {
		log.Printf("[ai] save consent failed: %v", err)
		return
	}
	log.Printf("[ai] cloud endpoint consent granted: scope=cloud")
}

// AIClearConsents 清除全部云端授权（key 不受影响）。
func (s *Service) AIClearConsents() error {
	if err := s.store.SetSetting(settingAICloudConsent, "{}"); err != nil {
		return fmt.Errorf("clear ai consents: %w", err)
	}
	log.Printf("[ai] cloud consents cleared")
	return nil
}

// ---------- 工具 ----------

func (s *Service) aiKeyFor(ref string) string {
	v, err := s.secrets.GetSecret(ref)
	if err != nil {
		if !errors.Is(err, store.ErrSecretNotFound) {
			log.Printf("[ai] read api key failed: %v", err)
		}
		return ""
	}
	return v
}

func (s *Service) emitAIStatus(requestID, phase string) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit("ai:status", map[string]string{"requestId": requestID, "phase": phase})
}

func (s *Service) emitAIDelta(requestID, text string) {
	if s.emitter == nil || text == "" {
		return
	}
	s.emitter.Emit("ai:delta", map[string]string{"requestId": requestID, "text": text})
}

func aiError(code, msg string) error {
	if msg == "" {
		return errors.New(code)
	}
	return fmt.Errorf("%s: %s", code, msg)
}

func aiErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var pe *ai.ProviderError
	if errors.As(err, &pe) {
		return string(pe.Code)
	}
	s := err.Error()
	if i := strings.Index(s, ":"); i > 0 {
		return s[:i]
	}
	return "error"
}

func dedupeAIWarnings(in []ai.Warning) []ai.Warning {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]ai.Warning, 0, len(in))
	for _, w := range in {
		key := w.Code + "\x00" + w.Detail
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, w)
	}
	return out
}

var sqlQuotedLiteralRe = regexp.MustCompile(`'(?:[^'\\]|\\.|'')*'`)

// redactSQLError 从数据库错误中移除单引号字面量（可能含重复键值、用户名等行数据），
// 再限制长度。只用于日志，不改变返回给前端的原始错误。
func redactSQLError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	s = sqlQuotedLiteralRe.ReplaceAllString(s, "'?'")
	if utf8.RuneCountInString(s) > 300 {
		r := []rune(s)
		s = string(r[:300]) + "…"
	}
	return s
}

// ---------- 全局并发闸门 ----------

// aiGate 限制同时打到模型的请求数（默认 1）。
// 本地 7B 模型对并发极其敏感；多标签同时生成会直接打爆显存。
type aiGate struct {
	ch chan struct{}
	mu sync.Mutex
}

func newAIGate(n int) *aiGate {
	if n < 1 {
		n = 1
	}
	return &aiGate{ch: make(chan struct{}, n)}
}

func (g *aiGate) tryAcquire() bool {
	select {
	case g.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g *aiGate) acquire(ctx context.Context) error {
	select {
	case g.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *aiGate) release() {
	select {
	case <-g.ch:
	default:
	}
}

// resize 在空闲时调整并发上限；忙碌时保持旧容量，下一次空闲调用再调整。
func (g *aiGate) resize(n int) {
	if n < 1 {
		n = 1
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if cap(g.ch) == n {
		return
	}
	select {
	case g.ch <- struct{}{}:
		<-g.ch
		g.ch = make(chan struct{}, n)
	default:
		// 有请求在途，等待后续调用
	}
}
