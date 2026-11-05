# 002 设计评审：K8s / K3s 面板（v0.4）

> 评审对象：`docs/design/002-k8s-panel.md`
> 评审人：director
> 日期：2026-11-05
> 评审依据：`director.md` 五维标准、`panoramix-engineering/SKILL.md`、`ARCHITECTURE.md`、`ROADMAP.md`、当前代码（`internal/app`、`internal/ssh`、`internal/store`、`frontend/src/App.vue`、`DBView.vue`、`ConnectionTree.vue`、`TerminalView.vue`），以及 client-go **v0.32.3 / v0.37.2** 源码实测。

## 评审结论：打回

方向成立，竞品调研和信息架构是迄今最扎实的一份，"SSH 信任链 + 自动发现"的差异化也是真的。但当前方案有一条会直接让核心功能（Pod 终端）失败的库级事实错误，两处安全边界缺口（kubeconfig 引用路径任意读、环境代理未真正关闭），以及一个不可能按期交付的范围。**骨架保留，修完阻塞项、做一次隧道 spike、砍成可独立验收的增量后再复审，不需要重写。**

### 五维速评

| 维度 | 判断 | 关键依据 |
| --- | --- | --- |
| 价值 | 真需求，差异化成立 | 目标用户（homelab / 单机 K3s）的痛点是"接入四道坎"，不是"少一个 k9s"；SSH 发现 + 免改写 kubeconfig 是竞品没做的路径 |
| 范围 | 不通过 | 117 项验收 + 7 个阶段 + 25 个绑定 + 新模块 + SSH 重构 + 约 3–4k 行前端；业余节奏下 6–10 周，不是 3–4 周 |
| 风险 | 高，集中在传输层 | exec 不走自定义 Dialer（功能必挂）；Dialer/transport 缓存会泄漏；`Proxy=nil` 不关闭环境代理；SSH 重连与 informer/watch 生命周期耦合 |
| 一致性 | 有条件通过 | 三视图/固定标签/空状态/危险操作三步都对；但 `App.vue` 的 `v-if` 会卸载视图，A2/D9 的"状态不丢"不成立；认证弹窗还挂在 `ConnectionTree` 里 |
| 可验证性 | 不足 | 验收很细，但绝大多数依赖真实 k3s；没有 fake SSH + fake apiserver 的确定性测试抓手，117 项里大量无法在 CI 回归 |

### 评审重点逐条结论

| # | 重点 | 结论 | 一句话 |
| --- | --- | --- | --- |
| 1 | 信息架构：第三个一级视图 | **通过** | 全屏资源表 + 详情 + 日志需要宽度，DBView 已有先例；入口留在连接树（右键/徽标）是正确拆分。同时看终端和 K8s 的诉求真实，但应显式延期到分屏/多窗口，不在 v0.4 硬塞 |
| 2 | 技术方案风险 | **不通过** | exec 的 SPDY/WebSocket 不走 `rest.Config.Dial`；`Dialer` 接口并不存在；transport 缓存会因重建 client 而增长；`Proxy=nil` 不生效 |
| 3 | 安全 | **有条件通过** | "只读是产品约束、RBAC 才是边界"的表述正确；但文件路径凭据可任意读、`sudo -n cat` 需要更严格的门槛、`insecure-skip-tls-verify` 需要导入时显式确认 |
| 4 | 范围 | **不通过** | 117 项不能作为一个增量交付；建议拆 v0.4.0 / v0.4.1，或明确接受 6–10 周排期 |
| 5 | 一致性 | **有条件通过** | 固定标签/空状态/三步确认都符合；但视图切换的保活、认证弹窗上提、文档同步三件事没落到实现细节 |

---

## 必须修改（阻塞）

### M1. exec/attach 不会经过自定义 Dialer —— Pod 终端在主场景下必然失败（最高优先级）

**事实**（client-go v0.32.3 与 v0.37.2 一致）：

