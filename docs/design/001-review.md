# 001 设计评审：AI SQL 助手（v0.3.1）

> 评审对象：`docs/design/001-next-iteration.md`（方向 A：AI SQL 助手）
> 评审人：director
> 日期：2026-11-04
> 评审依据：`director.md` 五维标准、`panoramix-engineering/SKILL.md`、`ARCHITECTURE.md`、`ROADMAP.md`、`AGENTS.md`、当前代码（`internal/db`、`internal/app`、`frontend/src/components/DBView.vue`）

## 评审结论：有条件通过

方向成立，竞品调研扎实，"本地默认 + 只发 schema + 人审执行"的隐私三层思路是对的。但当前方案在**预览与实发的一致性、凭据/授权绑定、DDL 脱敏、写操作判定、可测试性**五处有硬伤，且范围明显超过"一个可独立验证的增量"。修完阻塞项、砍掉外围范围后，可以进入实现。

### 五维速评

| 维度 | 判断 | 关键依据 |
| --- | --- | --- |
| 价值 | 真需求，但"默认本地"目前是空承诺 | 未配置 Ollama 时 AI 完全不可用；差异化在隐私而非生成能力；对多数用户，方向 B 的投入产出比更高 |
| 范围 | 过大 | 3 模式 + 设置弹窗 + 预览 + 取消/超时 + 双右键入口 + 响应式抽屉 + 可拖拽侧栏，已不是单个增量 |
| 风险 | 高，集中在数据外发 | 预览可被后续编辑绕过；key/consent 未绑定 endpoint；HTTP 重定向；`DEFINER`/`AUTO_INCREMENT` 泄露；写操作误判；本地模型并发 |
| 一致性 | 基本符合，需补文档与上下文 | `internal/ai` 单向依赖可行；事件表、目录、README/ROADMAP 需更新；查询标签本身没有 database 上下文 |
| 可验证性 | 验收细，但缺少确定性测试抓手 | 大量验收依赖真实 Ollama；缺 fake provider、payload 快照、取消/超时/重定向单测 |

### 必须修改（阻塞）

1. **"发送前预览"必须等于"实际发送"，不能是两次独立构建**
   - 现状：`AIPreviewContext` 与 `AIGenerate` 各自构建 payload；用户在预览后仍可改问题、增减表；两次调用之间 schema 也可能变化。第一次调用返回的预览，对第二次调用没有任何约束力。
   - 建议改成两阶段协议：
     - `AIPrepare(req) -> {previewId, payload, payloadHash, chars, warnings}`：只读 schema、构建 payload、缓存，不调用模型；
     - 用户确认后 `AIGenerate(previewId, requestId)`：后端只读取缓存中的 payload 原文发送；缓存单次使用、5 分钟过期；过期或 hash 不匹配则要求重新预览。
   - 本机 provider 可以跳过"人工看预览"，但也必须走同一个 `AIPrepare` 构建路径，保证内部一致。
   - 验收：预览后修改 schema 或问题，发送内容必须与预览一致，或明确要求重新预览；测试要覆盖这个竞态。

2. **API key 与云端授权必须绑定到规范化 endpoint，不能只按 providerId**
   - 现状：key ref 为 `ai:{providerId}:api_key`，云端授权是全局 `ai.cloud_consent`。用户把 base URL 从 A 改成 B，旧 key 会被发给 B，旧授权继续有效。这是一条真实的凭据外泄路径。
   - 建议：规范化 `scheme://host:port` 后取短 hash，key ref 改为 `ai:{provider}:{endpointHash}:api_key`；consent 按 endpoint 记录（含时间），host 变化即失效并要求重新授权；切换 provider 或清空配置时删除旧 key。
   - 当前"空字符串=不修改"无法删除 key，需补 `AIDeleteKey`（或 `AISaveConfig` 的 `clearKey` 语义）。
   - 验收：A 配 key → 改 B → 不发送 A 的 key、要求重新授权；清空配置 → keyring 无残留。

