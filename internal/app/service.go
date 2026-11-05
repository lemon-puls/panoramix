// Package app 是前端唯一的数据入口层：所有 Wails 绑定方法在此定义，
// 事件推送通过 Emitter 接口注入（协议见 ARCHITECTURE.md）。
// 本包禁止 import wails —— 与 WebView 的耦合只允许存在于 main.go。
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon-puls/panoramix/internal/db"
	"github.com/lemon-puls/panoramix/internal/finalshell"
	"github.com/lemon-puls/panoramix/internal/fsx"
	"github.com/lemon-puls/panoramix/internal/monitor"
	"github.com/lemon-puls/panoramix/internal/sftp"
	"github.com/lemon-puls/panoramix/internal/ssh"
	"github.com/lemon-puls/panoramix/internal/store"
	"github.com/lemon-puls/panoramix/internal/xshell"
)

// Emitter 后端 → 前端事件推送接口；由 main.go 注入 Wails 实现
type Emitter interface {
	SetContext(ctx context.Context)
	Emit(event string, data ...interface{})
}

// Dialoger 原生文件对话框（由 main 注入 Wails 实现；internal/* 不 import wails）
type Dialoger interface {
	PickOpenFile() (string, error)
	PickOpenFiles() ([]string, error)
	PickSavePath(defaultName string) (string, error)
	PickDirectory() (string, error)
}

// Service Wails 绑定服务：前端唯一入口
type Service struct {
	store     store.Store
	secrets   store.SecretStore
	emitter   Emitter
	ssh       *ssh.Manager
	transfers *sftp.Queue
	editor    *sftp.Editor
	dialog    Dialoger
	dbmgr     *db.Manager

	// AI 助手运行时（配置缓存、两阶段预览、并发闸门）；保持 app → ai 单向依赖
	ai *aiRuntime

	samplersMu sync.Mutex
	samplers   map[string]*monitor.Sampler // sessionId → 监控采样器
}

// NewService 组装服务层
func NewService(st store.Store, sec store.SecretStore, em Emitter, mgr *ssh.Manager,
	transfers *sftp.Queue, editor *sftp.Editor, dlg Dialoger, dbmgr *db.Manager) *Service {
	// 避免 typed-nil 接口：dbmgr 为 nil 时 schema 必须是真正的 nil
	var schema dbSchema
	if dbmgr != nil {
		schema = dbmgr
	}
	return &Service{
		store:     st,
		secrets:   sec,
		emitter:   em,
		ssh:       mgr,
		transfers: transfers,
		editor:    editor,
		dialog:    dlg,
		dbmgr:     dbmgr,
		ai:        newAIRuntime(schema),
		samplers:  map[string]*monitor.Sampler{},
	}
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

// buildConnectConfig 组装单台主机的连接配置（含从 keychain 取凭据）。
// 返回非 nil 的 ConnectHostResult 表示组装失败（错误码已归类）。
func (s *Service) buildConnectConfig(h *store.Host, trustMode string, sessionLog, isJump bool) (*ssh.ConnectConfig, *ConnectHostResult) {
	cfg := &ssh.ConnectConfig{
		HostID:        h.ID,
		Name:          h.Name,
		Addr:          h.Addr,
		Port:          h.Port,
		Username:      h.Username,
		AuthType:      string(h.AuthType),
		KeyPath:       h.KeyPath,
		InitCommands:  h.InitCommands,
		LoginScript:   toSSHLoginScript(h.LoginScript),
		KeepaliveSec:  h.KeepaliveSec,
		AutoReconnect: h.AutoReconnect,
		SessionLog:    sessionLog,
		TrustMode:     trustMode,
	}
	// 凭据在连接瞬间从 keychain 解出；Start 返回后由调用方立即清空
	// 目录同步：追加 OSC 7 上报命令（只注入目标主机，跳板机不注入）
	if !isJump && s.cwdSyncEnabled() {
		cfg.InitCommands = append(append([]string{}, h.InitCommands...), cwdSyncCommand)
	}
	switch cfg.AuthType {
	case "password", "keyboard_interactive":
		pw, err := s.secrets.GetSecret(store.HostPasswordRef(h.ID))
		if err != nil {
			if errors.Is(err, store.ErrSecretNotFound) {
				return nil, &ConnectHostResult{ErrorCode: "auth_failed",
					Message: "no password stored for this host"}
			}
			return nil, &ConnectHostResult{ErrorCode: "auth_failed",
				Message: "read credential from keychain failed"}
		}
		cfg.Password = pw
	case "key":
		// passphrase 可选：不存在按无加密私钥处理
		if pp, err := s.secrets.GetSecret(store.HostPassphraseRef(h.ID)); err == nil {
			cfg.Passphrase = pp
		}
	case "agent":
		// 无需凭据：使用本机 SSH agent
	default:
		return nil, &ConnectHostResult{ErrorCode: "connect_failed",
			Message: "unsupported auth type: " + cfg.AuthType}
	}
	return cfg, nil
}

// cwdSyncCommand bash 目录同步注入：每次提示符前用 OSC 7 上报 PWD。
// 文件面板据此跟随终端当前目录（仅 bash；zsh 用户可自行配置 precmd）
const cwdSyncCommand = `export PROMPT_COMMAND='printf "\033]7;file://%s%s\a" "$HOSTNAME" "$PWD"'`

// IsCwdSyncEnabled 目录同步开关（settings 持久化，默认关闭）
func (s *Service) IsCwdSyncEnabled() (bool, error) {
	v, ok, err := s.store.GetSetting("cwd_sync_enabled")
	if err != nil || !ok {
		return false, err
	}
	return v == "1", nil
}

// SetCwdSyncEnabled 开关目录同步（对新连接生效）
func (s *Service) SetCwdSyncEnabled(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	log.Printf("[app] cwd sync enabled=%v", on)
	return s.store.SetSetting("cwd_sync_enabled", v)
}

func (s *Service) cwdSyncEnabled() bool {
	on, err := s.IsCwdSyncEnabled()
	return err == nil && on
}

// toSSHLoginScript 把存储层的登录脚本转换为 ssh 包的结构（依赖方向保持单向）
func toSSHLoginScript(steps []store.LoginStep) []ssh.LoginStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]ssh.LoginStep, 0, len(steps))
	for _, st := range steps {
		if st.Expect == "" {
			continue
		}
		out = append(out, ssh.LoginStep{Expect: st.Expect, Send: st.Send})
	}
	return out
}

