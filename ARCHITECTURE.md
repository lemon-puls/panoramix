# ARCHITECTURE —— 架构与模块边界

> 定稿依据：2026-10-31 设计拷问记录。改架构先改本文。

## 总览

单进程 Wails v2 应用：Go 后端 + 系统 WebView（前端 Vue3）。前端只做渲染与交互，
所有 I/O（SSH/SFTP/DB/监控）都在 Go 侧。

```
┌──────────────── WebView (Vue3 + Naive UI) ────────────────┐
│  绑定方法调用（请求-响应）        Wails Events（推送订阅）   │
└───────────────┬───────────────────────────┬───────────────┘
                │                           │
┌───────────────▼───────────────────────────▼───────────────┐
│                internal/app  (Wails 绑定服务层)             │
│      前端唯一入口；参数校验、ID 生成、凭据引用转发             │
└───┬──────────────┬──────────────┬─────────────────┬───────┘
    │              │(M2)          │(M3)             │(v0.2+)
┌───▼────┐   ┌─────▼─────┐  ┌─────▼─────┐   ┌──────▼──────┐
│ store  │   │   ssh     │  │   sftp    │   │ monitor/db/ │
│SQLite+ │   │ 会话池/认证 │  │  传输队列  │   │ ai/k8s(预留) │
│keyring │   │ TOFU/PTY  │  │           │   │             │
└────────┘   └───────────┘  └───────────┘   └─────────────┘
```

**依赖方向**：`app → ssh / sftp / store / monitor / db / ai / xshell / fsx`；
`internal/*` 禁止横向 import、禁止反向 import。`internal/ai` 只依赖标准库，
不 import 任何 internal 包（schema 以中性 DTO 传入），因此不会破坏横向依赖约束。
`ssh` 与 `sftp` 共享一个会话时由 `app` 层编排（sftp 挂在 ssh 会话之上），不直接互引。

## 目录结构

```
├── main.go            # Wails 启动、资产嵌入、Emitter 桥接（全仓库唯一 import wails 的 Go 文件）
├── wails.json
├── internal/
│   ├── store/         # models / Store 接口 / sqlite 实现 / SecretStore(keyring)
│   ├── ssh/           # M2：SessionPool、Auth、TOFU(knownhosts)、PTY 读写
│   ├── sftp/          # M3：TransferQueue（goroutine 池、可暂停架构）
│   ├── ai/            # v0.3.1：OpenAI 兼容客户端、payload 构造、DDL 脱敏、SQL 判定
│   └── app/           # Service：全部绑定方法 + 事件发射
└── frontend/
    └── src/
        ├── i18n/      # vue-i18n（第一天生效，v0.1 仅 zh-CN）
        ├── stores/    # Pinia
        ├── views/     # 连接管理 / 终端 / 传输 …
        └── components/
```

## 前后端通信协议

### 绑定方法（前端 → 后端，请求-响应）

全部定义在 `internal/app.Service` 上，命名 `<资源><动作>`：

| 方法 | 说明 |
| --- | --- |
| `Ping() string` | 桥接自检 |
| `ListGroups / SaveGroup / DeleteGroup` | 分组 CRUD |
| `ListHosts / SaveHost(HostInput) / DeleteHost` | 主机 CRUD；`HostInput` 内含密码/passphrase，service 转交 SecretStore 后即清空，不落库不入日志 |
| `SetHostSecret / DeleteHostSecret / HasHostSecret` | 凭据独立管理（表单"留空=不修改"语义） |
| `ListSnippets / SaveSnippet / DeleteSnippet` | 片段 CRUD |

