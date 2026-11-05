---
name: panoramix-design
description: Panoramix 的产品设计与视觉规范。包含竞品洞察、设计系统变量、交互模式与已否决方案。designer 在设计与评审时使用。
---

# Panoramix 设计技能包

> 每次设计迭代后由 designer 更新。只记结论与依据，不记过程。

## 一、产品定位（不要动摇）

- **给谁**：国内外开发者/运维，从 Xshell / FinalShell / Navicat 迁移
- **凭什么**：一体化（终端 + SFTP + 监控 + 数据库 + K8s 在同一 UI）+ 单二进制 + 零遥测
- **不做什么**：不做 IDE、不做重型监控平台、不做云控制台替代品

## 二、竞品关键做法（调研结论）

| 竞品 | 值得学的 | 不值得学的 |
| --- | --- | --- |
| **HeidiSQL** | 表的固定标签（表/数据/查询）；表结构编辑器（基本/索引/外键/DDL 子标签）；数据网格内联编辑 + 顶部 ✓/✕ 批量应用；库→表树带行数估算 | 界面偏老（Win32 风格）；不支持多数据库类型统一抽象 |
| **TablePlus** | 极简高级的视觉；连接分组与标签页；结果网格的单元格复制体验；`Cmd+F` 多条件筛选、`Cmd+Option+F` 列可见性 | 商业授权 |
| **Navicat** | 编辑器内 Ask AI（生成/优化/格式化/修复/解释/方言转换）；云端 + Ollama/LM Studio；本地模型下表结构、样例数据、查询不出本机；数据同步/结构对比 | 云端默认、体量过大、商业授权 |
| **Xshell** | 会话管理、快速命令、分屏 | 界面陈旧 |
| **FinalShell** | 终端 + 监控 + 文件面板一体；目录同步 | 监控面板信息密度低、图表简陋 |
| **Termius** | 跨端同步、现代视觉；Autocomplete 内 AI 命令生成；AI Agent 执行前需确认 | 云依赖、订阅制 |
| **WindTerm** | 极低内存、终端性能 | 配置复杂 |
| **DataGrip** | AI text-to-SQL 读取真实 schema；官方称 AI 不共享/不访问库中数据；支持自选/本地模型；可 AI 解释执行计划 | 订阅/JetBrains 生态；自动执行验证不适合我们 |
| **Warp** | 右键解释终端报错；AI 命令建议；本地分类器触发；发送前 regex 脱敏 | 终端输出仍会发给 provider；Agent 可自动执行 |
| **RedisInsight** | 类型/内存/TTL 分布分析、按前缀聚合、批量删除、SCAN、约 30s/GB | 分析成本高、界面偏重 |
| **k9s** | 资源类型列表 + `Enter` 下钻 / `Esc` 返回；`l` 日志、`s` shell、`d` describe、`y` YAML；`Ctrl+T` 时间戳、`Ctrl+W` 换行、`Ctrl+S` 保存；`--readonly` 只读模式；头部数字键切命名空间、`0` 全部 | 命令模式（`:pod`）与 TUI 密度需要背语法；`--readonly` 不能替代 RBAC |
| **Lens** | Catalog→Clusters 多集群入口；左侧 Workloads/Network/Config/Storage/Namespaces/Events 分类 + 中间列表 + 右侧详情三栏；Pod 三点菜单（Details/Shell/Get logs/Edit/Delete）；日志选项含过滤、时间戳、上一次性、换行、下载、时间范围 | Electron 体量、商业授权；把 `~/.kube/config` 扫描当唯一入口 |
| **Headlamp** | CNCF/SIG-UI，Dashboard 归档后的官方推荐；RBAC 感知（无权限隐藏修改控件，日志/exec 同样受控）；多集群 + Projects；无 `list namespaces` 权限需降级命名空间来源 | web/in-cluster 形态、插件市场、应用分组不属于我们 |
| **kubectx/kubens** | `kubectx`/`kubens` 一命令切换；`-` 回到上一个；fzf 交互；常配 `kube-ps1` 常驻显示 context/namespace | 把 context 当用户要管理的对象；我们一个集群条目绑定一个 context |