// clearSecrets 清空配置及跳板链中的凭据（Start 之后立即调用）
func clearSecrets(cfg *ssh.ConnectConfig) {
	cfg.Password, cfg.Passphrase = "", ""
	for i := range cfg.Jumps {
		cfg.Jumps[i].Password, cfg.Jumps[i].Passphrase = "", ""
	}
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

	cfg, cErr := s.buildConnectConfig(h, trustMode, s.sessionLogEnabled(), false)
	if cErr != nil {
		return *cErr
	}
	// 跳板链：逐台解析（每个跳板用自身凭据；跳板自身的 ProxyChain 不再展开，
	// 避免环与无限递归）
	if len(h.ProxyChain) > 0 {
		byID := make(map[string]*store.Host, len(hosts))
		for i := range hosts {
			byID[hosts[i].ID] = &hosts[i]
		}
		seen := map[string]bool{h.ID: true}
		for i, jumpID := range h.ProxyChain {
			if i >= 3 {
				return ConnectHostResult{ErrorCode: "connect_failed",
					Message: "proxy chain too long (max 3 hops)"}
			}
			jh, ok := byID[jumpID]
			if !ok {
				return ConnectHostResult{ErrorCode: "connect_failed",
					Message: "proxy host not found: " + jumpID}
			}
			if seen[jh.ID] {
				return ConnectHostResult{ErrorCode: "connect_failed",
					Message: "proxy chain contains a cycle"}
			}
			seen[jh.ID] = true
			jcfg, jErr := s.buildConnectConfig(jh, trustMode, false, true)
			if jErr != nil {
				return *jErr
			}
			cfg.Jumps = append(cfg.Jumps, *jcfg)
		}
	}

	if s.ssh == nil {
		return ConnectHostResult{ErrorCode: "connect_failed", Message: "ssh manager not ready"}
	}
	sessionID, startErr := s.ssh.Start(*cfg)
	clearSecrets(cfg)

	if startErr != nil {
		var hke *ssh.HostKeyError
		if errors.As(startErr, &hke) {
			code := "host_key_unknown"
			if hke.Kind == "changed" {
				code = "host_key_changed"
			}
			// 只记主机与指纹，不记任何凭据
			log.Printf("[app] host key verification required: %s %s", hke.HostPort, hke.Fingerprint)
			return ConnectHostResult{ErrorCode: code, Fingerprint: hke.Fingerprint,
				Message: hke.HostPort}
		}
		log.Printf("[app] connect failed: %v", startErr)
		return ConnectHostResult{ErrorCode: "connect_failed", Message: startErr.Error()}
	}

	if terr := s.store.TouchHost(h.ID, time.Now()); terr != nil {
		log.Printf("[app] touch host failed: %v", terr)
	}
	s.startMonitor(h.ID, sessionID)
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
	s.stopMonitor(sessionID)
	return s.ssh.Close(sessionID)
}

