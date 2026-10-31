# AGENTS.md —— 项目开发规范

## 项目概况

- 项目名：**Panorama**（一体化开发者运维客户端：SSH 终端 / 系统监控 / SFTP / MySQL / Redis / K3s）
- 本目录名 `vista` 是历史目录名，仅作为本地路径使用；代码、文档、构建产物一律使用 Panorama
- Go module 路径暂用 `panorama`，GitHub 仓库建立后统一改为 `github.com/<owner>/panorama`
- 产品决策、里程碑与验收标准见 `ROADMAP.md`；架构与模块边界见 `ARCHITECTURE.md`。
  本文件只约束工程规范。

## 基础规范

1. 代码中适当添加注释，注释使用中文
2. 代码中关键位置必须打印日志，日志使用英文
3. 日志与错误信息中**严禁**出现密码、私钥内容、passphrase、token 等敏感信息
   （`internal/store` 有专门的脱敏测试保障）

## 技术选型（已定，不要替换）

| 层 | 选型 |
| --- | --- |
| Go | 1.27.x（低版本工具链由 go.mod `toolchain` 指令自动拉取） |
| 桌面框架 | **Wails v2**（禁用 v3 alpha） |
| 前端 | Vue 3 + TypeScript + Vite + Pinia + vue-i18n + **Naive UI**（明暗双主题，默认深色） |
| 终端 | `@xterm/xterm` v5 + addon-fit / webgl / search |
| SSH | `golang.org/x/crypto/ssh`（含 knownhosts） |
| SFTP | `github.com/pkg/sftp` |
| 存储 | `modernc.org/sqlite`（纯 Go、无 CGO） |
| 凭据 | `zalando/go-keyring`；keychain 不可用时回退主密码 AES-GCM |
| 图表（v0.2 起） | uPlot（禁用 ECharts 于监控面板） |
| SQL 编辑器（v0.3 起） | CodeMirror 6（禁用 Monaco） |

## 代码组织

```
panorama/
├── main.go  app.go  wails.json
├── internal/
│   ├── store/        # SQLite + keyring（唯一持久化层）
│   ├── ssh/          # M2：SSH 会话池、认证、TOFU、keepalive
│   ├── sftp/         # M3：传输队列
│   └── app/          # Wails 绑定服务层 —— 前端唯一数据入口
└── frontend/         # Vue3 + Vite + TS（wailsjs/ 下的生成代码允许提交）
```

- `internal/*` 各包之间**不得横向 import**，依赖方向只允许：`app → ssh/sftp/store`
- 前端不直接接触任何后端包，只能调用 `internal/app` 暴露的绑定方法
- 后端 → 前端的数据推送一律走 Wails Events（协议见 ARCHITECTURE.md）
- 不做插件化 / 多 module / 微内核

## 测试与验收

1. 每个模块接口先行，配套验收测试；`go test ./internal/...` 必须不依赖 WebView 可独立运行
2. 提交前必须通过：`go vet ./...` 与 `go test ./...`
3. Windows 产物编译校验：`GOOS=windows go build ./...`

## 代码提交规范

1. 完成代码编写后，你需要自行提交代码；如果还没有专门分支，则需创建新分支，
   分支命名示例：`feat/xxx-xxx-xxx`、`hotfix/xxx-xxx` 等
2. 提交信息遵循 Conventional Commits 规范，提交信息使用中文并且内容要写详细
3. 提交要原子性，不要把多个功能修改并在一个提交
4. 版本策略：SemVer 0.x，每个里程碑（M1–M4 / v0.2+）打 tag

## 开源基建（已定）

- License：MIT；GitHub 主仓库 + Gitee 镜像（手动同步）
- v0.x 零遥测；不做自动更新器；v0.x 不购买代码签名证书