### 数据网格专项（2026-11-05 调研，详见 `docs/design/003-data-grid-v0.3.2.md`）

| 竞品 | 具体做法 | 我们的取舍 |
| --- | --- | --- |
| **TablePlus** | `Cmd+F` 筛选；多条件默认 AND，`Apply All` 可切 OR；SQL 按钮回看生成查询；`Cmd+Option+F` 列选择（Apply/Clear）；quick filter | 取筛选面板 + 显式 Apply + SQL 回看 + 列选择器 + 快捷筛选；弃 OR（交给高级 WHERE）、客户端过滤 |
| **HeidiSQL** | Filter 面板左侧原始 WHERE、右侧多列搜索（自动 OR）；Apply Filter；右键 Quick filter；Shift 点击追加；#1879 切换表后旧筛选文本残留、需编辑才重新生效 | 取原始 WHERE 作为高级模式、快捷筛选追加、显式应用；弃跨列 OR、客户端过滤、切表保留旧筛选 |
| **DataGrip** | `Ctrl+F` 当前页搜索 + Filter Rows；列头 Local Filter（客户端，条件会记住）；右键 Quick filter（服务端，仅数据库对象数据）；WHERE 框 | 取服务端快捷筛选 + WHERE 框；弃客户端 Local Filter、按列记住值列表 |
| **DBeaver** | WHERE 式过滤框；`F11`；`Ctrl+F11` 按唯一值筛选；Configure columns 显隐/排序；过滤设置可保存到对象 | 取快捷按值筛选 + 列配置入口；弃保存筛选、列顺序持久化、唯一值列表 |

共同结论：**单值快捷筛选 + 多条件构建器是双层共识；显式 Apply 是共识；SQL 可回看是信任功能；列可见性与筛选是两件事；筛选持久化是分歧点。**

**评审后修正（2026-11-05，见 `docs/design/003-review.md`）：**

- **列注释不是 `DESCRIBE` 能拿到的**：MySQL `DESCRIBE` 只返回 `Field/Type/Null/Key/Default/Extra`，`Comment` 恒空。要注释必须用 `SHOW FULL COLUMNS`（推荐，直接替代、顺序一致、权限不变）或 `information_schema.COLUMNS.COLUMN_COMMENT`。同时 `Default` 要区分 SQL NULL 与空字符串，不能再靠 `Null == YES && d == ""` 猜。
- **Wails 错误不能靠 sentinel 身份**：Go `error` 过绑定只剩字符串。统一 `code: message`，前端按 code 映射 i18n（参照 `internal/app/ai.go` + `AIPanel.vue`）。
- **单元格写入必须参数化 + 事务**：`sqlLiteral` 只双写单引号，反斜杠会写坏/篡改数据；逐条 `DBQuery` 无事务会留下“部分成功”。`RowUpdate{PK,Set map[string]*string}` + `?` 占位符 + 单事务；nil=SQL NULL，空串≠NULL。
- **待提交保护要覆盖换连接**：`watch(activeId)` 会 `tabs.value = []`，比换表更隐蔽。保护必须在动作入口、状态变更前；`loadTableData` 用显式 intent 收口，不靠逐处补丁。
- **高级 WHERE 下快捷筛选**：选“禁用并提示先切回结构化”，不弹确认、不拼 RawWhere；面板有草稿时快捷筛选追加到草稿。
- **模式切换只换编辑表面，不换 Applied 来源**：`appliedSource: structured | raw`；预填只是 Sealed 表示，用户改文本并应用后才切到 raw，避免 `NO_BACKSLASH_ESCAPES` 语义漂移。
- **RawWhere 校验用词法器**（复用 `scanTopKeywords`），不用 `strings.Contains`；必须拒绝 `/*!50000 ... */` 版本注释，允许字符串内的 `;`/`--`，列名 `order`/`group` 不误杀，前导 `WHERE` 只去一次。

## 三、视觉规范（已在 `src/styles/global.css` 落地）