// ---------- 系统监控采样 ----------

// startMonitor 启动会话监控采样（间隔可经 settings: monitor_interval_sec 配置，1-60s）
func (s *Service) startMonitor(hostID, sessionID string) {
	if s.ssh == nil {
		return
	}
	interval := 2 * time.Second
	if v, ok, _ := s.store.GetSetting("monitor_interval_sec"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 60 {
			interval = time.Duration(n) * time.Second
		}
	}
	sampler := monitor.NewSampler(s.ssh, s.emitter, hostID, sessionID, interval)
	s.samplersMu.Lock()
	s.samplers[sessionID] = sampler
	s.samplersMu.Unlock()
	go sampler.Start()
	log.Printf("[monitor] sampler started: host=%s session=%s interval=%v", hostID, sessionID, interval)
}

// stopMonitor 停止并摘除采样器
func (s *Service) stopMonitor(sessionID string) {
	s.samplersMu.Lock()
	sampler := s.samplers[sessionID]
	delete(s.samplers, sessionID)
	s.samplersMu.Unlock()
	if sampler != nil {
		sampler.Stop()
	}
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

// ---------- SFTP 文件操作 ----------

// SftpListDir 列远程目录（dir 为空取用户主目录）
func (s *Service) SftpListDir(sessionID, dir string) ([]sftp.RemoteEntry, error) {
	cli, err := sftp.NewSftpClient(s.ssh, sessionID)
	if err != nil {
		log.Printf("[app] sftp open failed: %v", err)
		return nil, fmt.Errorf("sftp open: %w", err)
	}
	defer cli.Close()
	entries, err := sftp.ListDir(cli, dir)
	if err != nil {
		return nil, fmt.Errorf("list dir: %w", err)
	}
	return entries, nil
}

// SftpHome 返回远程用户主目录（文件面板首次打开定位用）
func (s *Service) SftpHome(sessionID string) (string, error) {
	cli, err := sftp.NewSftpClient(s.ssh, sessionID)
	if err != nil {
		return "", fmt.Errorf("sftp open: %w", err)
	}
	defer cli.Close()
	wd, err := cli.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	return wd, nil
}

// SftpMkdir 新建远程目录
func (s *Service) SftpMkdir(sessionID, dir string) error {
	cli, err := sftp.NewSftpClient(s.ssh, sessionID)
	if err != nil {
		return fmt.Errorf("sftp open: %w", err)
	}
	defer cli.Close()
	if err := sftp.Mkdir(cli, dir); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	log.Printf("[app] remote dir created: %s", dir)
	return nil
}

// SftpRemove 删除远程文件/目录（目录递归）
func (s *Service) SftpRemove(sessionID, target string) error {
	cli, err := sftp.NewSftpClient(s.ssh, sessionID)
	if err != nil {
		return fmt.Errorf("sftp open: %w", err)
	}
	defer cli.Close()
	if err := sftp.RemoveAll(cli, target); err != nil {
		return fmt.Errorf("remove %s: %w", target, err)
	}
	log.Printf("[app] remote removed: %s", target)
	return nil
}

// SftpRename 重命名/移动远程条目
func (s *Service) SftpRename(sessionID, oldPath, newPath string) error {
	cli, err := sftp.NewSftpClient(s.ssh, sessionID)
	if err != nil {
		return fmt.Errorf("sftp open: %w", err)
	}
	defer cli.Close()
	if err := sftp.Rename(cli, oldPath, newPath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", oldPath, newPath, err)
	}
	return nil
}

// ---------- 传输队列 ----------

// SftpUpload 上传本地文件/目录（目录递归），返回 batchID
func (s *Service) SftpUpload(sessionID, remoteDir, localPath string) (string, error) {
	if s.transfers == nil {
		return "", fmt.Errorf("transfer queue not ready")
	}
	batchID, err := s.transfers.Enqueue(sessionID, sftp.DirUpload, remoteDir, localPath)
	if err != nil {
		log.Printf("[app] upload enqueue failed: %v", err)
		return "", err
	}
	log.Printf("[app] upload batch started: %s %s -> %s", batchID, localPath, remoteDir)
	return batchID, nil
}

// SftpDownload 下载远程文件/目录（目录递归），localDir 为本地目标目录，返回 batchID
func (s *Service) SftpDownload(sessionID, remotePath, localDir string) (string, error) {
	if s.transfers == nil {
		return "", fmt.Errorf("transfer queue not ready")
	}
	batchID, err := s.transfers.Enqueue(sessionID, sftp.DirDownload, remotePath, localDir)
	if err != nil {
		log.Printf("[app] download enqueue failed: %v", err)
		return "", err
	}
	log.Printf("[app] download batch started: %s %s -> %s", batchID, remotePath, localDir)
	return batchID, nil
}

// ListTransfers 返回全部任务快照（前端打开传输面板时填充）
func (s *Service) ListTransfers() []sftp.Task {
	if s.transfers == nil {
		return nil
	}
	return s.transfers.Tasks()
}

// TransferCancel 取消任务
func (s *Service) TransferCancel(taskID string) error {
	if s.transfers == nil {
		return fmt.Errorf("transfer queue not ready")
	}
	return s.transfers.Cancel(taskID)
}

// TransferRetry 重试失败/取消的任务
func (s *Service) TransferRetry(taskID string) error {
	if s.transfers == nil {
		return fmt.Errorf("transfer queue not ready")
	}
	return s.transfers.Retry(taskID)
}

// PauseTransfer 暂停运行中的传输任务（保留连接与进度）
func (s *Service) PauseTransfer(taskID string) error {
	if s.transfers == nil {
		return fmt.Errorf("transfer queue not ready")
	}
	return s.transfers.Pause(taskID)
}

// ResumeTransfer 恢复已暂停的传输任务
func (s *Service) ResumeTransfer(taskID string) error {
	if s.transfers == nil {
		return fmt.Errorf("transfer queue not ready")
	}
	return s.transfers.Resume(taskID)
}

// SetTransferSpeedLimit 设置全局限速（KB/s，0=不限）；持久化到 settings
func (s *Service) SetTransferSpeedLimit(kbps int) error {
	if s.transfers == nil {
		return fmt.Errorf("transfer queue not ready")
	}
	if kbps < 0 {
		kbps = 0
	}
	s.transfers.SetSpeedLimit(kbps)
	if err := s.store.SetSetting("transfer_speed_limit_kbps", strconv.Itoa(kbps)); err != nil {
		return fmt.Errorf("save setting: %w", err)
	}
	log.Printf("[app] transfer speed limit set to %d KB/s", kbps)
	return nil
}

// GetTransferSpeedLimit 读取当前全局限速（KB/s）
func (s *Service) GetTransferSpeedLimit() (int, error) {
	v, ok, err := s.store.GetSetting("transfer_speed_limit_kbps")
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	n, _ := strconv.Atoi(v)
	return n, nil
}

// SetTransferPolicy 覆盖冲突策略（applyAll=true 记住整批）
func (s *Service) SetTransferPolicy(batchID, mode string, applyAll bool) error {
	if s.transfers == nil {
		return fmt.Errorf("transfer queue not ready")
	}
	return s.transfers.SetTransferPolicy(batchID, mode, applyAll)
}

// ---------- 文件对话框 ----------

// PickUploadFile 打开"选择文件"对话框，返回本地路径
func (s *Service) PickUploadFile() (string, error) {
	if s.dialog == nil {
		return "", fmt.Errorf("dialog not ready")
	}
	return s.dialog.PickOpenFile()
}

// PickUploadFiles 打开"选择文件"多选对话框，返回本地路径列表
func (s *Service) PickUploadFiles() ([]string, error) {
	if s.dialog == nil {
		return nil, fmt.Errorf("dialog not ready")
	}
	return s.dialog.PickOpenFiles()
}

// PickSavePath 打开"另存为"对话框（默认文件名取自远程文件），返回本地路径
func (s *Service) PickSavePath(defaultName string) (string, error) {
	if s.dialog == nil {
		return "", fmt.Errorf("dialog not ready")
	}
	return s.dialog.PickSavePath(defaultName)
}

// PickDir 打开目录选择对话框（目录下载用）
func (s *Service) PickDir() (string, error) {
	if s.dialog == nil {
		return "", fmt.Errorf("dialog not ready")
	}
	return s.dialog.PickDirectory()
}

// ---------- 数据库连接（MySQL / Redis） ----------

// DBConnInput 新建/编辑数据库连接的入参。
// Password 仅用于写入 keychain，写入后立即清空（与 HostInput 同语义）。
type DBConnInput struct {
	store.DBConn
	Password string `json:"password"`
}

func (in DBConnInput) String() string {
	return fmt.Sprintf("{db:%s kind:%s addr:%s:%d user:%s db:%s password:***}",
		in.Name, in.Kind, in.Addr, in.Port, in.Username, in.DBName)
}

func (in DBConnInput) GoString() string { return in.String() }

// ListDBConns 返回全部数据库连接
func (s *Service) ListDBConns() ([]store.DBConn, error) {
	conns, err := s.store.ListDBConns()
	if err != nil {
		return nil, fmt.Errorf("list db conns: %w", err)
	}
	for i := range conns {
		if _, serr := s.secrets.GetSecret(store.DBPasswordRef(conns[i].ID)); serr == nil {
			conns[i].HasSecret = true
		}
	}
	return conns, nil
}

// SaveDBConn 新建（ID 为空）或更新数据库连接；密码留空表示不修改
func (s *Service) SaveDBConn(in DBConnInput) (store.DBConn, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Addr = strings.TrimSpace(in.Addr)
	if in.Name == "" || in.Addr == "" {
		return in.DBConn, fmt.Errorf("name and address are required")
	}
	if in.Kind != store.DBMySQL && in.Kind != store.DBRedis {
		return in.DBConn, fmt.Errorf("unsupported kind: %s", in.Kind)
	}
	if in.ID == "" {
		in.ID = store.NewID()
	}
	if in.Password != "" {
		if err := s.secrets.SaveSecret(store.DBPasswordRef(in.ID), in.Password); err != nil {
			return in.DBConn, fmt.Errorf("save password: %w", err)
		}
	}
	in.Password = ""
	c := in.DBConn
	if err := s.store.SaveDBConn(&c); err != nil {
		return c, fmt.Errorf("save db conn: %w", err)
	}
	if _, serr := s.secrets.GetSecret(store.DBPasswordRef(c.ID)); serr == nil {
		c.HasSecret = true
	}
	log.Printf("[app] db conn saved: id=%s name=%q kind=%s", c.ID, c.Name, c.Kind)
	return c, nil
}

// DeleteDBConn 删除数据库连接及其凭据
func (s *Service) DeleteDBConn(id string) error {
	if err := s.store.DeleteDBConn(id); err != nil {
		return fmt.Errorf("delete db conn: %w", err)
	}
	_ = s.secrets.DeleteSecret(store.DBPasswordRef(id))
	if s.dbmgr != nil {
		s.dbmgr.CloseConn(id)
	}
	return nil
}

// dbConnAndPassword 取连接配置与密码（密码仅内存）
func (s *Service) dbConnAndPassword(id string) (store.DBConn, string, error) {
	conns, err := s.store.ListDBConns()
	if err != nil {
		return store.DBConn{}, "", err
	}
	for _, c := range conns {
		if c.ID == id {
			pw, _ := s.secrets.GetSecret(store.DBPasswordRef(c.ID))
			return c, pw, nil
		}
	}
	return store.DBConn{}, "", fmt.Errorf("db connection not found")
}

// TestDBConn 测试数据库连接
func (s *Service) TestDBConn(id string) error {
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return err
	}
	if c.Kind == store.DBRedis {
		return db.TestRedis(c, pw)
	}
	return db.Test(c, pw)
}