3. **HTTP 客户端必须收紧**
   - **禁止自动跟随重定向**（`CheckRedirect` 返回 `ErrUseLastResponse`）。否则本机/LAN endpoint 可以 307 把 schema 转发到公网，预览完全失效。
   - 只允许 `http`/`https`；拒绝 URL 内嵌 userinfo；API key 只放 `Authorization` header，不进 URL、不进错误信息、不进日志。
   - 禁止 `InsecureSkipVerify`；自签名 LAN 证书必须显式开关 + 警告。
   - 读响应体加 `io.LimitReader`（如 2MB），校验 content-type；请求体设上限（问题长度、schema 预算）。
   - 连接/首包/总超时分开（dial 5s、TLS 10s、总超时取配置）；loopback 明确 `Proxy: nil`，不要被 `HTTP_PROXY` 环境变量带走。
   - 验收：httptest 构造 302 到公网、超长响应、慢响应、取消、无效 JSON，断言均被正确处理且不泄露 payload。

4. **schema 上下文必须脱敏，不能原样发 `SHOW CREATE`**
   - `SHOW CREATE VIEW`/视图的 `SHOW CREATE TABLE` 含 `DEFINER=\`user\`@\`host\`` —— 直接违反设计里"不发送用户名/主机名"的承诺。
   - `SHOW CREATE TABLE` 含 `AUTO_INCREMENT=N`，会泄露近似行数。
   - **现有 `db.ShowCreateTable` 对视图会失败**：视图的 `SHOW CREATE` 返回 4 列（View / Create View / character_set_client / collation_connection），当前 `Scan(&name, &ddl)` 只接收 2 列，AI 上下文不能直接复用它。
   - 建议：新增 `GetSchemaDDL`（或 `ShowCreateObject`）区分表/视图；v1 建议把视图排除出 AI 上下文（或只发列名/类型，不发 `CREATE VIEW` 的 SELECT 定义）；统一剥离 `DEFINER=...`、`AUTO_INCREMENT=\d+`、版本注释；外键引用的未选表名要在预览中明示。
   - 验收：含 DEFINER、AUTO_INCREMENT 的 DDL；视图；外键跨表引用。

5. **写操作判定不能只看首关键字**
   - 现状的白名单首关键字 + "简单引号/反引号感知"扫描，漏掉：`WITH ... UPDATE/DELETE`、`SELECT ... INTO OUTFILE/DUMPFILE`、`SELECT ... FOR UPDATE`/`LOCK IN SHARE MODE`、前导注释、`CALL/SET/USE/LOAD DATA/HANDLER/DO/PREPARE`。
   - 建议实现小的词法扫描（去注释、字符串、反引号）：
     - 先判多语句；再判首语句类型；
     - 白名单只放 `SELECT/SHOW/EXPLAIN/DESC/DESCRIBE/WITH`，`WITH` 继续看下一个关键字；
     - 命中 `INTO OUTFILE|DUMPFILE|FOR UPDATE|LOCK IN SHARE MODE` 一律标红且默认不插入；
     - 无法确定时归为"需人工确认"，不要静默按只读处理。
   - 同时建议收紧 `internal/db.isQuery`：`SELECT ... INTO OUTFILE` 目前被当作查询执行，AI 警告不能替代 DB 层判定。
   - 验收：表驱动覆盖上述语句 + 注释/字符串里的分号。

6. **必须提供确定性测试替身，不能把验收押在真实 Ollama 上**
   - 验收标准大量依赖真实模型（"停掉 Ollama"、"测试连接成功"），QA 没有模型时无法执行，CI 也无法回归。
   - 建议：在 `internal/ai` 定义 `Client` 接口（如 `Generate(ctx, Payload) (Result, error)`），`app.NewService` 注入（与 `Emitter`/`Dialoger` 同模式）；测试用 `httptest.Server` 模拟 OpenAI-compatible 的各类响应。
   - 测试至少覆盖：payload 快照（无行数据/密码/主机名）；key 只进 `SecretStore`、SQLite 无 key、日志无 key/问题/响应；取消、超时、429/500、无效 JSON、重定向、多语句、DML。
   - 真实 Ollama 测试放 `//go:build integration` 或环境变量开关，不进 `go test ./internal/...` 默认路径。
   - 这是 QA 可执行的前提，不是"建议"。