- 背景层次：`--bg-app #10141a` → `--bg-panel #1a2028` → `--bg-elevated #232a35`
- 文本：`--text-primary #e9edf3` / `--text-secondary #a9b3c1` / `--text-muted #74808f`
- 主题色：`--accent #5eead4`（青绿）；语义色 info/warning/danger
- 圆角 8px；间距 4/8/12/16
- 数据用等宽字体 `--font-mono`，正文用 Inter
- **禁止硬编码色值**，一律用变量

## 四、交互模式（已确立）

- **固定标签**：表/数据不可关闭；查询至少保留一个
- **打开表**：单击 → 创建「表」「数据」两个固定标签并激活「表」，数据自动加载
- **危险操作三步**：表单 → SQL 预览 → 确认执行
- **树交互**：单击打开结构/数据，右键菜单做管理操作
- **空状态**：图标 + 主提示 + 操作指引（如"按 Ctrl+Enter 执行"）
- **AI 助手（2026-11 设计）**：查询标签右侧可折叠侧栏，默认关闭；生成结果只插入编辑器、永不自动执行；首次向某个 provider 发送前展示完整 payload 预览；provider 徽标区分本机/局域网/云端
- **AI 隐私三层**：本地模型默认 → 只发送 schema 元数据（不发送行数据/结果集/历史）→ 类型系统保证请求结构没有行字段
- **结构化筛选优先**：数据标签的列可见性 + 多条件 AND 筛选器优于让用户手写 WHERE；生成的 SQL 必须可回看
- **数据标签筛选模型（v0.3.2）**：结构化条件行 + 显式「应用筛选」+ chip 回显 +「预览 SQL」/「查看已执行 SQL」（两个文案必须区分）；最多 5 条、只 AND；原 WHERE 保留为「高级 WHERE」模式，与结构化**互斥**；结构化→高级无损预填但**不改变 Applied 来源**（`appliedSource`），高级→结构化不解析、需确认；筛选条件**不持久化**（静默筛选风险），列可见性按 `connId+db+table` 持久化
- **列可见性**：工具栏「列 可见/总数」弹层，隐藏数 >0 时按钮用 `--warning` 强调；搜索名称/注释（注释来自 `SHOW FULL COLUMNS`）、全选/清空作用于搜索结果、至少保留一列；不做拖动排序/列宽持久化；隐藏只影响显示与 CSV，`SELECT *` 不变（保证排序/筛选/隐藏列待提交编辑稳定）；CSV 按钮写「CSV（可见列）」并 toast 隐藏列数
- **快捷筛选**：单元格右键「按此值/包含此值/排除此值」，列头右键「按此列筛选/隐藏此列」；**仅结构化模式可用**，高级 WHERE 模式禁用并提示先切回；始终追加到现有条件、不自动应用；面板有草稿时追加到草稿；NULL 只给 IS NULL/IS NOT NULL；有未提交修改的单元格用数据库当前值并在菜单项直接标出
- **待提交编辑保护（PR0，先于筛选上线）**：任何重新查询/换表/**换连接（含切换 Redis）**/关闭标签/结构 DDL 在有待提交编辑时必须三选一「取消 / 丢弃并继续 / 应用修改并继续」；先保护后改状态；`loadTableData` 用显式 intent 收口；换连接按标签聚合，不能等 `watch(activeId)` 清空后再弹
- **筛选值处理**：一律文本输入（不用 NInputNumber，避免 bigint 精度丢失）；服务端参数化；`contains` 用 `LIKE ? ESCAPE '!'`，转义 `!` `%` `_`；NULL 与空串分开；raw WHERE 只接受谓词片段，用词法器拒绝顶层 `;`、注释（含 `/*!...*/`）、ORDER/GROUP/HAVING/LIMIT/OFFSET/UNION
- **单元格写入**：前端不拼 SQL；`DBUpdateRows` 参数化 + 单事务，失败整批回滚；`nil`=SQL NULL，空串≠NULL；不做 RowsAffected 冲突检测（同值更新会误报）
- **K8s 是第三个一级视图**（2026-11 设计）：与「终端 / 数据库」并列、全屏；不塞进 264px 连接树或终端标签
- **集群入口绑定 SSH 主机**：主机右键「发现 Kubernetes」；连接成功后自动探测（仅 `test -r`，不读文件），发现候选显示徽标 + 一次性提示；未主动确认不添加
- **K8s 标签模型**：一个固定「浏览器标签」（资源类型 + 命名空间原地更新）+ 多个可关闭详情标签；详情内用子标签（概览/日志/终端/YAML/事件），`v-show` 保活
- **隧道不落盘、不监听**：kubeconfig server 不改写，client-go 自定义 `Dialer` 走 SSH channel；TLS 用 kubeconfig 自带 CA；不生成重写后的 kubeconfig 文件，不开本地端口
- **K8s 凭据加密**：每个集群一把数据密钥存 OS keychain，kubeconfig AES-256-GCM 密文存 SQLite；删集群先删密钥（加密擦除）；前端永远拿不到 kubeconfig
- **日志不是终端**：Pod 日志用纯文本视图（搜索/换行/时间戳/上一次性/暂停滚动/缓冲上限），不套 xterm；终端才用 xterm
- **只读优先**：v0.4 无删除/扩缩容/重启/编辑；Pod 终端用常驻提示说明「操作会影响该容器」，不造假的三步确认

