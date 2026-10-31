package main

import (
	"context"
	"embed"
	"log"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lemon-puls/panoramix/internal/app"
	"github.com/lemon-puls/panoramix/internal/ssh"
	"github.com/lemon-puls/panoramix/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

// wailsEmitter 把 internal/app 的 Emitter 接口接到 Wails runtime。
// internal/* 不允许直接 import wails（ARCHITECTURE.md 依赖方向），桥接只在这一处。
type wailsEmitter struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (e *wailsEmitter) SetContext(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ctx = ctx
}

func (e *wailsEmitter) Emit(event string, data ...interface{}) {
	e.mu.RLock()
	ctx := e.ctx
	e.mu.RUnlock()
	if ctx == nil {
		return // 启动完成前的事件直接丢弃
	}
	runtime.EventsEmit(ctx, event, data...)
}

func main() {
	// 打开本地存储（SQLite）；凭据不落库，另存系统 keychain
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		log.Fatalf("[main] open store failed: %v", err)
	}
	defer st.Close()
	log.Printf("[main] store opened: %s", store.DefaultPath())

	// 凭据走 OS keychain（Windows Credential Manager）
	secrets := store.NewKeyringSecretStore("Panoramix")

	// internal/app 是前端唯一数据入口；事件桥在此注入
	em := &wailsEmitter{}

	// SSH 会话管理器：known_hosts 与会话日志放配置目录
	checker := ssh.NewHostKeyChecker(filepath.Join(store.ConfigDir(), "known_hosts"))
	mgr := ssh.NewManager(checker, em,
		filepath.Join(store.ConfigDir(), "known_hosts"),
		filepath.Join(store.ConfigDir(), "logs"))

	svc := app.NewService(st, secrets, em, mgr)

	if err := wails.Run(&options.App{
		Title:            "Panoramix",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 24, G: 24, B: 28, A: 1}, // 深色底，避免启动白屏闪烁
		OnStartup:        svc.Startup,
		OnShutdown:       svc.Shutdown,
		Bind:             []interface{}{svc},
	}); err != nil {
		log.Fatalf("[main] wails run failed: %v", err)
	}
}