// DBQuery 执行 SQL（MySQL）。查询返回结果集（最多 limit 行），DML 返回影响行数
func (s *Service) DBQuery(id, sqlText string, limit int) (*db.Result, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	res, qerr := s.dbmgr.Query(c, pw, sqlText, limit)
	if qerr != nil {
		// 错误里可能包含重复键值等行数据，先脱敏再写日志（AGENTS.md 硬性要求）
		log.Printf("[app] db query failed: %s", redactSQLError(qerr))
		return nil, qerr
	}
	return res, nil
}

// DBUpdateRows 按主键批量更新数据标签的未提交修改。
// 前端只传结构化的主键与列值；SQL 由 Go 侧参数化生成，整批放在一个事务内，
// 任一语句失败即全部回滚。日志只记录表名/行数与脱敏后的错误，不记录值。
func (s *Service) DBUpdateRows(id, database, table string, updates []db.RowUpdate) (int64, error) {
	if s.dbmgr == nil {
		return 0, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return 0, err
	}
	applied, uerr := s.dbmgr.UpdateRows(c, pw, database, table, updates)
	if uerr != nil {
		log.Printf("[app] db update rows failed: table=%s rows=%d err=%s",
			table, len(updates), redactSQLError(uerr))
		return 0, uerr
	}
	log.Printf("[app] db update rows ok: table=%s rows=%d", table, len(updates))
	return applied, nil
}