- `transport/spdy/spdy.go` 的 `RoundTripperFor(config)` 只使用 `restclient.TLSConfigFor(config)` 和 `config.Proxy`，`UpgradeTransport` 传 `nil`；它**从不读取 `config.Dial`**。
- `transport/websocket/roundtripper.go` 的 `RoundTripperFor(config)` 同样只取 TLS + Proxy。
- 因此 `remotecommand.NewSPDYExecutor(config, ...)` / `NewWebSocketExecutor(config, ...)` 会从**本机**直接 dial kubeconfig 里的 `127.0.0.1:6443`，而不是经 SSH channel。

**后果**：k3s 的主场景（apiserver 只监听远端 loopback）下，浏览器能看 Pods、日志能流，但"打开终端"必然 connection refused。验收 G1 直接失败，而这是 ROADMAP 明确要求的功能。

**建议**：不要用 `NewSPDYExecutor(config, ...)`。显式构造可隧道的 exec transport：

```go
// 1) 普通 REST transport：走 cfg.Dial → SSH
rt, err := rest.TransportFor(cfg)

// 2) SPDY upgrader：复用 rt 的 DialContext / TLSConfig
upgrader, err := httpstreamspdy.NewRoundTripperWithConfig(httpstreamspdy.RoundTripperConfig{
    UpgradeTransport: rt, // 内部 apiproxy.DialURL 会取 rt.DialContext
})

// 3) 套上 Bearer token / User-Agent 等 HTTP wrapper（与 spdy.RoundTripperFor 的做法一致）
wrapper, err := rest.HTTPWrappersForConfig(cfg, upgrader)

// 4) 第一个参数必须是 wrapper（包住 upgrader），不是裸 rt
exec, err := remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, "POST", reqURL)
```

注意：`NewSPDYExecutorForTransports` 的第一个参数是发升级请求用的 RoundTripper，必须是包住 `upgrader` 的 `wrapper`；直接传 `rt` 会变成普通 HTTPS 请求，SPDY 升级不会发生。

`apiproxy.DialURL` 会通过 `utilnet.DialerFor(rt)` 取到 `rt.DialContext`，再在该连接上做 TLS；ServerName 由 URL 主机推断，与 M2 的证书方案一致。WebSocket 路径要么用同样的自定义 transport，要么 v0.4 明确只做 SPDY。**验收必须包含一个"exec 的 dial 经过注入 tunnel"的确定性测试**，不能只靠真实集群手测。

### M2. client-go 的 Dialer/transport 方案描述错误，且有缓存泄漏风险

**事实**：

- `rest.Config` **没有** `Dialer` 接口字段；PR #112144 是 **closed，未合并**。实际落地的机制是 `transport.DialHolder`（一个可比较的 `struct{ Dial func(...) }`），且它只在 `transport.Config` 上，不在 `rest.Config` 上。
- `rest.Config.TransportConfig()` 每次调用都会 `&transport.DialHolder{Dial: c.Dial}` 新建一个 holder；全局 transport cache 以 holder 指针为 key。反复 `NewForConfig` / 重连重建 client 会让缓存条目持续增长，并 pin 住已经断掉的旧 SSH client（kubernetes#118703、#125818）。
- 设计里"设置 Dialer / Proxy=nil / UserAgent"这条实现路径不成立，engineer 会找不到 API。

**建议**：

1. **每个集群/隧道只构建一次**：`tc, _ := cfg.TransportConfig()`，把 `tc.DialHolder` 换成**共享的** `*transport.DialHolder`（闭包里读取当前 `*ssh.Client` 的原子指针），`rt, _ := transport.New(tc)`，再用 `kubernetes.NewForConfigAndClient(cfg, &http.Client{Transport: rt})` 与 `rest.RESTClientForConfigAndClient`。不要每建一个 client 就 `NewForConfig`。
2. **重连策略写清**：SSH 重连后，dialer 必须拿到新 client；隧道断开时 `CloseIdleConnections()`，并决定是重建 transport 还是复用 holder。集群切换时销毁旧 client。
3. 加测试：连续重连 N 次后 transport cache 不无限增长（或明确其上限），且旧 SSH client 不被 pin。