7. **本地模型必须有全局并发上限**
   - "同一标签一个请求"不够；多个查询标签可以同时打多个 7B 请求，显存/内存会直接打爆。
   - 建议全局信号量默认 1（可配 1–2）；第二个请求排队或明确拒绝，并显示"前面还有 N 个请求"；"新请求取消旧请求"只限同一标签。
   - 验收：并发两个请求，断言只有一个打到 provider。

8. **补全查询标签的 database 上下文**
   - `openQuery()` 创建的查询标签 `database` 为空，连接本身也可能没有默认库；AI 侧栏没有库选择器，`AIRequest.Database` 无法构造 schema 上下文。截图里直接出现 `users/orders/payments` 在当前代码路径上不成立。
   - 建议：AI 面板增加库选择器（默认连接 `DBName`，为空时必选）；表选择器按库加载；Redis 连接隐藏 AI 按钮并在服务端拒绝。
   - 验收：空库连接 → 打开 AI → 必须先选库；Redis 连接 → 无 AI 入口。

9. **范围收敛到可独立验收的增量**
   - 本轮删除：数据/结构标签右键"用 AI 查询此表"、查询历史右键"让 AI 解释"、窄窗口底部抽屉、侧栏拖拽（固定 360px + 折叠即可）；修复模式的 diff 高亮降级为"修正 SQL + 一句原因"。
   - 拆成：v0.3.1a provider 配置 + 预览 + 生成；v0.3.1b 解释 + 修复；v0.3.1c 入口补强。每个 PR 单独验收。
   - 按当前完整范围，1.5–2 周（业余 2–3 周）的估算是不可信的；砍完后才可信。

10. **产品默认与文档必须同步**
    - "默认本地模型"目前不成立：默认状态是"未配置"，用户没有 Ollama 就用不了。文案改成"AI 默认关闭；启用后默认只允许本机模型"。可选做首次打开时 1s 探测 11434/1234，探测不到就引导安装，而不是假装默认可用。
    - README 的"零遥测、零数据上报"必须补一句：仅用户显式配置云端 provider 并确认后，发送所选 schema 元数据；无自动上报。否则一旦有人用云端，定位就会尴尬。
    - `ARCHITECTURE.md` 更新：`internal/ai` 目录、依赖方向 `app → ai`、事件表新增 `ai:status`、未来模块挂载点从 v0.5 改为 v0.3.1；`ROADMAP.md` 记录 v0.3.1/v0.3.2 插入及 K3s 顺延。
    - 事件字段命名按文档的"小写下划线"还是现有代码的 camelCase，先统一；当前文档与代码已经不一致，新增事件别再扩大。

### 建议改进（不阻塞）

- `AITestConnection` 应接受草稿配置和 key（`AITestConnection(cfg AIConfig, apiKey string)`），否则测的是已保存的旧配置；顺便校验 model 是否存在（`GET /v1/models` 或最小 completion）。
- 本地 7B 冷启动可能超过 60s；默认 120s 或至少提示"本地模型首次加载可能较慢"，并把"超时"与"模型加载中"分开。
- 12k 字符预算按 rune 还是 byte 要说清；中文注释会膨胀，建议按近似 token 或做成可配置，并在超限前显示每表字符数。
- 预览策略：云端/LAN 每次发送都预览（至少显示摘要 + 可展开），本机可首次 + payload hash 变化时预览；不要只"首次向 provider 预览"。
- `AIConfig` 增加 `HasKey`（类似 `HasSecret`），前端显示"已保存 key"而不回显。
- `RequestID` 不要依赖 `crypto.randomUUID`：Wails 的 `wails://` 在部分 WebView 不是 secure context，可能 undefined；沿用后端 `store.NewID` 或前端 fallback。
- 解释/修复应使用"上次执行的 SQL + 上次错误"，不要用当前编辑器内容（用户可能在报错后改了 SQL，导致修复对象错位）。
- 代码块解析兼容 ```sql / ```mysql / 无语言；保留代码块外的说明作为 Explanation；多代码块取第一个 SQL 块。
- 错误映射：401/403/404/429/5xx/timeout/connection refused 分别给中英文可读文案；不要把 provider 原始响应体整段回显（可能含 key 片段或 HTML）。
- 日志：AI 路径不要打 question/DDL/response；`DBQuery` 当前 `log.Printf("[app] db query failed: %v", qerr)` 会把重复键值等行数据写进日志，修复模式上线前应脱敏（这也违反 AGENTS.md 的"等敏感信息"）。
- 插入编辑器用 `textarea.setRangeText` + `dispatchEvent(new Event('input'))`（或 `execCommand('insertText')`）以保留 undo 栈；不要直接改 `v-model` 后丢失光标。
- 侧栏固定 360px 在 1280 窗口 + 264px 左侧栏 + 960 最小宽度下只剩约 330px 编辑器；建议 320px 或可折叠，不要做底抽屉。
- provider 预设写清端口/路径：Ollama `http://127.0.0.1:11434/v1`、LM Studio `http://127.0.0.1:1234/v1`；BaseURL 规范化（去尾斜杠、缺 `/v1` 时提示）。
- `Temperature` 默认 0.1；给 `MaxTokens` 上限（如 2048），避免费用/延迟失控。
- 响应超过 20k 字符建议直接拒绝并要求重试，而不是截断后允许插入（截断可能产生无效 SQL）。
- 解释/修复的提示词语言应跟随 UI locale；当前写死"返回中文自然语言"，en-US 用户会拿到中文解释。
- 设计系统：使用 `--warning`/`--danger`/`--accent` 变量，provider 徽标不要硬编码红黄绿；间距用 4/8/12/16。
- `internal/ai` 的 payload builder 只接收 `ai.TableSchema{Name, DDL}` 这类中性 DTO，禁止 import `internal/db`（否则破坏横向依赖约束）。