## 五、已否决方案（不要重复提）

| 方案 | 否决原因 |
| --- | --- |
| uPlot 图表 | 交互与美观不足，用户明确要求改用 ECharts |
| ZMODEM 原生协议实现 | 协议流与终端输出混杂，风险高；改用 rz/sz 命令接管 → SFTP |
| 自动 COUNT 大表 | 会锁表/卡死；改为"返回满页即可下一页" |
| 数据标签本地排序 | 分页数据本地排序无意义；改为服务端 ORDER BY |
| 插件的微内核架构 | AI 协作下统一 module 更利于全局重构 |
| AI 自动执行 SQL / Agent 自动探索 | 公开基准（Spider 2.0 仅 21.3%、BIRD GPT-4 54.89%）不足以支撑自治执行；风险不可接受，改为"生成→人审→手动执行" |
| AI 默认发送样例行数据 | 隐私边界不可控；BIRD 表明值对准确率重要，但改为用户在问题里手动补值，不默认发送 |
| 应用内下载/打包本地模型 | 包体与维护成本高；只连接用户已运行的 Ollama/LM Studio |
| 本轮做终端 AI 报错解释 | 终端输出敏感信息多、脱敏不可靠；延后到 v0.5，与 AI 运维同轮 |
| 把数据标签增强并入 AI 助手同轮 | 范围过大；拆为 v0.3.1（AI）与 v0.3.2（筛选/列可见性） |
| 默认持久化筛选条件 | 打开表看到的是子集却不知道；HeidiSQL #1879 的旧筛选残留是前车之鉴。改为只持久化列可见性 |
| 客户端/当前页过滤 | 与服务端分页语义冲突，会得出“本页没有 ≠ 表里没有”；筛选必须服务端执行 |
| 结构化筛选 v1 支持 OR/嵌套 | 条件优先级与 UI 复杂度上升；OR 交给高级 WHERE |
| 数据标签内接入 AI / AI 生成筛选 | 结构化条件行已把手动成本降到 10–20 秒，AI 无 2 倍收益；数据标签常驻行数据，放 AI 按钮会模糊隐私边界。只保留确定性「在查询标签打开」桥接 |
| v0.3.2 做服务端列投影（只 SELECT 可见列） | 切换可见性需重新查询，隐藏列待提交编辑/排序/CSV 语义复杂；先保持 `SELECT *`，后续评估 |
| v0.3.2 做列拖动排序/列宽持久化 | 需与原始列索引、编辑、CSV、结构标签协调；本轮只做可见性 |
| raw WHERE 允许顶层 ORDER BY/LIMIT/UNION | 会与系统追加的 ORDER/LIMIT 拼成双重子句或改变结果集结构；高级模式只接受谓词片段 |
| 筛选值下拉 / DISTINCT 值列表 | 需额外查询、可能传输大量数据、性能不可控；快捷筛选 + 手动输入足够 |
| 用 `DESCRIBE` 取列注释 | MySQL `DESCRIBE` 不返回 Comment；改 `SHOW FULL COLUMNS` |
| 高级模式下把快捷条件拼进 RawWhere | 需要字面量转义、制造第二真相、与互斥原则冲突；改为禁用快捷筛选并提示先切回结构化 |
| 保护只覆盖重新查询/切表 | 漏掉 `watch(activeId)` 清空全部标签、切换 Redis、关标签；这些同样是静默丢失 |
| 前端拼 UPDATE / 逐条无事务 | 反斜杠/引号会写坏数据，部分失败无法解释；改参数化 + 单事务 |
| 前端按英文错误 message 匹配哨兵 | Wails 只传字符串，文案一改就碎；统一稳定 code + `code: message` |
| 预填后立即按 RawWhere 执行 | 内联字面量在 `NO_BACKSLASH_ESCAPES` 下与参数化语义不同；预填只做展示，改文本并应用后才切 Applied 来源 |
| 本轮做元数据跨请求缓存 | 外部 DDL 与失效复杂；先每次 `SHOW FULL COLUMNS`，延迟明显再评估 |
| 本轮做 RowsAffected 写入冲突检测 | 同值更新默认返回 0 行会误报；整批事务已保证原子性，外部删行刷新可见 |
| 用 SQL 解析器把高级 WHERE 转条件行 | OR/函数/子查询解析失败或语义偏差风险高；只做词法级安全校验与预填 |
| K8s 集成进连接树/终端标签 | 资源表 + 详情需要 ≥1000px；会污染「一个终端标签 = 一个或多个 PTY pane」的模型；DB 已有全屏一级视图先例 |
| 改写 kubeconfig 的 server + 本地端口转发 | 需要把凭据写成新文件、开本地 listener、管理端口生命周期；改用 custom Dialer 直接走 SSH channel |
| 用遥控 `kubectl` 子命令实现浏览/日志/exec | 依赖远端 kubectl、解析文本输出、无结构化 watch；改为 client-go 直连 API |
| 用 xterm 渲染 Pod 日志 | 日志需要搜索/换行/时间戳/暂停/导出，不需要 VT 仿真；增加渲染成本且不利于长行 |
| 自动发现即自动导入集群 | 会在用户未确认时复制高权限 kubeconfig；改为探测仅存在性、候选需显式添加 |
| v0.4 做「全局所有主机总览」 | 需要同时对多主机建 SSH + API 连接，失败面与复杂度成倍；顺延 v0.4.1，先做扎实单集群 |
| v0.4 浏览 Secret / ConfigMap | Secret 值进入 UI 的泄漏面大；本轮范围只到 Workload/Pod/Event/Namespace |