### M3. `Proxy = nil` 并不会关闭环境代理

**事实**：`apimachinery/pkg/util/net/http.go` 的 `SetOldTransportDefaults` 在 `t.Proxy == nil` 时会设置 `NewProxierWithNoProxyCIDR(http.ProxyFromEnvironment)`；`spdy.RoundTripperFor` 也在 `config.Proxy == nil` 时回退到 `ProxyFromEnvironment`。

**后果**：在配置了 `HTTPS_PROXY` 的机器上，kubeconfig 的凭据可能被发往环境代理；这与设计里"明确不读取系统代理、避免凭据经代理转发"的安全承诺直接矛盾，且直连（非隧道）场景更容易触发。

**建议**：

- 显式 `cfg.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }`，exec transport 也要同样设置；不要只写 `nil`。
- 验收加一项：设置 `HTTPS_PROXY=http://127.0.0.1:1` 后连接与 exec 均不经过代理（用本地假 apiserver 断言命中）。

### M4. kubeconfig 引用文件路径的任意读边界

**问题**：设计允许把 `certificate-authority` / `client-certificate` / `client-key` 的文件路径经 SFTP/本地读取并内联。但没有限制路径。一个精心构造的 kubeconfig 可以指向：

- 远端 `../../../../etc/shadow` 或用户目录下的私钥；
- 本地 `C:\Users\...\.ssh\id_rsa` 或其他敏感文件。

Panoramix 会把这些内容读进内存并加密落库。对用户是"导入一个 kubeconfig"的普通动作，实际是一次被诱导的任意文件读取。

**建议**：

- 相对路径一律以 kubeconfig 所在目录为根解析；**拒绝绝对路径**与解析后逃逸出该目录的 `..`；符号链接按解析后的真实路径再做一次边界检查。
- 单文件大小上限（如 256KB）、总上限、超时；失败只报文件名，不回报内容，不写日志。
- 本地导入同样适用；"同目录解析"不能成为读取任意路径的借口。
- 测试：`../../etc/passwd`、绝对路径、指向目录、超大文件、符号链接逃逸。

### M5. `sudo` 提权与"只读"表述需要收紧

设计已经正确指出"UI 只读不是安全边界，RBAC 才是"，但还有三处需要落到实现：

1. **`sudo -n cat`**：必须**按路径白名单**（`/etc/rancher/k3s/k3s.yaml`、`/etc/rancher/rke2/rke2.yaml`、`/etc/kubernetes/admin.conf`），路径不可由前端传入、不可拼 shell；超时 + 输出上限；结果不写日志；点击前用独立弹窗说明"将使用你的 sudo 权限读取该文件，内容只留在本机加密存储"。**建议 v0.4 直接不做这个按钮**，只给"用 root 连接 / 复制文件"的手动指引——它扩大提权面，且不是差异化核心。若用户坚持，必须作为独立安全评审项。
2. **`insecure-skip-tls-verify`**：不能只显示黄色徽标。导入时必须显式勾选确认（默认不勾），发现流程默认不自动添加；连接后徽标常驻，并在详情里说明"服务端证书未校验，可能被中间人替换"。
3. **终端 shell 白名单**：`/bin/sh`、`bash`、`ash` 之外一律拒绝，后端校验，不接受前端任意字符串。
4. **权限徽标（建议）**：连接后用 `SelfSubjectAccessReview` 检查 `pods/delete`、`deployments/patch`、`pods/exec` 等几个 verb，显示"此凭据可写 / 只读 / 部分受限"。这比反复写"只读"更诚实，也能解释为什么某些按钮不可用。

### M6. 删除语义与现有代码不一致，且 schema 缺"已失效"状态

**事实**：