M2 已追加：`ConnectHost(hostId, trustMode)`（trustMode: 空 / `add` / `replace`，
对应 TOFU 的"信任未知指纹 / 确认指纹变更"二次连接）、`CloseSession /
WriteSession / ResizeSession / IsSessionLogEnabled / SetSessionLogEnabled`；
v0.2 追加：`ScanImportDir(source, dir) / ImportHosts(previews)`（Xshell 与
FinalShell 统一导入）、`IsSessionLogEnabled / SetSessionLogEnabled`；
M3 已追加：`SftpListDir / SftpHome / SftpMkdir / SftpRemove / SftpRename /
SftpUpload / SftpDownload / ListTransfers / TransferCancel / TransferRetry /
SetTransferPolicy / PickUploadFile / PickSavePath / PickDir`（后三个经
`app.Dialoger` 接口走 Wails 原生对话框）；

v0.3.1 追加（AI SQL 助手，两阶段协议）：
`AIGetConfig / AISaveConfig(cfg, apiKey, clearKey) / AIDeleteKey / AITestConnection(cfg, apiKey)`
管理 provider 配置与 keychain；`AIPrepare(req)` 只读 schema、构建并缓存 payload，
返回 `{prepareId, payload, payloadHash, chars, warnings, needsConsent}`；
`AIGenerate(prepareId, consent)` 只发送缓存原文（单次使用、5 分钟过期），
因此“预览 = 实发”；`AICancel(requestId)` 取消进行中的请求；`AIClearConsents` 清除
按 endpoint 记录的非本机（LAN/云端）授权。

### 事件（后端 → 前端，Wails Events）

| 事件 | 载荷 | 时机 |
| --- | --- | --- |
| `conn:state` | `{hostId, sessionId, state}` | connecting / connected / reconnecting / closed / error |
| `terminal:output` | `{sessionId, dataB64}` | PTY 读端有数据；**合并发送：≤16ms 一批**（xterm.js write 节流，二进制走 base64 保安全） |
| `terminal:exit` | `{sessionId, code, reason}` | 会话结束 |
| `transfer:progress` | `Task` 快照（含 state/speedBps/err） | 传输队列节流推送（≥200ms 间隔）与状态变更 |
| `transfer:conflict` | `{transferId, batchId, sessionId, direction, name, path, size}` | 目标已存在且批次无记住策略时，任务挂起等待前端决策 |
| `monitor:sample` | `{hostId, metrics}` | v0.2，按采样间隔 |
| `ai:status` | `{requestId, phase}` | v0.3.1，AI 请求阶段；phase = `context` / `queued` / `request` / `parse` / `done` / `error`。不含 prompt/响应内容 |
| `ai:delta` | `{requestId, text}` | v0.3.1，流式响应的增量文本；前端按 requestId 过滤后追加显示；不落盘 |

命名规范：事件名与载荷字段全英文小写下划线；新增事件必须先补进本表。
`internal/*` 不直接 import wails：事件通过 `app.Emitter` 接口注入，
由 `main.go` 提供 wailsEmitter 桥接实现。
（历史说明：v0.1–v0.3 的既有事件载荷实际使用 camelCase，AI 新增事件沿用
camelCase 以与现有代码一致；全量统一命名另开任务，不在本增量扩大差异。）

## 数据模型（v0.1，SQLite schema v1）

- `groups`：id, name, parent_id, sort, created_at, updated_at
- `hosts`：id, group_id, name, addr, port, username, auth_type, key_path,
  init_commands(JSON 数组), keepalive_sec(默认 30), auto_reconnect,
  last_connected_at, sort, 时间戳
- `snippets`：id, name, content, sort, 时间戳
- `settings`：key-value（仅非敏感配置）
- **AI 配置（v0.3.1）**：`ai.enabled / ai.provider / ai.base_url / ai.model /
  ai.temperature / ai.timeout_sec / ai.allow_write / ai.allow_insecure_tls /
  ai.concurrency / ai.cloud_consent`。`ai.cloud_consent` 是
  `{endpointHash: {endpoint, grantedAt}}` JSON，非本机（LAN/云端）授权按规范化 endpoint 记录。
  不新增表；prompt / response / 行数据不落库。
- **API key**：OS keychain，account=`ai:{provider}:{endpointHash}:api_key`。
  endpointHash = sha256(协议+主机+端口+路径) 前 16 位；换 endpoint 不复用旧 key，
  保存时删除旧 endpoint 的 key。key 永不出现在 SQLite/日志/前端返回值中。
