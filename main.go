package main

import (
	"context"
	"embed"
	"log"
	"path/filepath"
	"sync"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lemon-puls/panoramix/internal/app"
	"github.com/lemon-puls/panoramix/internal/sftp"
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
	ctx := e.Ctx()
	if ctx == nil {
		return // 启动完成前的事件直接丢弃
	}
	runtime.EventsEmit(ctx, event, data...)
}

// Ctx 返回当前 runtime 上下文（供对话框等需要 ctx 的桥接使用）
func (e *wailsEmitter) Ctx() context.Context {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ctx
}

// wailsDialoger 把 internal/app 的 Dialoger 接口接到 Wails 原生对话框。
// 与 wailsEmitter 同理：wails 耦合只存在于 main.go。
type wailsDialoger struct {
	em *wailsEmitter
}

func (d *wailsDialoger) PickOpenFile() (string, error) {
	return runtime.OpenFileDialog(d.em.Ctx(), runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
	})
}

func (d *wailsDialoger) PickOpenFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(d.em.Ctx(), runtime.OpenDialogOptions{
		Title: "选择要上传的文件（可多选）",
	})
}

func (d *wailsDialoger) PickSavePath(defaultName string) (string, error) {
	return runtime.SaveFileDialog(d.em.Ctx(), runtime.SaveDialogOptions{
		Title:           "选择保存位置",
		DefaultFilename: defaultName,
	})
}

func (d *wailsDialoger) PickDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(d.em.Ctx(), runtime.OpenDialogOptions{
		Title: "选择下载目录",
	})
}

// OpenFile 用系统默认程序打开文件（远程编辑用）
func (d *wailsDialoger) OpenFile(path string) error {
	return browser.OpenFile(path)
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

	// SFTP 传输队列：复用已连接会话，事件经同一 Emitter 推送
	queue := sftp.NewQueue(mgr, em)

	dlg := &wailsDialoger{em: em}
	// 远程编辑器：下载 → 系统编辑器 → 保存自动回传（冲突检测）
	editor := sftp.NewEditor(mgr, em, dlg, filepath.Join(store.ConfigDir(), "edit"))

	svc := app.NewService(st, secrets, em, mgr, queue, editor, dlg)

	if err := wails.Run(&options.App{
		Title:            "Panoramix",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 24, G: 24, B: 28, A: 1}, // 深色底，避免启动白屏闪烁
		// 文件拖放：未启用时 OnFileDrop 永远不触发（M3 拖拽上传依赖此项）
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true, // 防止 webview 默认行为（拖入文件被直接打开）
			CSSDropProperty:    "--wails-drop-target",
		},
		OnStartup:  svc.Startup,
		OnShutdown: svc.Shutdown,
		Bind:       []interface{}{svc},
	}); err != nil {
		log.Fatalf("[main] wails run failed: %v", err)
	}
}