- 现有 `DeleteHost`（`internal/app/service.go:490`）是**先删数据库行，再忽略 keychain 删除错误**。
- 设计要求 K8s 集群"先删 keychain 密钥，密钥删除失败则中止，不删行"，并要求失败时"把行标记为已失效"——但 `k8s_clusters` DDL 没有对应字段。
- 删除有 N 个集群的主机时，keychain 操作不在 SQLite 事务内，无法真正原子。

**建议**：

- 统一顺序：停止流与隧道 → 在**一个 SQLite 事务**里删除集群行与主机行（主机删除时同时删 N 个集群行）→ best-effort 删除 keychain 密钥 → 把失败项作为"凭据密钥未清理，请手动处理"回报，不阻断删除。
- 如果坚持"密钥先删"，必须在 schema 增加 `invalid INTEGER NOT NULL DEFAULT 0`（或 `key_state TEXT`），并写清楚重试与 UI 呈现；否则"标记为已失效"无处落。
- 删除确认文案必须列出集群数量、集群名与"删除后需重新发现/导入"，并与现有 `dialog.warning` 一致。
- 验收：keychain 不可用/锁定、删除中途失败、删除正在使用的集群、删除有集群的主机。

### M7. 117 项验收不能作为一个增量交付

**计数**：A11 + B16 + C7 + D10 + E14 + F16 + G12 + H11 + I8 + J7 + K5 = **117**。这不是"验收细"，而是 3–4 个功能块。

**判断**：按当前代码密度（`DBView.vue` 约 2200 行、`service.go` 1214 行、`ai.go` 880 行），这套东西包含：新 `internal/k8s` 模块、client-go transport/exec/watch/log、SSH 无 PTY 连接与重连、加密存储与 keychain、发现/导入两套流程、约 3–4k 行前端、117 项 QA。业余 + AI 辅助下 **6–10 周**是合理区间；3–4 周不可信。

**建议：拆成两个可独立发布、独立验收的增量**（约 75 + 42 项）：