// DBListDatabases 列出数据库（MySQL）
func (s *Service) DBListDatabases(id string) ([]string, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.ListDatabases(c, pw)
}

// DBListTables 列出指定库的表与视图
func (s *Service) DBListTables(id, database string) ([]db.TableInfo, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.ListTables(c, pw, database)
}

// DBDescribeTable 返回列定义
func (s *Service) DBDescribeTable(id, database, table string) ([]db.ColumnInfo, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.DescribeTable(c, pw, database, table)
}

// DBTableMeta 返回表基本信息
func (s *Service) DBTableMeta(id, database, table string) (*db.TableMeta, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.TableMetaInfo(c, pw, database, table)
}

// DBListIndexes 列出索引
func (s *Service) DBListIndexes(id, database, table string) ([]db.IndexInfo, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.ListIndexes(c, pw, database, table)
}

// DBListForeignKeys 列出外键
func (s *Service) DBListForeignKeys(id, database, table string) ([]db.ForeignKeyInfo, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.ListForeignKeys(c, pw, database, table)
}

// DBShowCreateTable 返回建表语句
func (s *Service) DBShowCreateTable(id, database, table string) (string, error) {
	if s.dbmgr == nil {
		return "", fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return "", err
	}
	return s.dbmgr.ShowCreateTable(c, pw, database, table)
}