### 风险提示

- **价值风险**："默认本地"是空承诺；大多数目标用户没有 Ollama。首屏 AI 按钮可能变成"配置地狱"入口。若不做探测/引导，建议先按"可选高级功能"定位，别放进宣传主卖点。
- **准确性风险**：schema-only 对值相关查询天然弱；BIRD 人类 92.96% vs GPT-4 54.89%，Spider 2.0 仅 21.3%。本地方案的可执行率目标 ≥50% 很可能达不到；UI 必须持续提示"需复核"，不要用"准确"做文案。
- **隐私残余风险**："只发 schema"不等于不敏感；表名/列名/注释/外键引用都是商业信息。预览是唯一可信边界，必须每次云端发送都可见；provider 侧的日志/留存我们无法控制，README 要写明。
- **分类器不是安全边界**：loopback/LAN/公网判定基于 URL 字面，DNS 可解析到别处；把它当"提示"而不是"强制隔离"。真正的强制隔离只有"默认关闭 + 每次确认 + 不自动重定向"。
- **路由/排期风险**：插入 v0.3.1 + v0.3.2 会让 K3s 顺延 3–4 周；对一个"一体化运维客户端"来说，K3s 是更核心的承诺。建议 v0.3.1 只做生成 MVP（1 周内），或先做方向 B，把 AI 排在 K3s 之后；至少不要按当前范围一次做完。
- **现有代码风险**：数据编辑/DDL 编辑已经在前端拼 SQL 执行（`applyEdits`、`runDDL`），与 ROADMAP "v0.4 数据编辑"不一致；AI 增量不要顺手改这些，否则回归面失控。方向 B 的"RawWhere 收紧"也应单独立项。
- **Wails 绑定风险**：新增 `internal/app` 绑定方法后必须重新生成并跑 `scripts/dedupe_bindings.py`；否则 Rollup 重复声明。
- **Keychain 风险**：WSL/Linux 无 Secret Service 时 `go-keyring` 会失败；AI key 保存会直接报错，且 M4 的主密码回退还没实现。设置页要给出明确提示，不要静默降级到 SQLite。
- **本地模型资源风险**：7B 模型 + 12k schema + 多个标签并发，容易 OOM；全局并发 1 是底线。
- **日志风险**：修复模式把 DB 错误当输入，而 `DBQuery` 已经在日志里打印原始错误（可能含重复键值）；上线前必须处理，否则"不发送行数据"的承诺会被本地日志打脸。

### 一句话结论

方向值得做，但当前方案在"预览=实发"、凭据/授权绑定、DDL 脱敏、写操作判定和可测试性上有硬伤；按阻塞项砍成"生成优先"的小增量、补齐确定性测试后再进入实现，前提是用户先拍板路线与"AI 默认关闭、云端每次确认"的隐私默认。