## 六、路线状态（2026-11-05）

> v0.3.1 已发布；v0.4（K8s 面板）设计在总监评审后被打回，用户决定顺延。**本轮做 v0.3.2 数据标签增强**，设计见 `docs/design/003-data-grid-v0.3.2.md`，**已按总监评审修订（v1.1），待用户确认关键取舍**。

1. **v0.3.1：AI SQL 助手**——已完成（生成/解释/修复、本地默认、只发 schema、两阶段预览、永不自动执行）
2. **v0.3.2：数据标签增强**——设计已按评审修订，待用户确认：列显示/隐藏（按连接+库+表持久化）、结构化多条件 AND 筛选器（与高级 WHERE 互斥）、单元格/列头快捷筛选、待提交编辑保护；**PR0（保护 + 参数化写入）先行，排期 2 周**；不做 OR/嵌套、不做筛选持久化、不做自动 COUNT、不做数据标签 AI
3. **v0.4：K8s/K3s 面板**——`docs/design/002-k8s-panel.md` 被打回并顺延；本轮不实现。范围仍为第三个一级视图、SSH 发现为主、NS/Workload/Pod/Event 浏览 + 日志 + Pod 终端、严格只读
4. **v0.4.1 候选**：全局「固定集群」轻量总览、端口转发、主密码回退（Linux keychain 不可用时）
5. **v0.5：YAML 编辑 + AI 运维**——编辑→diff→apply；终端选中文本解释/命令建议，只插入不回车
6. **Backlog**：Redis 键空间分析、CodeMirror 6 语法高亮、多结果集、PostgreSQL；数据标签的列投影下推、保存筛选方案、筛选查询取消