// RedisScanKeys 渐进扫描键（cursor=0 从头开始；返回 Cursor=0 表示结束）
func (s *Service) RedisScanKeys(id string, cursor uint64, pattern string, count int) (*db.KeyPage, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.RedisScan(c, pw, cursor, pattern, count)
}

// RedisGetValue 读取键值详情
func (s *Service) RedisGetValue(id, key string) (*db.Value, error) {
	if s.dbmgr == nil {
		return nil, fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return nil, err
	}
	return s.dbmgr.RedisValue(c, pw, key)
}

// RedisSetTTL 设置键过期（秒；0 表示移除过期）
func (s *Service) RedisSetTTL(id, key string, seconds int64) error {
	if s.dbmgr == nil {
		return fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return err
	}
	return s.dbmgr.RedisSetTTL(c, pw, key, seconds)
}

// RedisDeleteKey 删除键
func (s *Service) RedisDeleteKey(id, key string) error {
	if s.dbmgr == nil {
		return fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return err
	}
	return s.dbmgr.RedisDelete(c, pw, key)
}

// RedisSetString 修改 string 类型值（keepTTL 保留原过期时间）
func (s *Service) RedisSetString(id, key, value string, keepTTL bool) error {
	if s.dbmgr == nil {
		return fmt.Errorf("db manager not ready")
	}
	c, pw, err := s.dbConnAndPassword(id)
	if err != nil {
		return err
	}
	return s.dbmgr.RedisSetString(c, pw, key, value, keepTTL)
}

// ---------- 本地文件浏览（双面板） ----------

// LocalHome 返回用户主目录
func (s *Service) LocalHome() string {
	return fsx.Home()
}

// LocalListDir 列本地目录（dir 为空取主目录）
func (s *Service) LocalListDir(dir string) ([]fsx.Entry, error) {
	return fsx.ListDir(dir)
}

// LocalParent 返回父目录
func (s *Service) LocalParent(dir string) string {
	return fsx.Parent(dir)
}

// LocalRoots 返回可用盘符/根（Windows 为 C:\、D:\ 等，Unix 为 /）
func (s *Service) LocalRoots() []string {
	return fsx.Roots()
}

// ---------- 远程编辑 ----------

// StartRemoteEdit 下载远程文件到临时目录并用系统编辑器打开
func (s *Service) StartRemoteEdit(sessionID, remotePath string) (sftp.EditInfo, error) {
	if s.editor == nil {
		return sftp.EditInfo{}, fmt.Errorf("editor not ready")
	}
	return s.editor.StartEdit(sessionID, remotePath)
}