| 版本 | 范围 | 砍掉/延期 |
| --- | --- | --- |
| **v0.4.0** | 后端底座（kubeconfig 解析/最小化/AES-GCM/后台 SSH/自定义 transport/**修正后的 exec**）；K3s+RKE2 发现；**单 context、data-only 导入**；NS/Workload/Pods/Events 浏览 + watch/轮询 + 降级；Pod 日志（跟随/行数/容器/时间戳/上一次性/搜索/暂停）；**Pod 终端（SPDY over tunnel）**；核心安全/错误/i18n/回归 | 多 context 导入、文件路径凭据内联、快捷键系统/帮助面板、日志导出、Workload 详情 Pods、Namespace YAML、`sudo` 按钮、12 标签打磨 |
| **v0.4.1** | 多 context 发现/导入 + 路径凭据内联（含 M4 边界）；快捷键/帮助；Workload 详情；日志导出；全局总览（若用户要） | — |

如果用户坚持 117 项全在 v0.4，请把排期改成 6–10 周并明确接受；不要用"砍一点"掩盖范围问题。

### M8. 缺少确定性测试抓手

**问题**：验收里 D1–D10、E1–E14、F1–F16、G1–G12 基本都要求真实 k3s 集群、真实 403、真实 CrashLoopBackOff、真实 600+ Pods。CI 无法回归，QA 也无法在没有环境时执行。

**建议**（这是进入实现的前提，不是"建议"）：

- `internal/k8s` 定义可注入的 dialer/transport 与 apiserver 客户端接口；测试用 **fake SSH server**（`x/crypto/ssh`）+ **httptest TLS apiserver** 覆盖：隧道建立、TLS ServerName、`/version`、list/watch、日志流、exec 升级、resize、401/403/410、隧道断开重连。
- 单元测试：kubeconfig 解析与最小化（只保留所选 context）、文件路径内联的边界、AES-GCM 往返 + AAD 篡改 + 密钥缺失、状态归一化、`仅看异常`、日志缓冲/ANSI/非法 UTF-8、事件环形缓冲、命名空间 403 降级。
- 安全测试：SQLite 文件与日志中不出现 `BEGIN PRIVATE KEY`/`BEGIN CERTIFICATE`/token；前端绑定返回值不含 kubeconfig；`internal/k8s` 不 import `wails`/`internal/ssh`（依赖守卫）。
- 真实集群项放 `//go:build integration` 或环境变量开关；`go test ./internal/...` 默认路径不依赖网络。
- 加 `go test -race` 与 goroutine 泄漏检查（K5）。

### M9. 视图切换保活与全局认证弹窗没有落到实现

**事实**：

- 当前 `App.vue` 用 `v-else-if="isDBView"` 渲染，切走即**卸载**视图；`TerminalView` 在 `onBeforeUnmount` 里 `term.dispose()`，且没有 `KeepAlive`。所以"切到数据库再回来，终端输出还在"目前并不成立；DB 的 `tabs` 也是组件内 `ref`，切走即丢。
- 认证/密码弹窗 `PasswordPromptModal` 挂在 `ConnectionTree` 里；K8s 视图不渲染连接树，因此 K8s 视图根本没有认证入口。设计里"上提到 App 级统一状态"是必须做的前置重构，不是顺手。

**建议**：

- K8s 视图状态（集群/资源/标签/日志缓冲/终端实例）放进 Pinia store，或对 `DBView`/`K8sView` 使用 `<KeepAlive>`；事件订阅放到 App 级或 store 级，避免切换视图时丢 `k8s:pods` / `k8s:log`。xterm 用 `v-show` 保活比重新创建更稳。
- 60 秒暂停计时器放在 Go 后端（它拥有隧道）或模块级 store，不能放在会被卸载的组件里。
- 把 `PasswordPromptModal` 与 TOFU 弹窗抽到 `App.vue`（或全局 provider），`ConnectionTree` 改为共享状态调用；这是 v0.4.0 的前置任务，需单独回归连接树。
- 把 A2 改成可验证的表述：明确"哪些状态保留、哪些会重建"；如果暂时不修 DB/终端的卸载问题，不要把"终端输出不丢"写进 K8s 验收。

### M10. 事件与状态语义有几处会"看起来是坏的"

1. **Pod 事件子标签只用本地 watch 缓存**：集群里事件量大时，2000 条环形缓冲会把旧 Pod 的事件挤掉；`kubectl describe` 仍能看到的事件，Panoramix 可能显示"无事件"。建议打开事件子标签时做一次**按 `involvedObject.uid` 的 fieldSelector 单次查询**（上限 200 条），与本地缓存合并；不要只依赖全量 watch。
2. **`仅看异常` 定义过宽**：`Succeeded`/`Completed` 的 Job Pod 不是 Running，会被误判为异常；`Restarts > 0` 会把一个月前正常重启过的健康 Pod 永久标红；`Terminating` 在滚动更新时很常见，不应一律算异常。建议：异常 = phase `Failed`/`Unknown`，或 `waiting.reason` 命中错误集合，或 Pending 超过阈值；Restarts 用"最近 N 分钟增长"或只展示不筛选。
3. **命名空间 403 降级后的计数**：全命名空间 watch 403、降级为单 NS 后，Namespaces 表的 Pods 计数对其它 NS 不再可信，必须显示 `—`/`受限`，不能显示 0。

---

## 建议改进（不阻塞）

- **发现探测复用现有终端 SSH client**：自动探测每次连接后另开一条后台 SSH，会多一次认证/TOFU/连接上限消耗。探测是短命 channel，优先复用该主机已有的活动 `*ssh.Client`；只有长期隧道用独立后台连接。探测结果 60 秒缓存保留。
- **哈希变更检测先 `Stat`**：已添加集群每次连接都重读 kubeconfig 算 SHA-256，隐私与 I/O 都不必要。先 SFTP `Stat` 比 size/mtime，变化后再读内容与哈希。
- **请求超时**：不要给 `rest.Config.Timeout` 设全局值（会杀掉 watch）；用 per-request context。单独设置 dial/TLS 握手/空闲连接超时；watch 用无超时 context。
- **`k8s_clusters.kubeconfig_sha256` 对导入来源的含义**：导入没有 `source_path`，哈希应存"规范化后 kubeconfig"的完整性哈希，或明确留空；不要写成"源文件哈希"。
- **日志视图性能**：10,000 行纯文本 + 搜索高亮，别用 `v-for` 逐行渲染；用虚拟列表或窗口化文本视图，F9 才有意义。
- **YAML 上限**：2MB 字符串经 Wails 绑定传输偏重，12 个标签 × 2MB 也不小；建议 512KB 或流式/按需分段。
- **Workloads 轮询只在表格可见时进行**，不要在 Pods 标签页也每 5s 拉 5 种 workload。
- **错误码补全**：429、5xx、timeout、context canceled；把 `specifying a root certificates file with the insecure flag is not allowed` 映射到 `k8s_invalid_kubeconfig` 并给出字段级说明。
- **klog 输出**：client-go 默认 `WarningHandler`/klog 会写 stderr；设置 redacting writer 或静默，避免 apiserver 的 Warning header/URL 进入日志。
- **`K8sGetYAML` 的 `kind` 白名单**要在后端校验（pod/workload/namespace），不要信任前端；`shell` 同理。
- **`K8sAddDiscovered` 部分失败**：多选添加时返回每个 context 的成功/失败，不要只返回成功列表。
- **`?view=k8s` 预览**：沿用 `preview-main.ts` 的 mock 模式，给 UI 验收提供无集群路径（尤其在 exec 修好前）。
- **组件拆分**：`K8sView.vue` 不要复制 `DBView.vue` 的 2200 行单文件；按左侧树、资源表、详情标签、日志、终端、发现/导入弹窗拆组件 + Pinia store。
- **Wails 绑定**：新增约 25 个方法后重新生成绑定并跑 `python3 scripts/dedupe_bindings.py`，否则 Rollup 重复声明（SKILL.md 已记录两次）。
- **二进制体积**：实测（client-go v0.32.3，`-s -w`，最小程序）`clientcmd + kubernetes.Clientset` 从 1.5MB 增到 27.1MB，加 informer/remotecommand 到 27.5MB，Windows 28.0MB——**增量约 25MB 量级，不是设计的 +8–15MB**。真实应用会因已有 `crypto/tls`/`net/http` 共享而略低，但仍应更新发布说明与体积预期；同时 client-go 会带入约 30 个传递依赖，"只依赖 client-go/apimachinery/yaml + 标准库"不准确，需锁版本 + `go mod tidy`。
- **`Service.Shutdown` 目前是空的，`ssh.Manager.Shutdown` 从未被调用**；设计 D10/K5 要求退出干净，需把 K8s 流、后台隧道与 SSH manager 的关闭接进 `Shutdown`（这是既有缺口，顺手补上，但要单独回归）。
- **信息架构的取舍写进文档**：第三个一级视图通过；"终端 + K8s 同屏"明确延期到分屏/多窗口（v0.5），不要在 v0.4 用隐藏标签模拟。

---

## 风险提示

- **最大的风险不是 UI，是隧道生命周期**：SSH 重连 × client-go transport 缓存 × informer/watch × HTTP/2 连接复用，这四者叠在一起才决定"断线后能不能自动恢复"。先用 **1–2 天 spike** 验证：真实 k3s + SSH，跑 `/version`、list、watch、log follow、exec TTY、resize；kill SSH，断言重连后全部恢复；再跑 1 跳/2 跳 ProxyJump。spike 不过，后面的排期都不成立。
- **`ssh.Client.DialContext` 的取消语义**：它内部起 goroutine 调无 ctx 的 `Dial`；远端 TCP 连接挂住时，ctx 取消后 goroutine 仍会等到 channel 关闭。必须给 SSH 连接设整体生命周期，隧道断开时关掉 client，避免 goroutine/channel 泄漏。
- **SSH 重构风险**：`dialChain`、`hostKeyCallbackFor`、重连、keepalive 目前都耦合在 `Session` 上。抽"无 PTY 连接工厂"会触碰最关键的模块；要求：现有测试全绿 + ProxyJump/TOFU/重连回归 + 不允许 K8s 复用 PTY `Session`。这是 v0.4.0 的独立阶段，不要和 UI 混在一个提交。
- **Keychain 在 Linux 无 Secret Service 的环境很常见**：设计选择"仅本次会话、不落明文"是诚实的，但用户会认为"我加了集群，重启就没了"。UI 必须在添加前就提示，而不是重启后才发现；同时要区分"keychain 不可用"与"密钥丢失/损坏"。
- **k3s 证书 SAN 例外**：默认包含 `127.0.0.1`/`localhost` 已核实；但用户用 `--tls-san` 并移除 loopback、或 kubeconfig 用 `tls-server-name` 指向不在 SAN 的名字时会失败。设计已有清晰错误，但不要"自动跳过校验"来兜底。
- **`insecure-skip-tls-verify` + CA 同时存在**会被 client-go 直接拒绝；解析阶段就要给出字段级错误，不要等连接时报一个晦涩的 x509。
- **2MB YAML / 64KB 日志批次 / 2000 事件 / 10k 行日志**：每个上限都要有内存与 UI 卡顿测试，不能只写在文档里。
- **117 项验收的 QA 成本**：即使功能全部实现，逐项验收也要 20–30 人日；这不是"测试人员顺手跑一下"的量级。
- **文档契约**：`ROADMAP.md` 的 v0.4 写着"经 SSH 隧道转换为可用 kubeconfig"和"全局所有主机总览"，设计改成"不重写 kubeconfig + 总览延期"。这是对既定契约的修改，必须用户拍板并同步 `ROADMAP.md` / `ARCHITECTURE.md`（新增 `internal/k8s`、事件表、`k8s_clusters`、keychain 引用、ADR）。

---

## 复审门槛

修完以下内容即可复审，不需要重写：

- [ ] M1：exec 用 `rest.TransportFor` + `spdy.NewRoundTripperWithConfig(UpgradeTransport)` + `NewSPDYExecutorForTransports`，并有 fake tunnel 的确定性测试
- [ ] M2：写清共享 `DialHolder` / 单次 transport 构建 / 重连重建策略，有缓存增长测试或明确上限
- [ ] M2：`Proxy` 显式返回 nil，exec transport 同样处理，有 `HTTPS_PROXY` 测试
- [ ] M3：kubeconfig 引用路径的目录边界、大小/超时、错误脱敏
- [ ] M4：`sudo` 按钮删除或白名单化；insecure TLS 导入确认；shell 白名单
- [ ] M5：删除顺序与 schema（`invalid`/`key_state`）定稿；`DeleteHost` 同步
- [ ] M6：v0.4.0 / v0.4.1 拆分表与验收项重编号
- [ ] M7：fake SSH + fake apiserver 测试计划与 `//go:build integration` 划分
- [ ] M8：Pinia/KeepAlive 保活方案、60s 暂停位置、认证弹窗上提
- [ ] M9：事件 fieldSelector 兜底、`仅看异常` 定义、命名空间受限计数
- [ ] 隧道 spike 结果（真实 k3s + ProxyJump + 断线重连 + exec TTY/resize）
- [ ] `ARCHITECTURE.md` / `ROADMAP.md` / 事件表 / schema 同步

## 一句话结论

方向对、差异化真实，但"exec 不走自定义 Dialer"会让 Pod 终端直接失败，加上代理未真正关闭、路径凭据任意读和 117 项范围，当前文档不能进入实现；先修 10 个阻塞项、做 1–2 天隧道 spike、把 v0.4 拆成 v0.4.0/v0.4.1，再开工。
