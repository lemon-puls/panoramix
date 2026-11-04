# 001 终审：AI SQL 助手（v0.3.1）

> 评审对象：分支 `feat/v0.3.1-ai-sql-assistant`（相对 main 的 AI 增量，`46e0c2f...HEAD`；共 11 个提交，其中实现/修复 7 个、文档 3 个、团队配置 1 个）
> 评审人：director
> 日期：2026-11-05
> 评审依据：`001-review.md` 的「必须修改」清单、`director.md` 五维标准、`ARCHITECTURE.md`、实际 diff 与测试执行结果

## 评审结论：有条件通过

上一轮 10 项「必须修改」中 9 项完整落实、1 项（全局并发闸门）实现存在一个低概率但真实的唤醒丢失（liveness）缺陷。隐私边界、凭据绑定、DDL 脱敏、写操作判定、预览=实发这五个核心风险点均已关闭，并有确定性测试。修掉 `aiGate` 的唤醒丢失并补回归测试后，可合并发布 v0.3.1。

---

## 一、上一轮「必须修改」逐条复核

| # | 要求 | 结论 | 证据 |
| --- | --- | --- | --- |
| 1 | 预览 = 实发（两阶段协议） | 已落实 | `AIPrepare` 构建并缓存 system/user 原文（`internal/app/ai.go:288-432`）；`AIGenerate` 只读缓存并原样发送（`:478-607`）；缓存单次使用、5 分钟过期。`TestAIPreviewEqualsActualSend` 在 Prepare 后篡改 schema，断言实际发送仍是预览版本 |
| 2 | key/consent 绑定规范化 endpoint | 已落实 | `KeyRef(provider, endpointHash)`（`internal/ai/endpoint.go:153`）；consent 按 endpointHash 存 JSON（`ai.go:651-680`）；切 endpoint 删除旧 key（`:194-233`）；新增 `AIDeleteKey`。`TestAIKeyBoundToEndpointAndDeletedOnSwitch`、`TestAIConsentBoundToEndpoint` 覆盖 |
| 3 | HTTP 客户端收紧 | 已落实 | 禁止跟随重定向（`CheckRedirect` 返回 `ErrUseLastResponse`）；仅 http/https、拒绝 userinfo；`Proxy: nil`；dial 5s/TLS 10s/总超时；响应体 2MB 上限；key 只进 `Authorization`。`TestGenerateRefusesRedirects`、超长响应、超时、取消、无效 JSON 均有测试 |
| 4 | schema 上下文脱敏 | 已落实 | `SanitizeDDL` 剥离 `DEFINER`、`AUTO_INCREMENT=N`、版本注释、`SQL SECURITY`、控制字符；视图整体排除出上下文；`ShowCreateObject` 正确解析 4 列视图结果（`internal/db/mysql.go`）。`TestSanitizeDDL...`、`TestAIViewsExcludedFromContext` 覆盖 |
| 5 | 写操作判定不能只看首关键字 | 已落实 | 引号/反引号/注释感知扫描；展开 `/*!NNNN ... */` 后再分析；覆盖 `WITH ... UPDATE`、`INTO OUTFILE/DUMPFILE`、`FOR UPDATE`、`LOCK IN SHARE MODE`；`internal/db.isQuery` 同步收紧。表驱动测试覆盖 25+ 用例 |
| 6 | 确定性测试替身 | 已落实 | `ai.Client` 接口 + app 注入（与 Emitter 同模式）；`httptest` 假 provider；payload 快照、重定向、超时、取消、授权绑定、并发均有测试；真实模型不在默认测试路径 |
| 7 | 本地模型全局并发上限 | **部分落实** | 默认并发 1、可配 1–2、排队与取消已实现；但 `aiGate.acquire` 存在唤醒丢失，见下节「阻塞项」 |
| 8 | 查询标签 database 上下文 | 已落实 | AI 面板新增库选择器；无默认库时必须显式选择；后端 `database_required`；Redis 连接 `redis_unsupported`。`TestAIRedisAndMissingDatabaseRejected` 覆盖 |
| 9 | 范围收敛 | 已落实 | 未做数据/结构/历史右键入口、底部抽屉、侧栏拖拽；修复模式降级为「SQL + 一句原因」；无「顺手做」的无关产品改动 |
| 10 | 产品默认与文档同步 | 已落实 | AI 默认关闭、启用后默认本机 Ollama；README 明确隐私边界与残余风险；ARCHITECTURE 更新目录/依赖/事件/存储；ROADMAP 记录 v0.3.1 插入与 K3s 顺延。事件字段沿用 camelCase 并写明这是存量一致性的取舍 |