// FinishRemoteEdit 结束编辑会话并清理临时文件
func (s *Service) FinishRemoteEdit(editID string) error {
	if s.editor == nil {
		return fmt.Errorf("editor not ready")
	}
	return s.editor.FinishEdit(editID)
}

// ResolveEditConflict 处理编辑冲突：overwrite=true 覆盖远程，false 重新加载远程版本
func (s *Service) ResolveEditConflict(editID string, overwrite bool) error {
	if s.editor == nil {
		return fmt.Errorf("editor not ready")
	}
	return s.editor.ResolveConflict(editID, overwrite)
}

// ListRemoteEdits 返回全部编辑会话（前端打开面板时填充）
func (s *Service) ListRemoteEdits() []sftp.EditInfo {
	if s.editor == nil {
		return nil
	}
	return s.editor.ListEdits()
}

// ---------- 第三方客户端导入（Xshell / FinalShell） ----------

// ImportPreview 统一的导入预览项（两种来源归一，前端只处理一种结构）
type ImportPreview struct {
	Source     string `json:"source"` // xshell | finalshell
	File       string `json:"file"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	KeyName    string `json:"keyName,omitempty"`
	HasProxy   bool   `json:"hasProxy,omitempty"` // FinalShell 配置了跳板，需导入后手动配置跳板链
	Valid      bool   `json:"valid"`
	SkipReason string `json:"skipReason,omitempty"`
}

// ScanImportDir 扫描第三方客户端配置目录，返回可导入预览列表。
// source: "xshell"（会话目录 *.xsh）| "finalshell"（conn/*_connect_config.json）
func (s *Service) ScanImportDir(source, dir string) ([]ImportPreview, error) {
	switch source {
	case "xshell":
		list, err := xshell.ScanDir(dir)
		if err != nil {
			return nil, err
		}
		out := make([]ImportPreview, 0, len(list))
		for _, p := range list {
			out = append(out, ImportPreview{
				Source: "xshell", File: p.File, Name: p.Name, Host: p.Host,
				Port: p.Port, Username: p.Username, AuthType: p.AuthType,
				KeyName: p.KeyName, Valid: p.Valid, SkipReason: p.SkipReason,
			})
		}
		return out, nil
	case "finalshell":
		list, err := finalshell.ScanDir(dir)
		if err != nil {
			return nil, err
		}
		out := make([]ImportPreview, 0, len(list))
		for _, p := range list {
			out = append(out, ImportPreview{
				Source: "finalshell", File: p.File, Name: p.Name, Host: p.Host,
				Port: p.Port, Username: p.Username, AuthType: p.AuthType,
				KeyName: p.KeyName, HasProxy: p.HasProxy,
				Valid: p.Valid, SkipReason: p.SkipReason,
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown import source: %s", source)
	}
}

// ImportHosts 将选中的有效预览导入为主机（同名同地址去重跳过）。
// 两家客户端的密码/密钥均为本机私有加密格式，不可迁移：导入的主机无凭据，
// 首次连接时前端引导补录。
func (s *Service) ImportHosts(previews []ImportPreview) (int, error) {
	existing, err := s.store.ListHosts()
	if err != nil {
		return 0, fmt.Errorf("list hosts: %w", err)
	}
	seen := map[string]bool{}
	for _, h := range existing {
		seen[h.Name+"|"+h.Addr] = true
	}
	imported := 0
	for _, p := range previews {
		if !p.Valid || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Host) == "" {
			continue
		}
		key := p.Name + "|" + p.Host
		if seen[key] {
			continue
		}
		auth := store.AuthType(p.AuthType)
		if auth != store.AuthPassword && auth != store.AuthKey && auth != store.AuthKeyboardInteractive {
			auth = store.AuthPassword
		}
		h := store.Host{
			Name:          p.Name,
			Addr:          p.Host,
			Port:          p.Port,
			Username:      p.Username,
			AuthType:      auth,
			KeepaliveSec:  30,
			AutoReconnect: true,
		}
		if err := s.store.SaveHost(&h); err != nil {
			return imported, fmt.Errorf("save host %s: %w", p.Name, err)
		}
		seen[key] = true
		imported++
	}
	log.Printf("[app] import: %d imported of %d previews", imported, len(previews))
	return imported, nil
}