- **密码/passphrase 永不入库**：存 OS keychain（service=`Panoramix`，
  account=`host:{id}:password|passphrase`）。keychain 不可用时的主密码 AES-GCM
  回退在 M4 补齐（加密容器文件，主密码经 Argon2id 派生）
- `known_hosts`：OpenSSH 兼容格式，存于配置目录，供 TOFU 与 x/crypto/ssh/knownhosts 使用

## 并发模型

- 每个 SSH 会话：独立 goroutine 对（读/写 PTY），会话池统一管理生命周期
- 传输队列：worker 池（默认并发 3），任务状态机 pending→running→done/failed/canceled，
  架构按可暂停可恢复设计（v0.1 不暴露暂停 UI）
- 事件推送统一走 Wails runtime；高频事件必须在发射前合并/节流（见协议表）
- AI 请求：全局信号量默认并发 1（可配 1–2），避免多个查询标签同时打爆本地 7B 模型；
  同一 requestId 的新请求会取消旧请求（仅同一标签语义）；prepare 缓存单次使用、5 分钟过期
- SQLite：`db.SetMaxOpenConns(1)` + WAL + busy_timeout，规避 modernc 驱动的锁问题

## 日志与脱敏

- 日志全英文、关键路径必打（连接建立/断开、认证结果、传输起止、迁移数量）
- 密码/私钥/passphrase/token 出现在结构体时的 `String()`/日志格式化必须脱敏；
  `internal/store` 的脱敏测试是硬性验收项
- AI 路径禁止打印 question / schema DDL / prompt / model response / API key；
  只记录 requestId、mode、endpoint scope、字符数、耗时与错误 code
- `DBQuery` 的失败日志先经 `redactSQLError`（移除单引号字面量）再输出，
  防止重复键值等行数据进入日志

## 关键技术决策记录（ADR 摘要）

| 决策 | 选择 | 理由（备选为何落败） |
| --- | --- | --- |
| UI 框架 | Wails v2 | xterm.js 免费拿到工业级 VT 兼容；Fyne 无成熟嵌入式终端；Wails v3 仍是 alpha |
| SQLite 驱动 | modernc.org/sqlite | 纯 Go 无 CGO，Windows 交叉编译与 CI 简单（mattn 需 gcc） |
| 密钥验证 | TOFU | 兼容 OpenSSH known_hosts，用户习惯一致；严格模式体验差 |
| 架构形态 | 单 module、无插件化 | AI 辅助编码下统一 module 最利全局重构；插件化是 v1.0 后的事 |
| 监控采集 | SSH 读 /proc+sys | 无 agent、连上即看；top/df 解析仅作非 Linux 回退 |
| 遥测 | v0.x 无 | 安全工具的信任是生命线 |
| AI 隐私边界 | 默认本地 + 只发 schema + 两阶段预览 + 永不自动执行 | 行数据是最大泄漏面；BIRD/Spider 2.0 表明 text-to-SQL 仍需人审；preview=send 由缓存保证 |
| AI provider 协议 | OpenAI-compatible /chat/completions（流式） | Ollama、LM Studio 与主流云网关均兼容；避免多套 SDK |

## 未来模块挂载点（只留接口位，不提前实现）

- `internal/monitor`（v0.2）：`app` 层新增 Sampler，复用 ssh 会话池开独立 channel
- `internal/db` / `internal/redis`（v0.3）：driver 在 `app` 层注册为新绑定服务
- `internal/ai`（v0.3.1，已落地）：OpenAI-compatible provider；后续终端 AI（v0.5）
  复用同一套 provider / keychain / 预览与脱敏基础设施
- `internal/k8s`（v0.4）：kubeconfig 来源抽象（SSH 隧道发现 / 手动导入），client-go
- AI 运维（v0.5）：终端选中文本解释与命令建议；SQL 助手已在 v0.3.1 落地