---

## 二、指定抽查项

### 2.1 `AIGenerate` 是否在发送时重查 consent

**是。** `AIGenerate` 在真正调用 provider 之前按「当前」配置重新判定（`internal/app/ai.go:498-506`）：

```go
needsConsentNow := entry.endpoint.Scope != ai.ScopeLocal && !s.hasCloudConsent(entry.endpoint.Hash)
if needsConsentNow {
    if !consent { return nil, aiError("consent_required", entry.endpoint.Normalized) }
    s.grantCloudConsent(entry.endpoint.Hash, entry.endpoint.Normalized)
}
```

即 Prepare 快照为「已授权」、用户随后清除授权时，旧预览不能再外发。`TestAIGenerateRechecksConsentAtSend` 覆盖该竞态，并断言被拒绝的请求未触达 provider。发送前还会校验 endpointHash 与 model 未变化（`:491-497`），配置变更返回 `config_changed`。

附带说明：拒绝 path 在 `takePrepared` 之后，缓存已被消费，用户需重新预览；这是可接受的取舍，但应在 UI 上表现为「重新生成」，目前前端确实如此。

### 2.2 敏感值提示是否只返回类型、不返回内容

**是。** `detectSensitiveHints` 只产出 `{field, kind}`（`internal/ai/payload.go:276-311`），不包含命中文本，也不自动改写用户输入；前端只把字段名与类型拼成提示条（`AIPanel.vue:151-163`）。`TestBuildPayloadSensitiveHints` 用密码/Bearer/AWS key/私钥/邮箱哨兵值断言提示结构里不出现任何原文。

非阻塞缺口：检测只覆盖 question/sql/error，未覆盖会被发送的 **schema 注释**。注释中的连接串/密码不会得到黄色提示（原文仍在 payload 预览中可见）。建议把同一组正则跑一遍 `SanitizeDDL` 后的 DDL。

### 2.3 `aiGate` 信号量实现

主路径正确：`tryAcquire` 快路径、`acquire` 排队、`release` 唤醒队首、`resize` 缩容不丢在途计数、扩容唤醒等待者；`TestAIGlobalConcurrencyLimit` 证明并发 1 时同时只有一个请求到达 provider。`resize` 采用「active 计数 + 等待队列」而非可替换 channel，方向正确。

**缺陷（阻塞）**：等待者被唤醒后若其 ctx 同时被取消，会丢掉这次唤醒，导致队列中的下一个请求永久挂起。

交错顺序：

1. 并发 1，请求 R 在途，等待者 A、B 入队；
2. R 调用 `release()`：`active=0`，`wakeLocked()` 弹出并 `close(A.ch)`；
3. 与此同时 A 的 ctx 被取消（用户点「停止」）；
4. A 的 `select` 中 `<-ch` 与 `<-ctx.Done()` 同时就绪，二者任选其一都会返回取消：选 `ctx.Done()` 走移除分支；选 `<-ch` 则回到循环顶部，因 `ctx.Err()!=nil` 直接返回；
5. 两种路径都没有把空出的槽位交给 B。此后 `active` 仍为 0、B 仍在队列，但没有新的 `release()/resize()` 事件，B 会一直停在 `queued` 阶段。

现有 `TestAIGateAcquireCanceledLeavesNoWaiter` 在**入队前**就取消 ctx，走不到这条路径，因此没覆盖。

修复建议（小改）：

- 在 `acquire` 内所有因 ctx 取消而返回的路径（`ctx.Done()` 分支，以及被 `<-ch` 唤醒后循环顶部发现 `ctx.Err()!=nil` 的路径）都在持锁时调用 `wakeLocked()`，把空槽位交给下一个等待者；`wakeLocked` 已有 `active<limit && len(waiters)>0` 保护，重复调用安全。
- 补回归测试：两个等待者，一个被唤醒的瞬间取消，断言另一个仍能获得槽位（可把「入队/唤醒」抽成可测函数，或注入唤醒钩子，避免依赖 `select` 随机性）。

---

## 三、代码审查结论（五维）

### 正确性

- 核心链路（Prepare → 缓存 → Generate → 解析 → 判定 → 前端插入）正确，边界处理完整：空问题、超长问题/SQL/错误、12k schema 预算、导出错误逐表字符数、视图排除、Redis/空库拒绝、单次预览、过期、取消、queued 状态。
- 模型响应解析与写操作判定按评审要求做了词法扫描，且 `/*! ... */` 先展开；`internal/db.isQuery` 同步修正了 `SELECT ... INTO OUTFILE` 与 `WITH ... UPDATE`。
- **一个安全开关的 TOCTOU（非阻塞）**：`AIGenerate` 重查了 endpoint/model，但没有重查 `AllowWrite`，发送时使用 `entry.allowWrite` 快照。若用户在预览后关闭「允许写操作」，本次结果仍可能 `InsertAllowed=true`。前端还有写操作二次确认，因此不是自动执行漏洞，但应改为发送时读取当前 `AllowWrite`（或配置变更即要求重新预览）。
- 停止/取消在极端时序下仍可能收到迟到结果：`stop()` 在请求阶段不递增 `runToken`，且 `AICancel` 与后端注册 cancel func 之间存在窄窗口；建议停止后同时作废旧 token 并清空流式文本。
- 标识符未过滤控制字符：表名/库名被直接拼进 `Selected tables:` 与 `### TABLE` 行（DDL 本体已脱敏）。恶意 schema 的表名（MySQL 反引号标识符允许换行）可破坏 prompt 结构。建议对标识符同样执行 `stripControlChars`，并限制可疑换行。
- `ShowCreateObject` 新增了对 `information_schema.TABLES` 的依赖，现有 `ShowCreateTable`（结构标签 DDL）也走这条路径；常规权限下无影响，但属于既有功能的行为变化，QA 回归时值得留意。

### 依赖方向

- `internal/ai` 仅依赖标准库（已逐文件核对 import），未 import 任何 internal 包，未新增 Go 依赖。
- 仅 `internal/app` import `internal/ai`，保持 `app → ai` 单向；`internal/*` 无任何 wails import（仅注释提及）。
- `Schema` 以 `db.TableInfo` 进入 app 层、以中性 `ai.TableSchema{Name, DDL}` 进入 payload builder，未破坏横向依赖约束。

### 凭据与日志

- key 只走 `SecretStore`（keyring），引用键为 `ai:{provider}:{endpointHash}:api_key`；`AIGetConfig` 只返回 `HasKey`，不回显 key。
- `TestAIConfigPersistsWithoutKeyInSQLite` 断言所有非敏感 settings 不含 key；`TestAIKeyBoundToEndpointAndDeletedOnSwitch` 断言切换 endpoint 后旧 key 被删除、新请求 `APIKey==""`。
- 日志审计：AI 路径只打 requestId/mode/scope/字符数/耗时/错误 code，无 question/DDL/prompt/response/key；`DBQuery` 失败日志已改为 `redactSQLError`（移除单引号字面量并限长）。
- 残余：provider 错误 message 会脱敏 bearer/sk/api_key 并限 200 字符后返回前端，但仍可能包含 schema 片段；可接受（仅回显给本机用户，不入日志）。

### 一致性与前端

- 使用既有设计 token（`--accent/--warning/--danger/--bg-elevated/--gap-sm/--radius-sm/--font-mono` 均存在），provider 徽标按 local/LAN/cloud 用语义色，未硬编码红黄绿；侧栏 320px、固定折叠，符合收敛后的范围。
- 前端不直接发 HTTP、不持有 key、无 `v-html`；payload 以文本插值渲染（自动转义）。
- 插入编辑器使用 `setRangeText` + `input` 事件，保留 undo 栈与光标；AI 结果永不自动执行。
- zh-CN / en-US 的 `ai.*` 键完整且数量一致（phase/error/warning/block/sensitiveKind/sensitiveField 逐组比对无差异）；AI 组件无硬编码中文文案（仅注释）。
- 生成的 Wails 绑定包含全部 AI 方法，签名与 Go 一致；`vue-tsc --noEmit` 通过。

### 可验证性（本次实际执行）

- `go test ./internal/...`：全部通过
- `go test -race ./internal/ai ./internal/app -count=1`：通过
- `go vet ./internal/...`：无输出
- `go build ./...`：通过
- `npx vue-tsc --noEmit`：通过
- i18n 键对齐脚本：无缺失

---

## 四、必须修改（阻塞）

1. **`aiGate` 唤醒丢失**：被唤醒的等待者若同时被取消，空出的槽位不会交给下一个等待者，后续排队请求可能永久停在 `queued`。按第二节 2.3 的方案修复（取消返回路径持锁调用 `wakeLocked`），并补一个覆盖「被唤醒瞬间取消」的回归测试。这是上一轮 #7 的遗漏，不是新功能。

## 五、建议改进（不阻塞）

- `AIGenerate` 发送时重读 `AllowWrite`（当前用 Prepare 快照）；配置中任何安全相关字段变化都应要求重新预览。
- 停止请求后递增 `runToken` 并清空 `streamingText`，避免取消竞态下迟到结果仍渲染。
- 对表名/库名做控制字符过滤；prompt 中所有来自 DB 的标识符都按不可信输入处理。
- 敏感内容提示覆盖 schema DDL（注释里的连接串/密码同样会被发送）。
- 设置弹窗的 endpoint/scope/consent 展示目前来自「已保存配置」，用户编辑草稿 URL 时显示会滞后；建议标注「已保存」或按草稿实时计算。
- provider 切到 `openai-compatible` 时不清空/不清空旧 baseUrl，容易把本地地址带到云端 provider 预设里；建议显式置空并提示填写。
- `AISaveConfig` 先删旧 key 再存新 key/配置：若 keychain 写入失败，旧配置已丢 key。可改为「新 key 写入成功后再清理旧 key」。
- `grantCloudConsent` 的日志固定写 `scope=cloud`，LAN 授权也会这样记；改成 `scope=non-local` 更准确。
- 本机 endpoint 的首次确认按 endpoint 记住、不随 payload 变化刷新；若 schema 变化较大，可考虑按 `payloadHash` 触发一次预览确认。
- 合并前处理工作区里两个未提交的技能文件改动（`.claude/skills/panoramix-design/SKILL.md`、`panoramix-qa/SKILL.md`），避免把流程笔记混进功能提交。

## 六、风险提示

- **准确性风险**：schema-only 对值相关查询天然弱（BIRD 人类 92.96% vs GPT-4 54.89%，Spider 2.0 仅 21.3%）；UI 已持续提示复核，不要对外宣称「准确」。
- **隐私残余**：schema 本身是商业信息；非本机 provider 的日志/留存不可控；预览是唯一可信边界。DNS 分类只是提示不是隔离，真正的强制项是「默认关闭 + 不跟随重定向 + 按 endpoint 授权」，这三点已落实。
- **keychain 可用性**：WSL/Linux 无 Secret Service 时保存 key 会直接报错（未静默降级到 SQLite，这是正确行为）；M4 主密码回退尚未实现，设置页需要足够清楚的提示。
- **现有功能回归面**：`ShowCreateObject`/`isQuery` 是两个既有函数的行为变更，建议发布前用真实 MySQL 跑一遍结构 DDL 与 `EXPLAIN`/`WITH`/`INTO OUTFILE` 回归。
- **并发资源**：默认并发 1 是底线；修复唤醒缺陷后，多标签排队行为才与文档一致。

## 七、一句话结论

隐私与安全边界这次是真落实了（预览=实发、key/consent 绑定 endpoint、DDL 脱敏、写操作人审），测试也足够硬；只剩 `aiGate` 一个低概率但真实的排队唤醒缺陷——修掉它并补测试，v0.3.1 可以合并发布。

