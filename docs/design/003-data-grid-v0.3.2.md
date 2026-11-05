# 数据标签增强（v0.3.2）

> 文档编号：003-data-grid-v0.3.2  
> 日期：2026-11-05  
> 作者：designer  
> 状态：**v1.1 已按评审修订 / 待用户确认**  
> 前置：v0.3 数据标签基础能力（WHERE 片段、服务端排序、分页、CSV、内联编辑 + 批量应用）；v0.3.1 AI SQL 助手已发布  
> 关联：`docs/design/001-next-iteration.md` 方向 B；`ROADMAP.md` v0.3.2；评审报告 `docs/design/003-review.md`

## 修订记录（2026-11-05，按 003-review）

| 评审阻塞项 | 处理方式 |
| --- | --- |
| 1. 列注释来源不存在 | `DescribeTable` 改为 `SHOW FULL COLUMNS`，`Comment` 才真实可用；选择理由与默认值语义见 §4.6，验收见 §7.2 |
| 2. 高级 WHERE 下快捷筛选未定义 | 选方案 (a)：**禁用并提示「先切回结构化筛选」**，不弹确认、不改谓词；筛选面板有未应用草稿时快捷筛选追加到草稿（§3.3.6、§7.5） |
| 3. 错误哨兵跨 Wails 边界丢失 | 统一为项目既有的 `code: message` 字符串约定，定义稳定错误码表；前端按 code 映射 i18n（§4.4、§7.7） |
| 4. 待提交保护漏连接切换 | 保护拆为**独立 PR0 前置**；覆盖 `watch(activeId)` 清空全部标签、切换 Redis、结构标签 DDL；状态变更必须在保护通过后（§3.3.8、§4.8） |
| 5. `applyEdits` 字符串拼 UPDATE | 新增参数化 `DBUpdateRows`：Go 侧 `?` 占位符 + 单事务 + 1000 行上限；前端不再拼 UPDATE（§4.5、§7.6） |
| 6. 排期 | 按 **2 周**排；保护（含参数化写入）作为第一个可验收增量（§4.8） |
| 7. RawWhere 校验与预填语义 | 复用 `scanTopKeywords` 做词法级校验（顶层分号/注释/子句、版本注释、字符串内关键字）；预填不切换 Applied 来源（§4.4） |
| 8. AI 边界表述过绝对 | 改为「本轮不新增任何自动发送路径；用户主动发送含值 SQL 时仍受 v0.3.1 预览约束」（§2.3、§4.7、§5.2、§7.8） |

---

## 0. 结论先行

本轮只做一个可独立验收的增量：**让数据标签从“能看数据”变成“能高效阅读和定位数据”**。

三件事：

1. **列可见性**：工具栏「列 24/30」按钮 + 弹层；搜索、全选、清空；按 `connectionId + database + table` 持久化；至少保留一列。
2. **结构化多条件筛选**：条件行 + 显式「应用筛选」+ chip 回显 + 「预览 SQL」「查看已执行 SQL」；条件之间 **AND**；服务端参数化查询；不自动 COUNT。
3. **快捷筛选**：单元格右键「按此值筛选 / 包含此值 / 排除此值」，列头右键「按此列筛选 / 隐藏此列」；始终追加到现有条件，不静默覆盖；**仅结构化模式可用**，高级 WHERE 模式禁用并提示先切回（§3.3.6）。

关键取舍（详见 §9）：

- 现有 WHERE 输入框**保留为「高级 WHERE」模式**，与结构化筛选器**互斥**；结构化 → 高级可无损预填（只换编辑表面，不换 Applied 来源）；高级 → 结构化不解析：未应用修改需确认放弃，已应用 RawWhere 需确认清除。
- **筛选条件不持久化**，列可见性持久化。理由：静默筛选会让人误读数据范围；列隐藏有「列 x/y」可见证据。
- **CSV 只导出当前页的可见列**（与屏幕一致）。
- 本轮**不在数据标签接入 AI**；只提供确定性的「在查询标签打开」桥接。
- **前置修复**（不是顺带）：任何会重新查询、切表、**切换连接/切换 Redis** 的操作，在存在待提交编辑时必须先确认；同时把单元格写入改成参数化事务（§4.5）。这块作为 PR0 先交付。
- 列面板的注释搜索依赖后端元数据从 `DESCRIBE` 改为 `SHOW FULL COLUMNS`（§4.6）；注释为空时只按列名搜索。

---

## 1. 用户场景

### 1.1 谁在用

- 每天在数据标签里翻表、筛数据、改单元格的 MySQL 后端开发者、运维、数据分析人员。
- 已经会用 SQL，但不想为“看一眼数据”写一条完整 SELECT；v0.3.1 的 AI 助手解决“写查询”，不解决“读表格”。
- 从 HeidiSQL / Navicat / TablePlus 迁移过来的用户，默认期待：宽表能隐藏列、能多条件筛选、能从单元格值一键筛。

### 1.2 三个日常场景

**场景 A：宽表阅读（每天多次）**

> `users` 表 32 列，`avatar`、`profile_json`、`remark` 三列很宽。用户只想看 `id / email / status / plan / created_at / last_login`。现在每次打开都要横向滚动，主键和状态列不能同屏；切到另一张表再切回来，视野又回到最左边。

**现在的痛**：无法隐藏无关列；没有“列视图”记忆；宽表阅读效率低。

**场景 B：多条件定位（每天多次）**

> 线上支付失败率升高。用户要查 `status='failed'`、`created_at >= '2026-10-01'`、`email contains 'foo'` 的记录。现有 WHERE 输入框要求手写 `status = 'failed' AND created_at >= '2026-10-01' AND email LIKE '%foo%'`；引号、LIKE 转义、类型都容易错。输入过程中每敲一个字符不会查库，但按 Enter 后才看到错误，改一轮要几十秒。

**现在的痛**：写 WHERE 片段有语法/类型成本；没有操作符可选；没有“查看实际执行的 SQL”；出错后只能看 MySQL 报错猜。

**场景 C：从一个值出发（排障时高频）**

> 用户从告警邮件拿到一个订单号或邮箱，打开表后要手工复制值、拼 `WHERE email = '...'`。如果这个值里有 `'`、`%`、`_`，还要自己转义。

**现在的痛**：没有“以此值筛选”的路径；复制 + 手写 + 转义，平均 30–60 秒。

### 1.3 本轮之后的目标

| 场景 | 现在 | 目标 |
| --- | --- | --- |
| 宽表阅读 | 每次横向滚动，列位置不记忆 | 一次隐藏，按表恢复；主键/状态同屏 |
| 三条件筛选 | 手写 WHERE，30–90 秒 + 试错 | 条件行 10–20 秒；错误在应用前可见 |
| 单值定位 | 复制 + 手写 + 转义 30–60 秒 | 右键 2 次点击，5 秒内出结果 |
| 筛选后继续分析 | 复制条件回查询标签 | 「查看已执行 SQL」→「在查询标签打开」 |

### 1.4 本轮不解决的痛

- 不自动统计总行数（大表会卡死，沿用“返回满页即可下一页”）。
- 不做 OR/嵌套条件；需要时用高级 WHERE。
- 不做跨表 JOIN 构造器；需要时用查询标签。
- 不做筛选方案命名/保存/共享。
- 不做数据标签的 AI。

---

## 2. 竞品对比

> 调研时间：2026-11-05。以下为公开文档、论坛与 issue 的实际检索结论；未逐条复现全部交互。

### 2.1 数据库客户端

| 产品 | 具体做法 | 来源 | 我们取什么 / 放弃什么 |
| --- | --- | --- | --- |
| **TablePlus** | `Cmd+F` 打开筛选器；可加多条条件；默认 **AND**，`Apply All` 下拉可切 OR；勾选后 `Apply All` / `Cmd+Return`；**SQL 按钮**可查看生成的查询；`Cmd+Option+F` 打开列选择，勾选后 Apply，Clear 恢复全列；文档提到 quick filter | [Filter 文档](https://tableplus.com/docs/gui-tools/filter)、[快捷键](https://tableplus.com/docs/utilities/shortcut-keys) | 取：筛选面板 + 显式 Apply + SQL 回看 + 列选择器 + 快捷筛选；弃：本轮不做 OR（交给高级 WHERE）、不做客户端过滤 |
| **HeidiSQL** | 数据标签的 Filter 面板：左侧是原始 WHERE 片段，右侧是“多列筛选”框（跨列搜索，自动生成 OR）；点 Apply Filter 或 Enter 应用；右键单元格 Quick filter（当前值/输入值/剪贴板）；按住 Shift 点击会把条件追加到现有筛选；issue #1879 记录了切换表后旧筛选文本仍在、但需编辑后才重新生效的残留问题 | [多列筛选建议](https://www.heidisql.com/forum.php?t=26650)、[行筛选讨论](https://www.heidisql.com/forum.php?t=6240)、[全列搜索](https://www.heidisql.com/forum.php?t=17992)、[#1879](https://github.com/HeidiSQL/HeidiSQL/issues/1879)、[#292](https://github.com/HeidiSQL/HeidiSQL/issues/292) | 取：原始 WHERE 作为高级模式、快捷筛选、追加语义、显式应用；弃：跨列 OR 搜索、客户端过滤、切换表保留旧筛选（我们主动清空） |
| **DataGrip** | 结果网格 `Ctrl+F` 搜索并可勾选 Filter Rows（当前页本地过滤）；列头可开 Local Filter（客户端按列值过滤，条件会被记住）；右键单元格 Quick filter（服务端重新查询，仅对数据库对象数据有效）；工具栏 WHERE 输入框按 Enter 应用 | [Filter data](https://www.jetbrains.com/help/datagrip/tables-filter.html)、[Explore data](https://www.jetbrains.com/help/datagrip/explore-data-in-data-editor.html) | 取：服务端快捷筛选、WHERE 输入框、显式应用；弃：客户端 Local Filter、按列记住值列表 |
| **DBeaver** | 结果集上方 WHERE 式过滤框，Enter/按钮应用；`F11` 过滤菜单；`Ctrl+F11` 按选中列的唯一值筛选；Configure columns 可显示/隐藏列、排序；过滤设置可保存到数据库对象 | [Data Filters](https://dbeaver.com/docs/team-edition/24.3/desktop/Data-Filters/)、[DBeaver Wiki](https://github.com/dbeaver/dbeaver/wiki/Data-Filters) | 取：快捷按值筛选、列配置入口、WHERE 式高级模式；弃：保存过滤设置、列顺序持久化、客户端唯一值列表 |

### 2.2 调研结论

1. **双层交互是共识**：单值快捷筛选 + 多条件构建器。只做一个不够：快捷筛选解决“从值出发”，构建器解决“组合条件”。
2. **显式 Apply 是共识**。没有一家在每次敲键时就查库；筛选面板都有 Apply / Enter。
3. **SQL 可见性是信任功能**。TablePlus 的 SQL 按钮、DataGrip/DBeaver 的 WHERE 框，都让用户知道实际执行了什么。
4. **列可见性与筛选是两件事**。入口、持久化、应用时机都不同；不能合并成一个“视图”弹层。
5. **筛选持久化是分歧点**。从公开文档看，TablePlus/DataGrip 更偏会话内；DBeaver 可按对象保存。HeidiSQL 的旧筛选残留（#1879）是真实的前车之鉴。我们选择**不持久化筛选**，避免“打开表看到的是子集却不知道”。
6. **客户端过滤不适合我们**。DataGrip/TablePlus 的本地过滤只作用于已加载页；我们的分页是服务端 `LIMIT/OFFSET`，客户端过滤会得到错误语义（“本页没有”不等于“表里没有”）。

### 2.3 与我们的差异

- 我们**没有总行数 COUNT**，所以结果区必须写“本页 N 行”，不能写“N 行”。
- 我们的筛选**始终服务端执行**，隐藏列仍可参与筛选/排序（数据仍被 `SELECT *` 取出）。
- 我们**不做客户端值列表**（需要额外查 DISTINCT，隐私与性能都不合适）。
- 我们的隐私立场：筛选条件与列偏好只存本机；v0.3.2 **不新增任何自动 AI 发送路径**，也不进入遥测。注意：「在查询标签打开」会把含内联值的 SQL 放进编辑器；用户之后主动用 v0.3.1 的 explain/fix 发送时，这段 SQL（含值）会按既有规则进入 payload，仍受 v0.3.1 的发送前预览约束。这不是本功能自动发送的。

---

## 3. 交互设计

### 3.1 数据标签总览

数据标签保持现有结构：工具栏 → 结果元信息 → 网格。新增一条**条件行**，只在有筛选或高级模式时出现。

**无筛选：**

```
┌ 数据 ──────────────────────────────────────────────────────────────────────────────┐
│ [筛选] [列 32/32]                              [刷新] [结构]  [‹] 第 1 页 [›] [1000 ▾] │
│                                                              [CSV] [✓] [✕]           │
├────────────────────────────────────────────────────────────────────────────────────┤
│ 本页 1000 行 · 18ms                                                                │
│ #  │ id │ email │ status │ plan │ created_at │ ...                                  │
│ 1  │ 1  │ a@x   │ paid   │ pro  │ 2026-...   │ ...                                  │
└────────────────────────────────────────────────────────────────────────────────────┘
```

**有筛选：**

```
┌ 数据 ──────────────────────────────────────────────────────────────────────────────┐
│ [筛选 3] [列 24/32] [排序: updated_at ▼（已隐藏）×]  [刷新] [结构] [‹] 第 1 页 [›] ... │
│ 筛选条件  status = paid ×   created_at ≥ 2026-10-01 ×   email 包含 foo ×  [+ 条件]   │
├────────────────────────────────────────────────────────────────────────────────────┤
│ 本页 87 行 · 34ms · 查看已执行 SQL                                                  │
│ #  │ id │ email │ status │ created_at │ ...                                         │
└────────────────────────────────────────────────────────────────────────────────────┘
```

**高级 WHERE 模式：**

```
┌ 数据 ──────────────────────────────────────────────────────────────────────────────┐
│ [高级 WHERE ●] [列 24/32]                            [刷新] [结构] [‹] 第 1 页 [›] ... │
│ WHERE [ status = 'paid' OR amount > 100                    ] [应用] [预览 SQL] [返回筛选器] │
│       只填条件；不能包含 ;、注释、ORDER BY / GROUP BY / LIMIT / UNION                    │
│ ● 有未应用的修改                                                                    │
├────────────────────────────────────────────────────────────────────────────────────┤
│ 本页 120 行 · 29ms · 查看已执行 SQL                                                 │
└────────────────────────────────────────────────────────────────────────────────────┘
```

规则：

- 工具栏第一行始终存在；筛选/列按钮在左侧，分页/刷新/CSV/编辑在右侧。
- 条件行只在“结构化筛选有已应用条件”或“高级模式”时出现；无筛选时高度为 0，不占空间。
- 「筛选 3」的数字是**已应用条件数**，不是结果行数；无筛选时只显示「筛选」。
- 结果元信息中的「本页 N 行」明确表示当前页，不暗示总数。
- 「查看已执行 SQL」在条件行右侧，点击打开只读 SQL 浮层（最近一次实际执行的 SQL），可复制、可在查询标签打开。

### 3.2 列可见性

#### 3.2.1 入口

1. 工具栏「列 24/32」按钮（结果加载后可点；未加载时禁用并提示“数据加载后可用”）。
2. 列头右键 →「隐藏此列」（立即生效，不经过弹层）。
3. 不提供 `Ctrl+F` 之外的隐藏快捷键；列面板快捷键见 §3.6。

按钮显示 `可见列数 / 总列数`，不含行号 `#` 列。隐藏全部列被禁止。

**计数始终可见**（评审建议）：只要隐藏数 > 0，按钮使用 `--warning` 边框/文字强调，tooltip 写「已隐藏 N 列；只影响显示与 CSV」。这是「我的列去哪了」的唯一持续证据，不能只在弹层里显示。

#### 3.2.2 列面板

点击「列 24/32」打开锚定弹层（宽 320px，最大高 460px）：

```
┌ 列显示 24/32 ──────────────────────────────┐
│ [搜索列或注释…                        ]     │
│ [全选] [清空]          （作用于搜索结果）    │
│ ┌────────────────────────────────────────┐ │
│ │ ☑ id          bigint                   │ │
│ │ ☑ email       varchar(200)   邮箱       │ │
│ │ ☐ avatar      blob                     │ │
│ │ ☑ status      enum('new','paid',...)   │ │
│ │ ☑ created_at  datetime                 │ │
│ │ ...                                    │ │
│ └────────────────────────────────────────┘ │
│ 隐藏只影响显示与 CSV；查询仍读取全部列        │
│ 已显示 24 / 32                             │
│ [全部显示]                    [取消] [应用] │
└────────────────────────────────────────────┘
```

交互：

- **搜索**：按列名或列注释做大小写不敏感的子串匹配；无匹配时显示「没有匹配的列」。
  - 注释来自 `SHOW FULL COLUMNS` 的 `Comment`（§4.6），不是 `DESCRIBE`；注释为空时只匹配列名。
  - 元数据加载失败时列面板仍可用（只依赖 `result.columns` 的列名），搜索框 tooltip 提示「暂时无法读取列注释」。
- **全选**：勾选当前搜索结果中的所有列；**清空**：取消当前搜索结果中的所有列。按钮下方常驻小字“作用于搜索结果”，避免用户以为操作了全部列。
- **至少一列**：手动取消最后一个可见列时，复选框回弹并显示行内提示「至少保留一列可见」。点「清空」若会导致 0 列可见，保留表格顺序中的第一个当前可见列，并提示「已保留 id」。
- **全部显示**：清除隐藏偏好（所有列可见），保留搜索词。
- **应用 / 取消**：修改是草稿；点「应用」才写入网格与持久化。`Esc`、点弹层外 = 取消。应用不会重新查询数据库。
- 列表顺序 = 网格列顺序；本轮**不支持拖动排序**（见 §8）。
- 行内展示类型与注释 tooltip（均来自 `SHOW FULL COLUMNS`；`DescribeTable` 在同一查询里同时返回 `Field/Type/Null/Key/Default/Extra/Comment`）；注释参与搜索。
- 弹层底部提示“隐藏只影响显示与 CSV；查询仍读取全部列”，解释为什么不改 `SELECT *`。
- 打开列面板时自动关闭筛选面板，反之亦然。

#### 3.2.3 列头右键

```
┌ 列头右键（email）──────────┐
│ 按此列筛选…                │
│ 升序排序                   │
│ 降序排序                   │
│ ─────────────────────────  │
│ 隐藏此列                   │
│ 复制列名                   │
└────────────────────────────┘
```

- 「隐藏此列」立即隐藏、立即持久化；无弹层确认。若它是最后一个可见列，菜单项禁用。
- 若有活动中的单元格编辑且属于该列：先提交（commitEdit），再隐藏；该修改进入待提交列表。
- 「复制列名」写剪贴板并 toast「已复制列名」。
- 升序/降序复用现有服务端排序；触发前走待提交编辑保护（§3.3.8）。

#### 3.2.4 持久化

> 实现更新（v1.2）：按本轮协调者指令落在 `settings` 表，而不是 localStorage。
> 纯函数（key 编码、解析、求交、修复）仍在 `gridPrefs.ts`，读写通过绑定完成。

- 键：`grid.columns.<connId>.<database>.<table>`（settings 表）
- 值：`{ "v": 1, "hidden": ["avatar", "profile_json"] }`，只存列名，不存顺序。
- 读取时机：打开数据标签、切换表、应用后。写入时机：列面板应用、列头隐藏。
  读取 `GetGridPref`、写入 `SetGridPref`、删除 `DeleteGridPrefsByPrefix`。
- 恢复语义：
  - 切换结构标签/数据标签：保留（标签对象仍在内存，且按表读取）。
  - 切换到另一张表再切回：各自恢复。
  - 重启应用后重新打开该表：恢复。
  - 同一表的两个连接：互不影响。
- 列被删除/重命名：读取时取 `hidden ∩ result.columns`；未知列名忽略但保留在存储中（改名回来后仍生效）。应用时重写为当前有效集合。
- 存储损坏/版本不匹配/设置读取失败：当作“无隐藏”，不报错。
- 删除连接时：`DeleteDBConn` 按 `grid.columns.<connId>.` 前缀清理。
- 隐藏后若当前可见列为 0（手工改 settings 值或列被删）：自动显示第一列并回写修复后的集合。

#### 3.2.5 与结构标签的关系

**明确：不联动，结构标签始终显示完整列定义。**

| 事件 | 数据标签 | 结构标签 |
| --- | --- | --- |
| 隐藏列 | 网格不渲染该列；CSV 不含该列 | 不变，仍显示全部字段与 DDL |
| 在结构标签加列 | 标记列元数据过期；下次加载刷新，新列默认可见 | 正常刷新 |
| 在结构标签改名 | 旧隐藏名失效，新列默认可见；引用旧列的筛选条件被移除并提示 | 正常刷新 |
| 在结构标签删列 | 引用该列的筛选/排序被清除并提示；隐藏偏好中的旧名忽略 | 正常刷新 |
| 列类型改变 | 刷新后重新计算可用操作符；不再支持的条件被移除并提示 | 正常刷新 |

结构标签是 schema 的真相来源；数据标签的隐藏集合只是**阅读偏好**，不改变数据库、不改变 DDL、不影响查询的 `SELECT *`。

#### 3.2.6 实现注意（给 engineer）

- 不要修改 `result.columns` 或行数组；构造 `visibleRefs = result.columns.map((name, idx) => ({ name, idx })).filter(...)`。
- 模板遍历 `visibleRefs`；所有 `pendingValue`、`startEdit`、`selectedCol`、`editing.col`、`copyCell` 使用 `ref.idx`（原始列索引），显示用 `ref.name`。
- `applyEdits` 继续使用 `result.columns` 与原始列索引构造待写入批次，但**不再生成 SQL 字符串**：把 `{主键列值, SET 列值}` 交给参数化绑定 `DBUpdateRows`（§4.5），因此隐藏列上的待提交编辑仍能正确写入。
- CSV 遍历 `visibleRefs`，其余逻辑不变。
- 若排序列为隐藏列：保留排序，工具栏显示 `排序: col ▼（已隐藏）×`；点击标签切换方向，× 清除排序。
- 隐藏列不渲染，因此不能从单元格右键；需要筛选隐藏列时走筛选面板的列选择器（带「隐藏」标记）。

### 3.3 多条件筛选器

#### 3.3.1 与现有 WHERE 的关系：互斥，两种模式

**结论：结构化筛选器是主模式；原 WHERE 输入框保留为「高级 WHERE」模式；两者互斥，不会同时生效。**

理由：

1. 两种谓词同时 AND 会让用户难以判断“结果为什么少”，也让 SQL 预览复杂。
2. 高级模式可以覆盖 OR、函数、子查询等结构化操作符表达不了的条件。
3. 互斥后，“当前生效的条件”只有一个来源：由 `appliedSource` 决定看条件数组还是高级文本；模式切换只是换一种编辑表面，不制造第二真相。

模式规则：

- 默认结构化模式。数据标签的工具栏按钮显示「筛选 N」。
- 结构化面板底部提供「使用高级 WHERE」。
- **结构化 → 高级**：
  - 若没有已应用条件，直接切换，输入框为空。
  - 若有条件，后端 `DBPreviewTableSQL` 返回可读 `WHERE` 片段，预填到高级输入框；同时在前端保留条件快照。此时**不重新查询**，谓词不变。
  - 若预填失败（列已被删除/重命名、条件不合法）：留在结构化模式并显示错误，不进入高级模式。
  - 输入框内容仍是“已应用”的（不是草稿）；用户编辑后才出现「● 有未应用的修改」。
  - **Applied 来源不因切换而改变**（评审修正）：引入 `appliedSource: 'structured' | 'raw'`。刚切到高级模式时 `appliedSource` 仍是 `structured`，执行仍走参数化条件数组；只有用户**修改文本并点「应用」**后，`appliedSource` 才变为 `raw`，此后执行走 RawWhere。这样避免了「预填把参数内联成字面量、再按 RawWhere 执行」在 `NO_BACKSLASH_ESCAPES` 会话下与参数化路径语义不一致。文本未变时「应用」禁用，不触发无意义查询。
- **高级 → 结构化**：
  - 若高级文本为空，或与进入高级模式时预填的文本完全一致：直接恢复条件快照（或空条件），不重新查询。
  - 若用户改过高级文本但**从未应用**（`appliedSource === 'structured'`）：弹确认，但语义是「放弃草稿」而不是「清除条件」：

```
┌ 返回筛选器 ────────────────────────────────────┐
│ 高级输入框里还有未应用的修改。                  │
│ 返回筛选器会丢弃这些修改；当前筛选条件不变。    │
│ [复制 WHERE]              [取消] [放弃并返回]   │
└─────────────────────────────────────────────────┘
```

    「放弃并返回」直接恢复条件快照，**不重新查询**（当前结果本来就对应这份条件）。
  - 若用户改过并**已应用**（`appliedSource === 'raw'`）：高级 WHERE 不能自动转换为结构化条件，弹确认：

```
┌ 返回筛选器 ────────────────────────────────────┐
│ 高级 WHERE 不能自动转换为结构化条件。           │
│ 返回筛选器会清除这段 WHERE。                    │
│ 建议先复制需要的条件。                          │
│ [复制 WHERE]              [取消] [清除并返回]   │
└─────────────────────────────────────────────────┘
```

    「清除并返回」清空高级条件并切回结构化（无筛选）；这是一次查询，走待提交编辑保护。
  - **不做 SQL 解析器**。不尝试把 `OR`、函数、子查询转成条件行；解析失败比不解析更糟。
- 切换表：**保留当前数据标签的模式**（评审建议：不惩罚老用户），但清空条件、高级文本、预填快照与草稿；`appliedSource` 重置为 `structured`。
- 模式不持久化到磁盘；重启后默认结构化。会话内按数据标签记住 `filterMode`，新建数据标签默认结构化。

#### 3.3.2 筛选面板

点击「筛选」打开锚定弹层（宽 560px）：

```
┌ 筛选条件 ─────────────────────────────────────────────────────┐
│ 以下条件同时满足（AND）                       最多 5 条         │
│ ┌ 列 ──────────────┬ 操作符 ────────┬ 值 ───────────────┬──┐ │
│ │ status         ▾ │ 等于 (=)     ▾ │ paid              │× │ │
│ │ created_at     ▾ │ 大于等于 (>=) ▾ │ 2026-10-01        │× │ │
│ │ email          ▾ │ 包含          ▾ │ foo               │× │ │
│ └──────────────────┴────────────────┴───────────────────┴──┘ │
│ [+ 添加条件]                                                   │
│ ───────────────────────────────────────────────────────────── │
│ ▸ 预览 SQL                                                     │
│ [清除筛选]                                     [取消] [应用筛选] │
└───────────────────────────────────────────────────────────────┘
```

- 数据未加载或列元数据不可用时，筛选按钮禁用并提示“数据加载后可用”。
- 打开时：无已应用条件 → 一行空条件，列默认第一列，操作符默认 `=`；有已应用条件 → 回填。
- 点 chip 打开面板时，聚焦对应行。
- 「+ 添加条件」在 5 条时禁用，tooltip「最多 5 条；更多请用高级 WHERE」。
- 条件间固定 AND，没有 OR 开关；面板顶部与底部都有提示。需要 OR 时用高级 WHERE。
- 条件行是草稿；修改不查库。`Ctrl+Enter` 或「应用筛选」才执行一次查询。
- 「清除筛选」：清空已应用条件并执行一次无筛选查询；不是只清草稿。需要撤销草稿改动时用「取消」。
- 「取消」/`Esc`/点外：丢弃草稿，已应用条件不变。
- 应用成功后关闭面板；失败时关闭面板并在结果区显示错误 +「编辑筛选」入口（见 §3.4）。
- 更改列时：操作符与值重置为该列类型的合法默认值；已填值尽量保留（如果新类型仍合法）。

#### 3.3.3 操作符与值类型

操作符集合（本轮固定，不开放自定义）：

| UI 文案 | 内部值 | 生成 SQL | 值输入 | 适用类型 |
| --- | --- | --- | --- | --- |
| 等于 | `eq` | `` `col` = ? `` | 文本 | 字符/文本、数值、日期、枚举/集合 |
| 不等于 | `ne` | `` `col` <> ? `` | 文本 | 字符/文本、数值、日期、枚举/集合 |
| 大于 | `gt` | `` `col` > ? `` | 文本 | 数值、日期时间 |
| 大于等于 | `gte` | `` `col` >= ? `` | 文本 | 数值、日期时间 |
| 小于 | `lt` | `` `col` < ? `` | 文本 | 数值、日期时间 |
| 小于等于 | `lte` | `` `col` <= ? `` | 文本 | 数值、日期时间 |
| 包含 | `contains` | `` `col` LIKE ? ESCAPE '!' `` | 文本 | 字符/文本、JSON（警告） |
| 为空 | `is_null` | `` `col` IS NULL `` | 无 | 所有 |
| 不为空 | `is_not_null` | `` `col` IS NOT NULL `` | 无 | 所有 |

类型判定（来自 `SHOW FULL COLUMNS` 的 `Type` 小写前缀）：

| 类型族 | 判定前缀 | 可用操作符 |
| --- | --- | --- |
| 数值 | `tinyint` `smallint` `mediumint` `int` `integer` `bigint` `decimal` `numeric` `float` `double` `real` `bit` | `= != > >= < <= IS NULL IS NOT NULL` |
| 日期时间 | `date` `datetime` `timestamp` `time` `year` | `= != > >= < <= IS NULL IS NOT NULL` |
| 字符/文本 | `char` `varchar` `tinytext` `text` `mediumtext` `longtext` | `= != contains IS NULL IS NOT NULL` |
| 枚举/集合 | `enum` `set` | `= != IS NULL IS NOT NULL`（集合的“包含”语义需 `FIND_IN_SET`，走高级 WHERE） |
| JSON | `json` | `contains IS NULL IS NOT NULL` + 警告 |
| 二进制/大对象 | `binary` `varbinary` `tinyblob` `blob` `mediumblob` `longblob` | 仅 `IS NULL IS NOT NULL` |
| 其他/未知 | 其余（如 spatial） | 仅 `IS NULL IS NOT NULL` |

值输入规则：

- **统一用文本输入框，不用 `NInputNumber`。** 64 位整数（`bigint`）超出 JS 安全整数范围，数字输入会精度丢失；日期选择器会引入时区/格式二次解释。文本输入 + 后端参数化最稳。
- 数值列：前端用宽松正则校验 `^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`；不合法时行内红字，禁用应用。值仍以字符串参数传递，由 MySQL 按列类型转换。
- 日期时间列：文本输入，占位符 `2026-10-01` 或 `2026-10-01 12:00:00`；不做严格前端校验，MySQL 报错在应用后显示。`TIMESTAMP` 比较受会话时区影响，高级用户需注意。
- 空字符串是合法值：`= ''` 匹配空串，不匹配 NULL。值框为空时显示行内提示「将匹配空字符串」。
- 要匹配 SQL NULL 必须选「为空/不为空」；在值框输入 `NULL` 只会匹配字符串 `"NULL"`。
- `contains` 用 `!` 作为 LIKE 转义字符，值中的 `!` `%` `_` 会转义（见 §4.4）。空值 + contains 会匹配所有非 NULL 行，显示提示。
- 普通字符/文本列：`contains` 大小写由列 collation 决定（`utf8mb4_0900_ai_ci` 通常不区分）；需要区分时用高级 WHERE + `BINARY`。
- JSON 的 contains 按序列化文本匹配，非索引、区分大小写；面板显示警告，复杂 JSON 条件走高级 WHERE。
- `!=` 与 `contains` 遵循 SQL 三值逻辑：不匹配 NULL 行。需要包含 NULL 时另加「为空」条件（AND 下不会成立；应使用高级 WHERE 的 `OR`）。
- 条件值最大 4096 字符；超出时行内错误。
- 同一列可以出现多条条件（如 `created_at >= x` 且 `created_at < y`），用于范围。

#### 3.3.4 已应用条件与 chip

- 条件行下方的 chip 区显示**已应用**条件：`status = paid ×`、`created_at ≥ 2026-10-01 ×`、`email 包含 foo ×`。
- 值超过 24 字符截断显示，tooltip 显示完整值；不截断实际参数。
- 点击 chip 打开面板并聚焦该行；点 `×` **立即移除并重新查询**（走保护）；`[+ 条件]` 打开面板并新增一行。
- 「清除全部」立即清空并查询；保护失败则保留。
- 无已应用条件时 chip 区不渲染。

#### 3.3.5 SQL 预览与「查看已执行 SQL」

两种入口，**文案必须区分**（评审建议，避免“查看 SQL”指两件事）：

1. 筛选面板内的「**预览 SQL**」：预览**当前草稿**，随草稿变化，**不代表已在执行**。
2. 结果元信息旁的「**查看已执行 SQL**」：查看**最后一次实际执行**的 SQL。

面板内的 SQL 预览：

- 只在展开时请求后端 `DBPreviewTableSQL`；草稿变化后 250ms 防抖；旧响应按序号丢弃。
- **预览固定使用 `page = 0`**：应用筛选会把页码重置到第 1 页，预览必须与“应用后立即看到的结果”一致；预览浮层注明「应用后从第 1 页开始」。结果元信息里的「查看已执行 SQL」才使用实际页码。
- 展示完整可执行 SQL：`SELECT * FROM ... WHERE ... ORDER BY ... LIMIT ... OFFSET ...`，值已按 MySQL 字面量规则内联。
- 提供「复制 SQL」「在查询标签打开」。后者新建查询标签并填入 SQL，**不自动执行**；填入的是当前预览的完整 SQL（含当前页的 LIMIT/OFFSET），首行附注释 `-- 当前页 SQL；去掉 LIMIT/OFFSET 可查全部`。
- 标注「预览（值为转义后的展示形式）」。执行路径使用参数化查询；两者谓词与值一致，由同一个 builder 生成（§4.4）。
- 预览请求也会做列/类型校验；不合法时显示同样的行内错误，不发数据查询。
- 高级模式：输入框旁提供「预览 SQL」，调用同一个 `DBPreviewTableSQL`（RawWhere），显示最终 `SELECT ... WHERE <raw> ... LIMIT/OFFSET`，不执行；同样提供复制/在查询标签打开。输入变化后防抖刷新，旧响应丢弃。
- 若 `appliedSource === 'structured'`（刚预填、尚未修改应用），预览浮层标注「当前执行来源：结构化条件；高级文本仅作展示」，避免用户以为已经在按文本执行。修改文本并应用后，来源才切为 raw。

#### 3.3.6 从单元格/列头快捷筛选

**单元格右键：**

```
┌ 单元格右键（email = O'Reilly）──────┐
│ 按此值筛选（= O'Reilly）            │
│ 包含此值（contains）                │
│ 排除此值（!=）                      │
│ ──────────────────────────────────  │
│ 编辑单元格                          │
│ 复制值                              │
└─────────────────────────────────────┘
```

- 「按此值筛选」：追加 `col = value`；「包含此值」：追加 `col contains value`（仅文本）；「排除此值」：追加 `col != value`。
- 追加到现有条件，不覆盖；打开筛选面板并定位到新行，**不自动应用**（用户可确认/修改值）。
- **仅结构化模式可用**（评审阻塞项 2，选方案 a）。处于高级 WHERE 模式时：
  - 单元格右键菜单保留「编辑单元格」「复制值」，但三个快捷筛选项全部**禁用**；菜单顶部显示不可点击的提示行「高级 WHERE 模式下不可用 · 先切回结构化筛选」。
  - 列头右键的「按此列筛选…」同样禁用，tooltip「高级 WHERE 模式下不可用；先返回筛选器」。
  - 不弹确认框、不自动切模式、不把条件拼进 RawWhere（会制造第二真相）。用户要快捷筛选，先走 §3.3.1 的「高级 → 结构化」流程；若高级文本已应用，会按该流程确认并清除 RawWhere。
- **筛选面板已打开且有未应用草稿**：快捷筛选追加到**草稿**（`conditionsDraft`），不丢弃草稿、不改 Applied；面板保持打开并滚动/聚焦到新行。若没有草稿，则从 Applied 条件复制一份作为草稿再追加。
- 已有相同 `列 + 操作符 + 值` 的条件：不重复添加，聚焦已有行（草稿或 Applied 中匹配均可）。
- 已有 5 条：不添加，提示「最多 5 条条件，请先删除一条」，打开面板。
- NULL 单元格：菜单只显示「筛选 NULL（IS NULL）」「筛选非 NULL（IS NOT NULL）」「复制（NULL）」；不显示 `=`/`contains`。
- 数值/日期列：快捷菜单只放 `=`、`!=`；范围操作（`> >= < <=`）通过筛选面板。
- JSON 列：只显示「包含此值」与 NULL 操作。
- BLOB/二进制：只显示 NULL 操作。
- 值超过 4096 字符：禁用快捷筛选，提示「值过长，请在筛选面板手动输入」。
- 有未提交修改的单元格：使用**数据库当前值**（屏幕显示的是待提交值）。菜单项文案直接暴露将使用的值：`按数据库值筛选（= O'Reilly）`、`包含数据库值（…）`，并在菜单底部显示一行「该单元格有未提交修改」。避免用户点完才发现筛的是旧值。
- 右键不触发编辑；只有左键单击才进入编辑（沿用现状）。
- 菜单基于现有 `NDropdown trigger="manual"` 模式（与 FilePanel/TerminalView 一致）。
- 数据标签结果元信息的提示文案同步改为「点击表头排序；单击单元格编辑；右键筛选/复制」；现有 `db.clickSort` 的“点击单元格复制”与实际行为不符，本轮一并修正（查询标签文案不动）。

**高级模式下的右键菜单示意：**

```
┌ 单元格右键（email = O'Reilly）──────────────┐
│ 高级 WHERE 模式下不可用 · 先切回结构化筛选  │  ← 灰字，不可点
│ 按此值筛选（= O'Reilly）          （禁用）   │
│ 包含此值（contains）              （禁用）   │
│ 排除此值（!=）                    （禁用）   │
│ ───────────────────────────────────────────  │
│ 编辑单元格                                   │
│ 复制值                                       │
└──────────────────────────────────────────────┘
```

**列头右键：**

```
┌ 列头右键（email）──────┐
│ 按此列筛选…            │
│ 升序排序               │
│ 降序排序               │
│ ─────────────────────  │
│ 隐藏此列               │
│ 复制列名               │
└────────────────────────┘
```

- 「按此列筛选…」打开筛选面板，新增一行并预选该列；值空，操作符按类型默认。
- 不自动添加条件，不自动应用。
- 高级 WHERE 模式下该项禁用（tooltip「高级 WHERE 模式下不可用；先返回筛选器」）；排序与隐藏列不受影响。

#### 3.3.7 筛选结果的显示

- 应用后页码重置为 0；保留排序。
- 结果元信息：`本页 N 行 · Xms · 查看已执行 SQL`。有筛选时在条件行已可见，不重复。
- 0 行且有筛选：专用空状态「没有符合筛选条件的行」+ `[调整筛选]` `[清除筛选]`；**不要**复用「暂无数据」。
- 0 行且无筛选：`此表暂无数据` + `[刷新]`。
- 分页到空页（page > 0 且 0 行）：显示「本页没有数据」+ `[上一页]` `[回到第一页]`，保留筛选。
- 不自动 COUNT；不显示总页数；「下一页」仅在当前页满页时可用（沿用现有策略）。

#### 3.3.8 待提交编辑保护（PR0，必须先落）

现状：`loadTableData` 在入口无条件清空 `pending` / `editing`；`watch(activeId)` 在切换连接时执行 `tabs.value = []`，**连标签一起清空**。筛选会显著提高重新查询频率，不先修保护就等于放大既有数据丢失。

**总原则：先保护，后改状态。** 所有入口必须在修改 `tab.table / page / pageSize / sortCol / conditions` 或 `activeId` **之前** `await` 保护结果；用户点「取消」时，页码、排序、筛选草稿、表名、连接都保持原样。

**触发清单（全量，实现时用 `grep` 逐一核对调用点）：**

| 类别 | 入口 |
| --- | --- |
| 重新查询 | 应用筛选、移除 chip、清除筛选、排序、翻页、改每页条数、刷新、重试、应用页大小变化 |
| 换表 | 表树点击数据、结构标签「查看数据」、`openTableData` / `openTableStructure` |
| **换连接** | 侧栏连接点击（MySQL→MySQL、MySQL→Redis），即 `activeId` 变化 |
| 关闭标签 | 关闭有 `pending` 的数据标签（`canCloseTab`） |
| 结构变更 | 结构标签对同一表执行 `ALTER` / `DROP` 前（避免改列后 pending 必失败） |
| 列可见性 | **不触发**（不重新查询）；但活动单元格正在编辑时先 commit，再隐藏 |

任何触发操作在 `pending.length > 0` 时先弹：

```
┌ 有未提交的修改 ─────────────────────────────────────┐
│ 重新查询会丢弃 3 处修改（位于 2 行），              │
│ 其中 1 处位于隐藏列。                               │
│ [取消]        [丢弃并继续]        [应用修改并继续]  │
└─────────────────────────────────────────────────────┘
```

**切换连接/切换 Redis（评审新增的数据丢失路径）：**

- 连接点击必须走新的 `requestSwitchConnection(id)`：先收集当前所有数据标签的 pending，再决定是否修改 `activeId`。**不能**等 `watch(activeId)` 触发后再弹窗——那时 `tabs` 已经被清空，旧表、旧行、旧主键都没了。
- 弹窗按标签聚合：

```
┌ 切换连接 ───────────────────────────────────────────┐
│ 切换连接会关闭 3 个标签，其中 2 个有 5 处未提交修改 │
│ （位于 2 张表）。                                   │
│ [取消]        [丢弃并切换]        [全部应用并切换]  │
└─────────────────────────────────────────────────────┘
```

- 「取消」：`activeId` 不修改，标签、页码、筛选全部保留。
- 「丢弃并切换」：清空所有标签后切换。
- 「全部应用并切换」：按标签逐个调用 `DBUpdateRows`（每个标签一个事务）；全部成功后才清空标签并切换。任一标签失败则**中止切换**：已成功标签的 pending 已清空并已落库，失败标签保留 pending，弹窗显示「已应用 X 个标签，Y 个失败」；不假装全部成功。
- 切换到 Redis 连接走完全相同的流程。
- `watch(activeId)` 只作为防御：把清空标签的逻辑移到受保护的 `switchConnection` 之后；若检测到 `activeId` 变化时仍有 pending，记录错误日志并拒绝清空（说明有调用点漏了保护），不能再静默丢。

**实现收口（评审要求，不允许逐处打补丁）：**

- 给 `loadTableData(tab, { resetPage, intent })` 增加显式意图；语义至少区分 **normal / discard-approved / applied-approved**（具体枚举名随实现，现有代码用 `'discard' | 'applied'`）。`pending.length > 0` 且不是 approved 时直接返回 `pending_guard_required`，**不产生任何副作用**。所有调用点都必须显式传意图，并用 `grep loadTableData` 核对。
- 统一 `guardPending(tabs): Promise<'cancel' | 'discard' | 'apply'>`；只有返回非 `cancel` 后，调用方才修改状态并执行原操作。
- 翻页/排序先计算目标值（`targetPage` / `targetSort`），保护通过后再赋值；不允许先 `tab.page++` 再弹窗。
- 换表保护在 `ensureDataTab` 改 `tab.table` 之前执行；捕获旧的 `{connId, database, table, result, pending}`，应用修改时用旧结果生成主键，不能用新表的。
- 「应用修改并继续」调用 `DBUpdateRows` 后**不触发内部 reload**；成功后再执行原操作。失败则保留 pending、终止原操作。
- **部分失败语义**：`DBUpdateRows` 单标签整批事务，失败即回滚，pending 保持全量，用户不会遇到“部分成功 + 重复应用”。跨标签（切换连接）按标签事务，语义见上。
- **应用成功但原查询失败**：提示「修改已应用；查询失败，显示的是上一次结果」并提供刷新，避免用户误以为没生效。
- 弹窗期间冻结触发按钮与编辑；重复点击被忽略，正在应用时按钮显示 loading 且不可再点。
- 弹窗内容包含“位于隐藏列”的提示；工具栏的编辑区在有隐藏列待提交修改时显示 `⚠ N 处位于隐藏列`（tooltip 列出列名）。
- 这是对既有行为的修正，回归验收必须覆盖；PR0 单独可验收，先于筛选上线。

#### 3.3.9 加载、错误与并发

- 查询中：保留上一次结果，网格置灰且**禁止编辑/排序**，结果元信息显示「筛选中…」+ 细进度条；避免在过期行上编辑。
- 成功：替换结果，重置选择/编辑态（pending 已在保护流程处理）。
- 失败：保留上一次结果，顶部显示错误 banner（`--danger`）+ `[重试]` `[编辑筛选]` `[复制错误]`；标注「显示的是上一次结果」。不要把网格清空。
- 快速连续操作：数据标签维护 `loadSeq`，只接受最后一次请求的响应；旧响应丢弃。**所有**状态更新（`result`、`loading`、`error`、`lastTableSQL`）都要先判断 seq，旧响应不能把新请求的 loading 置 false 或清掉新请求的错误。筛选面板的 SQL 预览同样按序号丢弃旧响应。
- 查询进行中禁用 chip 的 `×` 与「应用筛选」，避免快速点击并发打出多条长查询（连接池只有 5 个）；需要中途改条件时先等当前查询结束。
- 查询超时（现有 60s）：显示可读错误 + 建议「缩小条件或增加索引」；本轮不提供取消按钮（见 §8）。
- 错误码为 `unknown_column`（应用外 DDL 导致条件列失效）时：自动刷新一次列元数据、移除失效条件并提示「列定义已变化，已移除 N 条失效条件」，不再让用户反复点应用。自动刷新只做一次，避免循环重试。

### 3.4 空/加载/错误状态汇总

| 状态 | 显示 | 操作 |
| --- | --- | --- |
| 首次加载 | 骨架/「加载中…」 | 无 |
| 表为空 | 「此表暂无数据」 | 刷新 |
| 筛选无命中 | 「没有符合筛选条件的行」 | 调整筛选 / 清除筛选 |
| 空页 | 「本页没有数据」 | 上一页 / 回到第一页 |
| 查询中（有旧结果） | 旧结果置灰 + 「筛选中…」 | 等待；禁止编辑 |
| 查询失败（有旧结果） | 错误 banner + 旧结果 | 重试 / 编辑筛选 / 复制错误 |
| 查询失败（无旧结果） | 错误空状态 | 重试 / 编辑筛选 |
| 列元数据加载失败 | 筛选按钮禁用 + 「无法读取列定义」；**列面板仍可用**（只用 `result.columns`，注释搜索不可用） | 刷新 |

### 3.5 筛选面板的草稿/已应用状态

用三个概念避免混乱：

- **Applied**：网格当前结果对应的谓词（条件数组 / 高级文本 / 排序 / 页码）+ **来源** `appliedSource`。
- **Draft**：面板内或高级输入框中正在编辑的内容。
- **Sealed**：模式切换时，把 Applied 写成另一种表示（结构化 ↔ 高级），不改变谓词、不查询、**不改变 `appliedSource`**。

规则：

- 草稿修改不查库；只有 Apply 把 Draft 提升为 Applied。若 Apply 的是高级文本，同时把 `appliedSource` 置为 `raw`。
- 模式切换只做 Sealed，不提升草稿；未应用的编辑在切换时丢弃或提示（高级 → 结构化有文本差异时弹确认）。
- 刚切到高级模式时，输入框是 Applied 的 Sealed 表示；只有内容偏离预填值才显示 `● 有未应用的修改`，此时「应用」高亮。
- 结构化模式下，面板关闭后草稿不可见；再次打开回填 Applied，用户自然知道 chip 是 Applied。
- 「当前生效的谓词」由 `appliedSource` 决定：`structured` 看条件数组，`raw` 看高级文本。不再用“模式”本身判断，避免预填后两套表示都被当成真相。

### 3.6 快捷键

| 快捷键 | 作用域 | 行为 |
| --- | --- | --- |
| `Ctrl+F` | 数据标签 | 打开筛选面板并聚焦第一个值输入；已打开时聚焦。高级模式下聚焦高级输入框。 |
| `Ctrl+Shift+L` | 数据标签 | 打开列面板并聚焦搜索框；已打开时聚焦。 |
| `Ctrl+Enter` | 筛选面板 / 高级模式 | 应用筛选（等价于「应用筛选」/「应用」）。 |
| `Esc` | 弹层 | 关闭并丢弃草稿；列面板同。 |
| `Enter` | 单元格编辑 | 提交当前单元格（现有行为，不变）。 |
| `Ctrl+Enter` | 查询标签 | 执行查询（现有行为，不变）。 |

- 不使用 `Ctrl+Alt+F`（Windows 上 AltGr 冲突风险）。
- 快捷键仅在数据标签容器内生效，不劫持终端 `Ctrl+F` 搜索。
- 弹层内按钮/复选框保持原生 Tab 顺序；chip 的 × 有 `aria-label`。

### 3.7 视觉规范

- 全部使用 `src/styles/global.css` 变量：`--bg-elevated`（弹层）、`--border-subtle`、`--accent`（主按钮/激活）、`--warning`（高级模式、隐藏列待提交修改、JSON contains 警告）、`--danger`（错误、清除）。
- 条件 chip：高 24px、圆角 12px、11px 字号、`--bg-elevated` 背景；hover 边框 `--accent`。
- 弹层间距用 4/8/12/16；面板内边距 12px；条件行间距 8px。
- 列表面板最大高 360px，内部滚动；不出现页面级横向滚动。
- 不用 emoji 作为功能图标；沿用现有 SVG/文本符号。
- 窄窗口（≤ 960px，与验收统一）：弹层仍锚定，但宽度收窄到 `min(560px, calc(100vw - 32px))`；条件行的列/操作符选择器可换行；不引入底部抽屉。

---

## 4. 数据与接口

### 4.1 前端状态

在 `WorkTab` 的数据标签上新增（仅 `kind === 'data'` 使用）：

```ts
type FilterMode = 'structured' | 'raw'

interface FilterCondition {
  column: string
  op: 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte' | 'contains' | 'is_null' | 'is_not_null'
  value: string
}

interface DataTabState {
  // Applied：当前网格结果对应的谓词
  filterMode: FilterMode
  appliedSource: 'structured' | 'raw'   // 实际执行来源；切换模式不改变它
  conditions: FilterCondition[]
  rawWhere: string
  lastTableSQL: string          // 最后一次执行的 SQL（可读形式）
  // Draft
  conditionsDraft: FilterCondition[]
  rawWhereDraft: string
  filterPanelOpen: boolean
  columnsPanelOpen: boolean
  // Mode switch
  rawPrefill: string            // 进入高级模式时由后端返回的 WHERE
  conditionsSnapshot: FilterCondition[] | null
  filterDirty: boolean          // 高级文本是否改过但未应用
  // Metadata
  columnsStale: boolean         // 结构标签 DDL 后置 true
  metadataError: string         // 列元数据加载失败；只禁用筛选操作符
  // Column visibility
  hiddenColumns: string[]       // 当前表的有效隐藏集合（与 result.columns 求交）
  // Loading
  loadSeq: number
  filterError: string           // 最近一次查询错误的 code: message
}
```

实现建议：

- 不要让 `conditionsDraft` 与 `conditions` 共享数组引用；用结构化克隆。
- `hiddenColumns` 的真相在 settings 表（`grid.columns.<connId>.<db>.<table>`）；tab 内是缓存，切换表时重读。
- 行/列索引：见 §3.2.6；隐藏列不改变 `result.rows` 与原始列索引。
- **切表重置清单**（按评审补充，漏一项就会串表）：`conditions / conditionsDraft / rawWhere / rawWhereDraft / rawPrefill / conditionsSnapshot / filterDirty / filterPanelOpen / columnsPanelOpen / lastTableSQL / filterError / columnsStale / metadataError / hiddenColumns`；`pending / editing / result / page / sortCol` 由加载流程处理；`filterMode` 按 §3.3.1 在会话内保留。
- pending 编辑项在 UI 层沿用既有「大写 `NULL` 表示置空」的输入约定；构造 `RowUpdate` 时转成 `value: string | null`（null = SQL NULL），**哨兵不进入绑定参数**。字面量大写 `"NULL"` 本轮仍无法与置空区分（既有约定，不在本轮改）；需要写该字符串时用查询标签。该转换随 PR0 一起做。

### 4.2 本地持久化（settings 表）

只持久化列可见性：

```
key   = `grid.columns.${connId}.${database}.${table}`
value = { "v": 1, "hidden": string[] }
```

- 绑定：`GetGridPref(key)` / `SetGridPref(key, value)` / `DeleteGridPrefsByPrefix(prefix)`；
  底层是既有 `settings` 表（非敏感 UI 状态）。
- 读取失败（JSON 解析、版本不符、设置读取失败）→ 返回空数组，不抛错。
- 写入失败 → 静默忽略（本次会话仍生效）。
- 删除连接时由 `DeleteDBConn` 按 `grid.columns.<connId>.` 前缀清理；不在数据库端存任何偏好。
- 应用内表改名/删除时清理旧 key；孤儿 key 不影响功能，但会积累。
- 集中读写：所有 key 编码、版本检查、求交、损坏修复都放在 `gridPrefs.ts`，`DBView.vue` 不直接拼 key 或调用存储绑定。
- 不持久化：筛选条件、筛选模式、高级 WHERE、页码、排序、页大小（页大小仍按现有行为在本次会话内保留）。

### 4.3 Go 类型与绑定

新增 `internal/db/table_query.go`（不新增包，依赖方向不变）：

```go
type FilterOp string

const (
    OpEq        FilterOp = "eq"
    OpNe        FilterOp = "ne"
    OpGt        FilterOp = "gt"
    OpGte       FilterOp = "gte"
    OpLt        FilterOp = "lt"
    OpLte       FilterOp = "lte"
    OpContains  FilterOp = "contains"
    OpIsNull    FilterOp = "is_null"
    OpIsNotNull FilterOp = "is_not_null"
)

type FilterCondition struct {
    Column string   `json:"column"`
    Op     FilterOp `json:"op"`
    Value  string   `json:"value"`
}

type TableQuery struct {
    Database   string            `json:"database"`
    Table      string            `json:"table"`
    Conditions []FilterCondition `json:"conditions"`
    RawWhere   string            `json:"rawWhere"`
    SortColumn string            `json:"sortColumn"`
    SortDesc   bool              `json:"sortDesc"`
    Page       int               `json:"page"`
    PageSize   int               `json:"pageSize"`
}

type TableQueryPreview struct {
    SQL        string `json:"sql"`        // 可执行的可读 SQL，值已内联
    Where      string `json:"where"`      // 不含 WHERE 关键字的可读谓词，用于模式切换预填
    ParamCount int    `json:"paramCount"` // 测试/调试用；含 LIMIT/OFFSET 占位符
}

type TableDataResponse struct {
    Result      *Result      `json:"result"`
    Columns     []ColumnInfo `json:"columns"`     // 与本次校验同一份元数据，前端不再单独调用 DBDescribeTable
    ExecutedSQL string       `json:"executedSql"` // 与预览同一 builder 生成的可读 SQL
}
```

绑定方法（`internal/app`）：

```go
func (s *Service) DBTableData(connID string, q db.TableQuery) (*db.TableDataResponse, error)
func (s *Service) DBPreviewTableSQL(connID string, q db.TableQuery) (*db.TableQueryPreview, error)

// 参数化写入；在 internal/db 已有 RowUpdate / Manager.UpdateRows 的基础上暴露绑定
func (s *Service) DBUpdateRows(connID, database, table string, updates []db.RowUpdate) (int64, error)
```

- 前端不再拼表数据的 WHERE；`buildTableSQL` 删除。
- **前端也不再拼编辑 UPDATE**；`DBQuery` 保留给查询标签与 DDL，单元格写入只走 `DBUpdateRows`（§4.5）。
- 新增绑定后重新生成的 Wails 绑定必须跑 `scripts/dedupe_bindings.py`，否则 Rollup 重复声明。
- 不新增事件；`loadSeq` 在前端做。

### 4.4 SQL 生成与校验

单一 builder，同时产出执行 SQL、参数、可读 SQL：

```go
type builtQuery struct {
    SQL     string   // 占位符版本，用于执行
    Args    []any
    Preview string   // 值内联版本，用于显示/复制
    Where   string   // 不含 WHERE，用于模式切换
}
```

校验顺序（全部在 `internal/db`，不信任前端）。核心校验必须抽成 **`validateAndBuild(cols []ColumnInfo, q TableQuery)` 纯函数**，测试注入假列定义，不依赖真实 MySQL；网络/元数据获取在外层。

1. **表校验**：`SHOW FULL COLUMNS FROM database.table` 成功；得到 `name → ColumnInfo`（含 `Comment`，§4.6）。
2. **模式互斥**：前端按 `appliedSource` 只填一边——`structured` 时传 `Conditions`、`RawWhere` 留空；`raw` 时传 `RawWhere`、`Conditions` 留空。`len(Conditions) > 0 && RawWhere != ""` → code `filter_mode_conflict`。
3. **条件数**：≤ 5 → code `too_many_conditions`。
4. **列校验**：条件列、排序列必须存在于当前列定义；反引号转义用现有 `quoteIdent`；未知列 → code `unknown_column`。
5. **操作符白名单 + 类型白名单**：按 §3.3.3 的矩阵；不合法 → code `operator_not_allowed`。
6. **值长度**：条件值 ≤ 4096 runes → `value_too_long`；RawWhere ≤ 4000 runes → `raw_where_too_long`。
7. **RawWhere 校验（必须用词法扫描，不能用 `strings.Contains`）**：
   - 复用 `internal/db/mysql.go` 的 `scanTopKeywords`（已能跳过字符串、反引号标识符、注释、括号内内容）。新增/扩展一个扫描器返回顶层 token 与注释位置，供本校验使用。
   - 拒绝**顶层** `;`；字符串字面量里的 `;` 不算。
   - 拒绝任何注释：`--`、`#`、`/* ... */`；**特别拒绝 MySQL 版本注释 `/*! ... */`**（`/*!50000 ... */` 内的 SQL 会执行，不能当注释放过）。
   - 拒绝顶层 `ORDER BY`、`GROUP BY`、`HAVING`、`LIMIT`、`OFFSET`、`UNION`（按 token 相邻关系判定，如 `ORDER` + `BY`）。这些子句必须由系统追加，否则会与分页/排序拼出双重子句或改变结果集结构。子查询/括号内的这些关键字允许（深度 > 0）。
   - 列名恰好叫 `order` / `group` 时不能误杀：只有 `ORDER` 紧跟 `BY`、`GROUP` 紧跟 `BY` 才算子句（`order = 1` 通过）。
   - **前导 `WHERE`**：用户很可能输入 `WHERE status = 1`（现有占位符写的是「筛选条件（WHERE 片段）」）。系统在词法层去掉**恰好一个**最前面的 `WHERE` 关键字并记录一次提示「已自动去掉开头的 WHERE」；其余位置的 `WHERE` 不特殊处理。避免生成 `WHERE WHERE ...`。
   - 拒绝 NUL/控制字符（保留换行、Tab）。
   - 允许 `OR`、`AND`、`NOT`、括号、子查询、函数。
   - 明确：这不是安全沙箱；用户拥有数据库凭据，高级模式就是直接拼 SQL。拦截分号/注释/子句是为了**防止误操作破坏系统追加的 ORDER BY / LIMIT**，不是为了防注入。
8. **分页**：`Page >= 0`、`PageSize ∈ {100,500,1000,5000}`；先算 `offset = int64(Page) * int64(PageSize)`，再要求 `offset <= 1_000_000_000`（不再用 `Page <= 1_000_000` 这个与 offset 上限不一致的规则）；越界 → code `invalid_page`，前端重置到第 1 页。
9. **排序**：列已校验；方向只允许 ASC/DESC；非法 → code `invalid_sort`。

**元数据获取策略**：本轮每次 `DBTableData` / `DBPreviewTableSQL` 做一次 `SHOW FULL COLUMNS`（单表通常 < 5ms），**不引入跨请求缓存**，避免缓存失效与外部 DDL 的一致性问题；代价是分页/排序多一次轻量往返。若真实大表/远连接上延迟明显，再评估 60s TTL 缓存（不作为本轮验收）。

生成：

```
SELECT * FROM `db`.`table`
[WHERE <condition 1> AND <condition 2> ...]
[ORDER BY `col` ASC|DESC]
LIMIT ? OFFSET ?
```

- `SELECT *` 是有意的：隐藏列仍会被读取，以保证排序、筛选、隐藏列上的待提交编辑、以及切回可见时的稳定（CSV 单独按可见列导出）。列投影下推见 §8。
- 值全部用 `?` 占位符；参数顺序 = 条件值 → LIMIT → OFFSET。
- 参数化执行路径：现有 `Manager.Query` 只接受 SQL 字符串；本轮在 `db` 内新增 `queryArgs(ctx, sql, args, limit)`，用 `QueryContext(ctx, sql, args...)` 执行，并复用现有 `Result` 扫描/`formatValue`/`Truncated` 逻辑（建议抽出 `scanRows` 供 `Query` 与 `TableData` 共用）。`TableData` 不走 `isQuery` 分类，它始终是一条 SELECT。
- `contains`：
  - SQL：`` `col` LIKE ? ESCAPE '!' ``
  - 参数：`"%" + escapeLike(value) + "%"`
  - `escapeLike`：`!` → `!!`、`%` → `!%`、`_` → `!_`。
  - 选择 `!` 而不是反斜杠：避免 `NO_BACKSLASH_ESCAPES` 与双重转义错误；`!` 本身也可转义。
  - 示例：值 `50%_off!` → 参数 `%50!%!_off!!%`。
- `IS NULL` / `IS NOT NULL` 不产生参数。
- 可读 SQL（Preview）：
  - 字符串字面量用单引号，内部单引号双写；反斜杠、换行、Tab、回车按 MySQL 常规转义；NUL 在校验阶段拒绝。预览假定会话未启用 `NO_BACKSLASH_ESCAPES`；执行始终走参数，不受该设置影响。
  - 数值/日期值也以带引号字符串显示，与参数化执行的隐式转换一致；用户复制到查询标签可执行。
  - `LIMIT/OFFSET` 直接内联数字。
  - 预览与执行共用同一条件遍历逻辑，只是值的呈现方式不同；单元测试断言两者在相同输入下的谓词一致。

**错误约定（评审阻塞项 3）：跨 Wails 边界只传 `code: message` 字符串。**

Wails 会把 Go `error` 序列化成 `Error()` 字符串，`errors.Is` 的身份到不了前端。沿用项目既有约定（`internal/app/ai.go` 的 `aiError` / `aiErrorCode`，前端 `AIPanel.vue` 用 `/^([a-z_]+):\s*(.*)$/s` 解析）：

```go
// internal/db/table_query.go
type QueryError struct {
    Code    string // 稳定错误码，前端按它映射 i18n
    Message string // 兜底展示；可含服务端原文
}

func (e *QueryError) Error() string { return e.Code + ": " + e.Message }

func queryErrf(code, format string, args ...any) error {
    return &QueryError{Code: code, Message: fmt.Sprintf(format, args...)}
}
```

- Go 侧测试用 `errors.As` 断言 `*QueryError.Code`；**前端绝不按英文消息字符串匹配**。
- `DBUpdateRows` 的写入错误同样用这个约定：参数/约束错误是稳定 code；MySQL 执行错误统一包装为 `apply_edits_failed: <server message>`，`message` 只做展示。
- 日志只记录 `code`、模式、条件数、耗时、表名；**不记录值、RawWhere、结果行**；MySQL 原文经 `redactSQLError` 后再写日志（沿用 `DBQuery` 先例）。

稳定错误码表（前端 i18n key = `db.err.<code>`）：

| code | 触发 | 前端默认文案（zh-CN） |
| --- | --- | --- |
| `filter_mode_conflict` | Conditions 与 RawWhere 同时非空 | 筛选模式冲突，请只保留一种筛选方式。 |
| `too_many_conditions` | 条件 > 5 | 最多 5 条条件；更多请用高级 WHERE。 |
| `unknown_column` | 条件/排序列不在列定义中 | 列 {name} 不存在，可能已被删除或重命名；已尝试刷新列定义。 |
| `operator_not_allowed` | 操作符与列类型不匹配 | 该列不支持操作符 {op}。 |
| `unsafe_raw_where` | 顶层分号/注释/子句/版本注释/控制字符 | 高级 WHERE 不能包含 ;、注释（--、#、/* */、/*! */）或 ORDER BY / GROUP BY / HAVING / LIMIT / OFFSET / UNION 子句。 |
| `raw_where_too_long` | RawWhere > 4000 runes | 高级 WHERE 过长（上限 4000 字符）。 |
| `value_too_long` | 条件值 > 4096 runes | 条件值过长（上限 4096 字符）。 |
| `invalid_page` | 页码/偏移量越界 | 分页参数无效，已重置到第一页。 |
| `invalid_sort` | 排序方向非法 | 排序参数无效。 |
| `table_metadata_unavailable` | 元数据读取失败 | 无法读取列定义，请刷新或检查权限。 |
| `too_many_edits` | 单批 > 1000 行 | 一次最多应用 1000 行修改。 |
| `apply_edits_failed` | 写入事务失败 | 应用修改失败：{message}（已回滚，未写入任何行）。 |
| `pending_guard_required` | 编程错误：未走保护就调用加载 | 有未提交修改，加载被拒绝。（不应出现在 UI） |

- `unknown_column` 的 `message` 可带列名；前端优先用结构化参数展示，取不到就显示原文。
- 其余 MySQL 查询错误不带稳定 code，前端显示服务端消息 + 「编辑筛选」入口。

### 4.5 参数化写入（DBUpdateRows）

> 评审草稿里曾称 `DBApplyEdits`；以实际绑定名 `DBUpdateRows` 为准（`internal/app/service.go`）。

现状 `applyEdits` 在 `DBView.vue` 用 `sqlLiteral` 拼 UPDATE：只双写单引号，不处理反斜杠；每条 UPDATE 单独 `DBQuery`，无事务。反斜杠/引号/NUL/emoji 都可能写坏数据，部分失败无法解释。本轮保护流程会把这条路径变成主路径，必须先改。

**接口**（与 `internal/db` 已落地的 `RowUpdate` / `Manager.UpdateRows` 对齐）：

```go
type RowUpdate struct {
    PK  map[string]*string `json:"pk"`
    Set map[string]*string `json:"set"`
}

func (s *Service) DBUpdateRows(connID, database, table string, updates []db.RowUpdate) (int64, error)
```

- Go 侧 `RowUpdate` 的值是 `*string`：`nil` = SQL NULL，空字符串 = `''`。前端沿用「大写 `NULL` 表示置空」的输入约定，构造请求时转成 `null`；**哨兵不跨到 Go**，Go 侧不解析 `"NULL"`。小写 `null` 按字面量写入；字面量大写 `"NULL"` 与置空仍不可区分（既有约定，本轮不改）。
- 前端按当前结果（使用原始列索引，隐藏列不改变索引）构造每个待提交行的 `PK`（表的主键列）与 `Set`（该行所有改动列）；复合主键全部带上。
- Go 侧：库/表/列名走 `quoteIdent`；所有值（**包括主键值**）走 `?`；`SET`/`WHERE` 的列名排序保证 SQL 稳定、可断言；主键值为 nil 用 `IS NULL`（防御性，MySQL 主键实际非空）。
- **后端校验，不信任前端**：`Set`/`PK` 列必须存在于 `SHOW FULL COLUMNS`；`PK` 的键集合必须等于表的全部 `Key == "PRI"` 列（防止漏主键导致更新多行）；空 `Set`/空 `PK` 拒绝；单批 ≤ 1000 行。不符合 → `apply_edits_failed`，不执行任何语句。
- 整批在一个事务内执行（`BeginTx`），任一语句失败即 `Rollback` 并返回 `apply_edits_failed`；**不存在“部分行已写入”**，pending 保持全量，不会让用户重复应用。
- 单批上限 1000 行（`maxRowUpdates`），超出返回 `too_many_edits`。`Manager.UpdateRows` 目前返回普通 error；`Service.DBUpdateRows` 必须把它包装成 `code: message`（事务/执行错误 → `apply_edits_failed`，空 Set/空 PK → `apply_edits_failed` 并带原因），不能把裸 error 直接透给前端。
- 不加 `LIMIT 1`：主键/复合主键已唯一定位；`LIMIT` 在部分复制/配置场景有额外限制。
- **不做 RowsAffected 冲突检测**（本轮取舍）：若主键行已被外部删除，UPDATE 影响 0 行但事务成功，界面会清空 pending，刷新后行消失。理由：同值更新在默认 `CLIENT_FOUND_ROWS=false` 下也返回 0 行，贸然判冲突会误报；引入 `clientFoundRows` 会改变全局 DML 的 affected 语义。用户刷新即可看到真实状态；未来需要乐观锁时另加版本列或“0 行时复查存在性”。
- 日志：记录 applied 数量、表名、错误码；不记录 PK/Set 值；MySQL 原文经 `redactSQLError`。
- 测试：反斜杠（`C:\new`、`C:\\`）、单引号、双引号、NUL、emoji、复合主键、bigint 主键（>2^53）、空串 vs NULL、第二条 UPDATE 失败整批回滚。

### 4.6 列元数据

**数据源修正（评审阻塞项 1）：`DescribeTable` 改为 `SHOW FULL COLUMNS`。**

现状 `DescribeTable` 只执行 `DESCRIBE`；`DESCRIBE` 不返回 `Comment` 列，所以 `get("Comment")` 永远是空串。§3.2.2 的注释搜索/ tooltip 和原验收 7.2 都不成立。

选择 `SHOW FULL COLUMNS FROM db.table` 而不是查 `information_schema.COLUMNS`：

- 它是 `DESCRIBE` 的直接替代，返回顺序与表定义一致，不需要额外 `WHERE TABLE_SCHEMA/TABLE_NAME`，也没有 `information_schema` 的库名大小写/排序规则细节。
- 权限要求与 `DESCRIBE` 相同，不额外扩大权限面。
- 一次返回 `Field / Type / Collation / Null / Key / Default / Extra / Privileges / Comment`，现有 `ColumnInfo` 字段全部能映射，不需要新绑定。
- 视图同样适用。

实现要点：

- 行解析抽成纯函数 `parseColumnRows(cols []string, rows [][]any) []ColumnInfo`，用假数据单测。
- **`Default` 必须区分 SQL NULL 与空字符串**：`SHOW FULL COLUMNS` 下无默认值是 SQL NULL，默认值是 `''` 时是空串。现有 `Null == "YES" && d == ""` 的启发式会误判；解析时保留原始 `*string`（nil → `Default = nil`，"" → `Default = ptr("")`）。
- `Comment` 映射到 `ColumnInfo.Comment`；列注释为空时，列面板搜索只匹配列名。
- 元数据读取失败（权限/表被删）返回 `table_metadata_unavailable`；此时筛选面板禁用（没有类型/操作符矩阵），但列面板仍用 `result.columns` 的列名工作。
- 数据标签加载时使用 `DBTableData` 响应里的 `Columns`（与后端校验同一份 `SHOW FULL COLUMNS` 结果），缓存到 tab（列名、类型、注释、PK）；**不再额外调用一次 `DBDescribeTable`**，避免一次冗余往返。`DBDescribeTable` 仍供结构标签与其他调用使用。
- 用于：筛选操作符、值类型、PK、列面板类型/注释、排序校验（后端另做）。
- 表切换：清空缓存，重新加载。
- 结构标签执行 DDL 后：把同一表的数据标签标记 `columnsStale = true`；下次数据标签加载/激活时重新读取，并：
  - 移除引用不存在列或不再支持操作符的条件（toast「已移除 N 条失效条件」）。
  - 清除不存在的排序列。
  - 隐藏偏好按新列名求交；新列默认可见。
  - 若同表数据标签存在 pending，先走结构变更保护（§3.3.8），不要让用户应用必失败的 UPDATE。
- 数据标签与结构标签不共享可变数组；各自持有副本。

### 4.7 日志与隐私

- 新路径禁止打印筛选值、原始 WHERE、结果行、PK/Set 值；只记录 `table`、条件数、模式、耗时、错误码、applied 数量（沿用 `redactSQLError` 处理数据库错误）。
- 筛选条件与列偏好只存本机；**v0.3.2 不新增自动 AI 发送路径**，也不进入遥测。用户主动把含值 SQL 从「查看已执行 SQL」带到查询标签、再发送给 AI 时，值会随 SQL 进入 v0.3.1 的 payload；这是用户主动行为，受发送前预览约束。
- 高级 WHERE 是用户输入，可能包含敏感值；错误提示可回显，日志不回显。
- 新增错误码是稳定接口；日志只写 code，不写可能随文案变化的 message。

### 4.8 组件与实现顺序

新增：

- `frontend/src/components/GridFilterPanel.vue`：条件行、操作符/值、SQL 预览、模式切换、应用。
- `frontend/src/components/GridColumnsPanel.vue`：搜索、勾选、全选/清空、应用。
- `frontend/src/components/GridFilterChips.vue`：已应用条件 chip、清除全部、+ 条件。
- `frontend/src/stores/gridPrefs.ts`：settings 偏好的 key 编码/解析/求交/修复（纯函数，便于测试）。
- `frontend/src/stores/gridEdits.ts`（可选）：把 pending 编辑 → `RowUpdate` 的转换抽成纯函数，便于测试隐藏列/复合主键/NULL。

修改：

- `frontend/src/components/DBView.vue`：数据标签模板、`loadTableData` 换 `DBTableData`、待提交编辑保护（单一收口 + 连接切换）、右键菜单、快捷键、CSV 文案。
- `internal/db/mysql.go`（已落地 `RowUpdate` / `UpdateRows`）+ 新增 `internal/db/table_query.go`：builder + 校验 + 错误码；`DescribeTable` 改 `SHOW FULL COLUMNS` 与纯函数解析。
- `internal/app/service.go`：三个绑定方法（`DBTableData`、`DBPreviewTableSQL`、`DBUpdateRows`），错误码包装与日志脱敏。
- `frontend/src/i18n/zh-CN.ts` / `en-US.ts`：`db.filter.*`、`db.columns.*`、`db.cell.*`、`db.pendingGuard.*`、`db.err.*`、`db.gridHint`（数据标签专用提示，不动查询标签的 `db.clickSort`）。
- `ARCHITECTURE.md`：新文件、三个绑定方法、settings 偏好、无新事件；`ROADMAP.md` 在落地时勾选。
- `frontend/package.json`：加入最小 `vitest` 配置（至少跑 `gridPrefs`、数字正则、pending→RowUpdate 转换）。评审指出“纯函数便于测试”在没有测试框架时是空话；不引入组件级测试。

**PR 拆分（每个可独立验收；保护先行）：**

1. **PR0（数据安全，必须先合入）**：待提交编辑保护的单一收口与全部触发入口（含 `watch(activeId)` 连接切换、切换 Redis、关标签、结构标签 DDL）；`applyEdits` 改 `DBUpdateRows` 参数化 + 事务；pending 到 `RowUpdate` 的 NULL 边界转换（沿用大写 `NULL` 置空输入）；最小 vitest。**这一步先于任何筛选 UI 上线。**
2. PR1：Go builder + `validateAndBuild` 纯函数 + 单元测试 + `DBTableData`/`DBPreviewTableSQL` 绑定；`DescribeTable` 改 `SHOW FULL COLUMNS`。
3. PR2：列可见性 UI + 持久化 + 列头隐藏 + CSV 文案。
4. PR3：结构化筛选 UI + chip + SQL 预览 + 高级 WHERE 模式与词法校验。
5. PR4：快捷筛选 + 模式化禁用/草稿追加 + i18n + 全量回归。

预估：**2 周**（评审修正：1–1.5 周不可信）。其中 PR0 约 3–4 天；PR1–PR4 约 6–7 个工作日；剩余为回归与手工验收。若时间紧，先砍「在查询标签打开」或列面板的搜索/全选，**不要砍 PR0**。

---

## 5. 与 v0.3.1 AI 助手的协作

### 5.1 结论

**v0.3.2 不在数据标签增加任何 AI 入口，也不做“AI ↔ 筛选器”转换。**

只做一个确定性的桥接：从「查看已执行 SQL」把当前筛选 SQL 送到查询标签，用户可在那里继续用 AI 解释/优化，或手动改成 JOIN/聚合。

### 5.2 为什么不做

1. **价值不成立**：结构化条件行已经把“写 WHERE”的时间降到 10–20 秒；AI 生成估计也要 10–30 秒 + 人工复核。对熟悉列名的用户，AI 不快；对不熟悉列名的用户，列选择器（带类型/注释/搜索）已经解决了“记不住列名”。
2. **隐私边界会被模糊**：数据标签是日常最常驻的行数据界面（查询标签的结果是一次性的）。即使 `internal/ai` 在类型层面只接受 schema，用户在数据标签看到 AI 按钮时，会合理怀疑“它是不是能看到这些行”。不在数据标签放 AI，是最清晰的边界。
3. **失败回退没有收益**：没有 AI，手动路径本来就是唯一路径且足够快；引入 AI 只增加一个可能失败、可能产生错误条件的入口。
4. **范围控制**：v0.3.1 刚落地，AI 基础设施应先在查询标签稳定；数据标签增强本身已经是完整增量。
5. **不做 AI SQL ↔ 筛选器双向转换**：
   - AI 生成 SQL → 结构化条件：需要解析 `WHERE` 的 OR、函数、优先级、引号/注释；解析失败或语义偏差的风险高，用户难以发现。AI 生成的 SQL 留在查询标签，人审后执行即可。
   - 结构化条件 → AI 优化：AI 看不到数据分布，且发送条件值会扩大隐私面；优化空间有限。用户需要时可把「查看已执行 SQL」送到查询标签，再用 AI 解释/优化。
   - 结论：两条路径各自独立；数据标签负责“筛”，查询标签负责“写/解释/优化”。
6. **不新增自动发送路径，但桥接不是零隐私影响**：从「查看已执行 SQL」带到查询标签的是含内联值的完整 SQL；用户之后若主动点 explain/fix，值会随 SQL 进入 v0.3.1 的 payload。这是用户主动行为，且发送前有预览；文档和验收必须这样写，不能说“筛选值绝不进入 AI payload”。

### 5.3 AI 创新三问（如果未来做）

| 问题 | 回答 |
| --- | --- |
| 比手动快多少？ | 目前没有证据。三个条件用手动条件行约 10–20 秒；AI 生成 10–30 秒 + 复核。只有在“用户不知道该筛哪些列/值”时可能有价值，但结构化面板的列搜索/注释已经覆盖大半。没有 2 倍收益前不做。 |
| 隐私边界？ | 如果未来做，payload 只能是 schema + 数据库名 + 列名/操作符；**默认不包含用户输入的值**，值需用户显式勾选才发送；**绝不包含行数据、结果集、当前页值、列值分布**。默认只允许本机 provider；云端每次发送前预览。类型层面禁止 `db.Result` 进入请求。 |
| 失败了能否退回手动？ | 必须能。AI 只能产出“待审阅的条件行”，不得自动应用；用户可逐条删除/修改；AI 不可用时条件行照常手动编辑。 |

### 5.4 确定性的桥接（本轮做）

- 「查看已执行 SQL」浮层提供「在查询标签打开」：新开查询标签，填入完整 SELECT（含当前页 LIMIT/OFFSET，首行注释提示可去掉分页查全部），**不自动执行**。
- 价值：筛选后要做 JOIN/聚合/导出全量时，不用手抄 WHERE；也方便把筛选交给查询标签的 AI 助手解释或优化（用户自己决定是否发送）。
- 不涉及 AI 请求，不增加隐私面。

### 5.5 未来候选与门槛

只有满足以下全部条件，才考虑在 v0.3.3+ 做 AI 筛选：

1. 用户研究或固定任务测试证明比手动快 2 倍以上。
2. payload 类型层面只含 schema + 条件，不含行数据；有快照测试。
3. 默认本机 provider；云端逐次预览。
4. 输出只能是条件行，永不自动应用。
5. 有明确的“为什么这样筛”的解释和 SQL 预览。

候选场景（优先级低）：「解释为什么当前筛选返回 0 行」——需要发送条件 + 列类型，可能还要 COUNT；与“不自动 COUNT”冲突，暂不做。

---

## 6. 边界情况

| # | 场景 | 处理 |
| --- | --- | --- |
| 1 | 空字符串 vs NULL | `= ''` 只匹配空串；NULL 必须用「为空」。值框空时提示。 |
| 2 | `!=` / `contains` 与 NULL | 遵循 SQL 三值逻辑，NULL 行不匹配；需要包含 NULL 用高级 WHERE 的 OR。 |
| 3 | 值含 `'` `"` `\` 中文 emoji | 参数化执行；预览按字面量转义；不报错。 |
| 4 | `contains` 值含 `%` `_` `!` | 用 `!` 转义；`a%b` 只匹配字面 `a%b`，不匹配 `axb`。 |
| 5 | 数值列输入非数字 | 前端行内校验拦截；不发送查询。 |
| 6 | 64 位 bigint | 文本输入，不经过 JS Number；不丢精度。 |
| 7 | 日期格式非法/时区 | 文本直传，MySQL 报错可读；TIMESTAMP 受会话时区影响，文档提示。 |
| 8 | BLOB/二进制列 | 可隐藏；只能筛 NULL/非 NULL；CSV 可能显示乱码，建议隐藏。 |
| 9 | JSON 列 | 可 contains（序列化文本、非索引、区分大小写）+ 警告；复杂条件走高级 WHERE。 |
| 10 | enum/set | `=`/`!=`/NULL；set 的包含语义用高级 `FIND_IN_SET`。 |
| 11 | 列被删除/重命名 | 隐藏偏好忽略未知名；条件/排序在元数据刷新后移除并提示；不崩。 |
| 12 | 持久化隐藏了全部列 | 自动显示第一列并回写修复。 |
| 13 | 隐藏列上有待提交编辑 | 允许；工具栏 `⚠ N 处位于隐藏列`；批量应用正常执行。 |
| 14 | 排序列为隐藏列 | 保留排序；显示 `排序: col ▼（已隐藏）×`，可切换方向/清除。 |
| 15 | 隐藏列参与筛选 | 允许；筛选面板的列选择器显示「隐藏」标记。 |
| 16 | 切表、**切换连接、切换 Redis、关闭标签**时有待提交编辑 | 弹保护框；连接切换按标签聚合；取消时 `activeId` 与标签都不变。 |
| 17 | 应用筛选/排序/翻页/刷新时有待提交编辑 | 同上；先保护、后改状态；杜绝静默丢弃。 |
| 18 | 筛选 0 行 vs 表为空 | 两个不同空状态；筛选 0 行提供调整/清除。 |
| 19 | 页码超出范围 | 筛选/排序/页大小变化重置到第 0 页；空页提供回第一页。 |
| 20 | 大表筛选慢 | 不 COUNT；保留旧结果 + 加载态；60s 超时错误；不自动重试。 |
| 21 | 无 SELECT 权限 | 显示服务端错误；筛选面板可编辑但不应用；不清空旧结果。 |
| 22 | 连接断开 | 错误 banner；保留旧结果与条件；重试。 |
| 23 | 视图（VIEW） | 筛选/列隐藏/排序可用；无主键不能编辑；列元数据来自 `SHOW FULL COLUMNS`。 |
| 24 | Redis 连接 | 不显示筛选/列控件；沿用现有 Redis 视图。 |
| 25 | 高级 WHERE 含 `;` / 注释 | 行内错误，拒绝应用；提示原因。 |
| 26 | 高级 WHERE 含顶层 ORDER BY / GROUP BY / HAVING / LIMIT / OFFSET / UNION | 校验拒绝并给出行内错误；子查询内部的这些关键字允许。 |
| 27 | 高级 → 结构化，文本被修改 | 未应用：放弃草稿并返回，不查库；已应用：弹确认，清除并返回需走保护；不尝试解析。 |
| 28 | 超过 5 条条件 | 添加按钮禁用；快捷筛选提示先删除；不自动覆盖。 |
| 29 | 重复条件 | 允许（范围查询需要）；快捷筛选相同条件定位已有行，不重复添加。 |
| 30 | 快捷筛选有未提交修改的单元格 | 使用数据库当前值 + 警告；不用未落库值。 |
| 31 | 快捷筛选 NULL | 只提供 IS NULL / IS NOT NULL。 |
| 32 | 快捷筛选超长值（>4096） | 禁用并提示手动输入。 |
| 33 | 列面板搜索无结果 | 「没有匹配的列」；全选/清空无操作。 |
| 34 | settings 值损坏/不可用 | 当作无隐藏；不报错；本次会话仍可改。 |
| 35 | 多连接同表 | 列偏好按 connId 隔离。 |
| 36 | 结构标签 DDL 改列 | 执行前若同表有 pending 先走保护；标记元数据过期；下次加载移除失效条件并提示。 |
| 37 | 查询进行中继续改条件 | 草稿可改；应用按钮与 chip × 禁用（单飞）；旧响应不能改 loading/error。 |
| 38 | 查询错误后旧结果展示 | 标注“显示的是上一次结果”；允许重试/编辑筛选；不假装是新结果。 |
| 39 | CSV 与待提交编辑 | CSV 导出数据库中的当前页值，不含未提交修改；tooltip 写明。 |
| 40 | CSV 与隐藏列 | 只导出可见列；是当前页，不是全表；按钮写「CSV（可见列）」，导出后 toast「已导出 N 列，隐藏 M 列」。 |
| 41 | 窄窗口 | 弹层宽度自适应；条件行可换行；不横向溢出。 |
| 42 | 语言切换 | 所有新增文案走 i18n；不硬编码中文。 |
| 43 | 表没有主键 | 不能编辑（沿用现有提示）；筛选、列隐藏、快捷筛选、排序正常。 |
| 44 | 高级 WHERE 模式下右键快捷筛选 | 禁用并提示「先切回结构化筛选」；不弹确认、不修改 RawWhere；编辑/复制仍可用。 |
| 45 | 筛选面板有未应用草稿时快捷筛选 | 追加到草稿并聚焦新行；不丢弃草稿、不修改 Applied。 |
| 46 | 列注释为空/元数据失败 | 注释搜索退化为列名搜索；元数据失败时列面板仍可用，筛选面板禁用。 |
| 47 | 大写 `NULL` vs SQL NULL | 沿用既有约定：输入大写 `NULL` 表示置空；小写 `null` 按字面量写入；字面量大写 `"NULL"` 本轮不支持（用查询标签）。 |
| 48 | 写入值含反斜杠/引号/NUL/emoji | 参数化 `?` 传递，不做字符串拼接；原样写入。 |
| 49 | 批量写入中途失败 | 整批事务回滚；pending 保留全量；提示失败行序号。 |
| 50 | 主键行被外部删除 | UPDATE 影响 0 行但事务成功；本轮不报冲突，刷新后行消失（§4.5 取舍）。 |
| 51 | 外部 DDL 导致 `unknown_column` | 自动刷新元数据一次、移除失效条件并提示，不循环重试。 |
| 52 | 切换连接时多个标签有 pending | 聚合弹窗；「全部应用并切换」按标签事务，任一失败中止切换并如实报告。 |
| 53 | 页偏移越界 | `offset > 1e9` 时重置到第 1 页并提示；不发送查询。 |
| 54 | 应用修改成功但原查询失败 | 提示「修改已应用；查询失败，显示的是上一次结果」+ 刷新。 |
| 55 | 关标签时有 pending | 走保护；关闭前可应用/丢弃/取消。 |

---

## 7. 验收标准

> QA 可直接照做。前置：MySQL 8.0，创建 `grid_qa` 测试表与数据（§7.1）。

### 7.1 QA 测试数据

```sql
DROP TABLE IF EXISTS grid_qa;
CREATE TABLE grid_qa (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  name VARCHAR(100) COMMENT '姓名',
  email VARCHAR(200) COMMENT '邮箱',
  status ENUM('new','paid','failed') NOT NULL DEFAULT 'new',
  amount DECIMAL(12,2) NULL,
  created_at DATETIME NULL,
  note TEXT NULL,
  payload JSON NULL,
  avatar BLOB NULL,
  flag TINYINT NULL,
  c01 VARCHAR(20), c02 VARCHAR(20), c03 VARCHAR(20), c04 VARCHAR(20), c05 VARCHAR(20),
  c06 VARCHAR(20), c07 VARCHAR(20), c08 VARCHAR(20), c09 VARCHAR(20), c10 VARCHAR(20)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO grid_qa (name,email,status,amount,created_at,note,payload,flag) VALUES
('a%b',       'a_b@example.com',   'paid',   10.50, '2026-10-01 10:00:00', 'a!b',   JSON_OBJECT('k',1), 1),
('O''Reilly', 'quote@example.com', 'new',    0.00,  '2026-10-02 11:00:00', '',      NULL, 0),
('张三',       'zh@example.com',    'failed', 99.99, '2026-09-30 09:00:00', '中文 😀', JSON_OBJECT('k',2), NULL),
('axb',        'axb@example.com',   'paid',   NULL,  NULL,                 NULL,    NULL, 1),
('empty',      '',                  'new',    0.00,  '2026-10-03 00:00:00', '',      NULL, 0),
('nullmail',   NULL,                'new',    0.00,  '2026-10-04 00:00:00', NULL,    NULL, 0),
('ab',         'ab@example.com',    'new',    0.00,  '2026-10-05 00:00:00', 'ab',    NULL, 0);

-- 约 5000 行（跨连接生成数字序列）
INSERT INTO grid_qa (name,email,status,amount,created_at,flag)
SELECT CONCAT('user', a.n*100 + b.n*10 + c.n),
       CONCAT('u', a.n*100 + b.n*10 + c.n, '@example.com'),
       ELT(1 + MOD(a.n + b.n + c.n, 3), 'new', 'paid', 'failed'),
       MOD(a.n*100 + b.n*10 + c.n, 1000),
       '2026-01-01' + INTERVAL MOD(a.n*100 + b.n*10 + c.n, 240) DAY,
       MOD(a.n, 2)
FROM (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) a
CROSS JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) b
CROSS JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) c;

-- 超过 JS 安全整数范围的 bigint（用于精度验收）；放在最后，避免影响自动递增
INSERT INTO grid_qa (id,name,email,status,amount,created_at,flag)
VALUES (9007199254740993,'bigint','bigint@example.com','paid',1.00,'2026-10-01 00:00:00',1);
```

另建一个视图：

```sql
CREATE OR REPLACE VIEW grid_qa_v AS
SELECT id, email, status, created_at FROM grid_qa;
```

### 7.2 列可见性

- [ ] 打开数据标签，工具栏显示「列 20/20」（不含 `#`）；点击打开面板，列出全部 20 列。
- [ ] **后端元数据**：`DBDescribeTable('grid_qa')` 与 `DBTableData` 响应中的 `columns` 都返回 `email` 列 `comment === '邮箱'`、`name` 列 `comment === '姓名'`（证明已从 `DESCRIBE` 改为 `SHOW FULL COLUMNS`）。
- [ ] 打开数据标签只发一次表数据请求即可拿到列元数据（`DBTableData` 的 `columns`）；不额外调用 `DBDescribeTable`。
- [ ] `grid_qa` 中新增一列 `empty_def VARCHAR(10) NOT NULL DEFAULT ''` 后，元数据 `default === ''`（不是 `null`）；新增 `null_def VARCHAR(10) NULL DEFAULT NULL` 后 `default === null`。
- [ ] 搜索 `prof` 无匹配；搜索 `邮箱` 命中 `email`（注释参与搜索）；搜索 `C0` 命中 c01–c09。
- [ ] 隐藏任意列后，工具栏「列 x/y」按钮有 `--warning` 强调，tooltip 显示已隐藏数。
- [ ] 在搜索结果内点「全选」，只勾选搜索结果；点「清空」，只取消搜索结果；底部提示「作用于搜索结果」。
- [ ] 手动取消最后一个可见列，复选框回弹并提示「至少保留一列可见」。
- [ ] 取消 5 列后点「应用」：网格立即少 5 列，按钮变为「列 15/20」；**不触发数据查询**（观察无 loading/耗时变化）。
- [ ] 点「取消」：网格与按钮不变。
- [ ] 切换到「表」标签再切回「数据」：隐藏集合保持。
- [ ] 切到另一张表再切回：各自恢复；不同表的隐藏集合互不影响。
- [ ] 重启应用并重新打开该表：隐藏集合恢复。
- [ ] 在另一个连接打开同名表：隐藏集合不串。
- [ ] 在结构标签中新增一列：数据标签重新加载后新列可见；已有隐藏列仍隐藏。
- [ ] 在结构标签中把 `name` 改名为 `full_name`：原隐藏名失效，`full_name` 可见；无报错。
- [ ] 删除一个隐藏列：无报错；其余隐藏列不受影响；按钮计数正确。
- [ ] 列头右键「隐藏此列」：立即隐藏并持久化；最后一个可见列时菜单项禁用。
- [ ] 结构标签始终显示全部列与完整 DDL，不受隐藏影响。
- [ ] 先按 `created_at` 排序，再隐藏 `created_at`：工具栏出现「排序: created_at ▼（已隐藏）×」；结果顺序不变；点标签切换方向、点 × 清除排序。
- [ ] 隐藏列可被筛选：在筛选面板选择该列（显示「隐藏」标记），应用后结果正确。
- [ ] 隐藏列上有待提交编辑：工具栏显示 `⚠ N 处位于隐藏列`；点批量应用后数据库值更新。
- [ ] CSV 只包含可见列，且只包含当前页；与屏幕列顺序一致；按钮写「CSV（可见列）」，导出后 toast「已导出 N 列，隐藏 M 列」。
- [ ] Redis 连接不显示「筛选」「列」控件。
- [ ] 手工把 settings 中 hidden 设为全部列：重新打开后自动显示第一列并回写修复。
- [ ] 模拟列元数据加载失败（临时撤权限或断连后重试）：筛选按钮禁用并提示；列面板仍能按列名打开、勾选、应用。

### 7.3 多条件筛选器

- [ ] 点「筛选」打开面板；无已应用条件时有一行空条件，默认第一列 + 等于。
- [ ] 添加 3 条条件：`status = paid`、`created_at >= 2026-10-01`、`email contains foo`；修改过程中结果行数不变（不查库）；点「应用筛选」后 chip 显示 3 条，结果符合预期。
- [ ] 面板内「预览 SQL」显示完整 SELECT，含 3 个条件、ORDER BY（若有）、LIMIT/OFFSET；值与操作符正确；应用筛选前预览的 OFFSET 对应第 1 页。
- [ ] 把预览 SQL 复制到查询标签执行：返回行数与数据标签当前页一致；同一筛选条件下结果集合一致（比较 id 集合）。
- [ ] 操作符按类型变化：`amount` 有 `> >= < <=`；`email` 有 `contains`，没有 `>`；`avatar` 只有 IS NULL/IS NOT NULL；`payload` 只有 contains + NULL。
- [ ] `= ''` 只返回 `email = ''` 的 1 行；不返回 NULL 行；`IS NULL` 只返回 NULL 行。
- [ ] 值含 `'`（O'Reilly）、`"`、`\`、中文、emoji：无 SQL 错误，结果正确。
- [ ] contains 转义：
  - `name contains a%b` 只命中 `a%b`，不命中 `axb`（`%` 不是通配）。
  - `email contains a_b` 只命中 `a_b@example.com`，不命中 `axb@example.com`（`_` 不是通配）。
  - `note contains a!b` 只命中 `a!b`，不命中 `ab`（`!` 本身被转义，不会吞掉后面的 `b`）。
- [ ] 数值列输入 `abc`：行内错误，应用按钮不可用；不发送查询。
- [ ] bigint 值超过 2^53：筛选与显示不丢精度（用 `id = 9007199254740993` 测试）。
- [ ] `created_at >= 2026-10-01`：返回该日 00:00:00 及之后的记录（按列类型比较）；日期值不经过 JS Date。
- [ ] 添加第 6 条条件：按钮禁用并提示最多 5 条；不出现第 6 行。
- [ ] 条件之间只有 AND；找不到 OR 开关；面板提示“需要 OR 用高级 WHERE”。
- [ ] 应用后页码回到第 1 页；筛选保留；上一页禁用；下一页可用（满页时）。
- [ ] 结果元信息显示「本页 N 行」，不显示总行数/总页数；「下一页」只在当前页满页时可用。
- [ ] 点 chip 的 ×：立即重新查询，chip 消失，页码回到第 1 页。
- [ ] 「清除全部」：条件清空，返回无筛选结果，页码第 1 页。
- [ ] 筛出 0 行：显示「没有符合筛选条件的行」+ 调整/清除；不是「此表暂无数据」。
- [ ] 空表：显示「此表暂无数据」；筛选面板仍可打开。
- [ ] 查询进行中：chip 的 `×` 与「应用筛选」禁用，不会并发打出多条长查询。
- [ ] 快速连续点两个 chip 的 ×：最终结果对应最后一次操作，不被旧响应覆盖；旧响应不会把新请求的 loading 置 false 或清掉新错误。
- [ ] 手工构造 `page` 使 `offset > 1e9`：提示分页参数无效并重置到第 1 页；不发送查询。
- [ ] 应用外删除条件列后点应用：返回 `unknown_column`，自动刷新一次元数据、移除失效条件并提示，不循环重试。
- [ ] 慢查询（可在高级 WHERE 用 `SLEEP(70) = 0` 构造）：旧结果保留并置灰，显示「筛选中…」；60s 后错误 banner；可重试。
- [ ] 查询失败时旧结果不被清空，标注“显示的是上一次结果”。
- [ ] 「重置/清除筛选」走待提交编辑保护。

### 7.4 高级 WHERE 模式

- [ ] 结构化面板点「使用高级 WHERE」：高级输入框预填当前条件的可读 WHERE；网格不重新查询；谓词不变；**`appliedSource` 仍为 structured**。
- [ ] 刚进入高级模式、未改文本时「应用」禁用；点「返回筛选器」直接恢复条件快照，不查库。
- [ ] 在高级输入框改条件并点「应用」：结果更新；「高级 WHERE」按钮高亮；未应用时显示 `● 有未应用的修改`；应用后 `appliedSource` 变为 raw。
- [ ] 高级模式支持 `status = 'paid' OR amount > 100`：结果正确（结构化模式做不到）。
- [ ] 高级模式支持 `EXISTS (SELECT 1 FROM ...)` 子查询：结果正确。
- [ ] 输入 `WHERE status = 'paid'`（带前导 WHERE）：自动去掉一个 `WHERE` 后正常执行，并提示「已自动去掉开头的 WHERE」；不生成 `WHERE WHERE`。
- [ ] 输入 `;`、`--`、`#`、`/*`、`/*!50000 1=1 */`：行内错误，拒绝应用，不发送查询。
- [ ] 字符串字面量里含 `;` 或 `--`（如 `note = 'a;b--c'`）：视为普通值，允许执行。
- [ ] 输入顶层 `ORDER BY id`、`GROUP BY status`、`LIMIT 10`、`OFFSET 10`、`UNION SELECT 1`：行内错误，拒绝应用，不发送查询。
- [ ] 列名恰好为 `order` / `group` 的表：`order = 1`、`group = 1` 能正常应用，不被误判为子句。
- [ ] 子查询经派生表使用 `LIMIT`（如 `id IN (SELECT id FROM (SELECT id FROM grid_qa LIMIT 5) AS t)`）：允许，查询正常。
- [ ] 高级模式点「预览 SQL」：显示含当前 WHERE 的完整 SELECT（含系统追加的 LIMIT/OFFSET，OFFSET 对应第 1 页）；不执行；可复制/在查询标签打开。
- [ ] 未修改高级文本点「返回筛选器」：条件快照恢复，不重新查询。
- [ ] 改过高级文本但**未应用**点「返回筛选器」：确认框文案为「放弃修改，当前筛选不变」；「放弃并返回」恢复条件快照且不查库。
- [ ] 改过并**已应用**后点「返回筛选器」：确认框提供「复制 WHERE / 取消 / 清除并返回」；「清除并返回」清空条件并查无筛选结果，且走待提交保护。
- [ ] 切换表：保留当前数据标签的 `filterMode`（会话内），条件与高级文本清空，`appliedSource` 回到 structured。
- [ ] 重启应用：默认结构化模式（模式不持久化）。

### 7.5 快捷筛选

- [ ] 右键文本单元格：菜单含「按此值筛选 / 包含此值 / 排除此值 / 编辑单元格 / 复制值」；右键不进入编辑。
- [ ] 「按此值筛选」：打开筛选面板，新增 `email = 'quote@example.com'`，不自动应用；应用后结果正确。
- [ ] 「包含此值」仅文本列出现；「排除此值」生成 `!=`。
- [ ] 右键 NULL 单元格：只有「筛选 NULL / 筛选非 NULL / 复制」；筛选结果正确。
- [ ] 右键数值/日期单元格：不出现 contains；有 `=`/`!=`。
- [ ] 右键 JSON 单元格：只有 contains + NULL 操作，并有性能/大小写警告。
- [ ] 右键 BLOB 单元格：只有 NULL 操作。
- [ ] 已有 3 条条件时快捷筛选：追加为第 4 条，原有条件保留。
- [ ] 已有 5 条条件时快捷筛选：不添加，提示先删除，打开面板。
- [ ] 重复的相同条件：不重复添加，聚焦已有行。
- [ ] **筛选面板已打开且有未应用草稿**：快捷筛选追加到草稿、聚焦新行；原草稿不丢失、Applied 不变。
- [ ] **高级 WHERE 模式**：单元格右键三个快捷筛选项禁用，菜单顶部提示「先切回结构化筛选」；编辑/复制仍可用；点禁用项无反应、不修改 RawWhere。
- [ ] **高级 WHERE 模式**：列头右键「按此列筛选…」禁用并显示 tooltip；排序/隐藏列仍可用。
- [ ] 右键有未提交修改的单元格：菜单项直接显示数据库值（如「按数据库值筛选（= O'Reilly）」），并有「该单元格有未提交修改」提示；不出现“筛出 0 行”的误导。
- [ ] 超长值（>4096）：快捷筛选禁用并提示。
- [ ] 列头右键「按此列筛选…」：打开面板并预选该列；不自动添加/应用。
- [ ] 列头右键「复制列名」：剪贴板为列名。
- [ ] 列头右键「隐藏此列」：立即隐藏；最后一个可见列时禁用。
- [ ] 数据标签提示文案为「点击表头排序；单击单元格编辑；右键筛选/复制」，不再显示「点击单元格复制」。

### 7.6 待提交编辑保护

- [ ] 修改 2 个单元格（未提交），点筛选「应用筛选」：出现保护弹窗，显示修改数/行数/隐藏列数。
- [ ] 「取消」：筛选不应用，**页码、排序、筛选草稿、表名都不变**，修改仍在。
- [ ] 「丢弃并继续」：修改清空，筛选生效。
- [ ] 「应用修改并继续」：先执行参数化 UPDATE（单事务），再按新筛选查询；数据库值已更新；不会先 reload 一次。
- [ ] 应用修改失败（如断开连接）：整批回滚，修改保留，原操作不继续，错误可见。
- [ ] 修改中有一条 UPDATE 失败（如第二条撞唯一键）：**第一条也回滚**；数据库无部分写入；pending 保留全量。
- [ ] 同样的弹窗出现在：排序、翻页、改每页条数、刷新、切换表、跨库打开另一张表、关闭标签。
- [ ] **切换连接（MySQL→MySQL）**：有 pending 时点侧栏另一个连接，弹聚合保护；「取消」后 `activeId`、标签、修改全部保留，没有半切换状态。
- [ ] **切换连接（MySQL→Redis）**：同上；取消后不进入 Redis 视图。
- [ ] 多个数据标签都有 pending 时切换连接：弹窗显示标签数与总修改数；「全部应用并切换」逐标签事务执行，全部成功后才切换。
- [ ] 跨标签应用中某个标签失败：切换中止；失败标签 pending 保留；已成功标签已落库且 pending 清空；提示如实显示「已应用 X 个标签，Y 个失败」。
- [ ] 保护弹窗期间重复点击触发按钮：不产生第二个弹窗、不重复执行；应用过程中按钮 loading 且不可点。
- [ ] 结构标签对同表执行 `ALTER` / `DROP` 前有 pending：先弹保护；取消则 DDL 不执行。
- [ ] 列可见性应用不弹窗；活动单元格编辑先提交，隐藏后该修改仍在待提交列表。
- [ ] 隐藏列上的待提交修改在应用后数据库正确；工具栏隐藏列提示消失。
- [ ] 复合主键表：两列主键都进入 WHERE，更新只命中目标行。
- [ ] 伪造请求只带复合主键中的一列：后端拒绝（PK 集合必须等于全部主键列），不执行任何 UPDATE。
- [ ] 伪造请求把普通列当 PK：后端拒绝，不执行。
- [ ] 编辑值为反斜杠（`C:\new`、`C:\\`）、单引号、双引号、中文、emoji、NUL：数据库值与输入一致，无 SQL 错误、无额外语句。
- [ ] 编辑值为空字符串 vs SQL NULL：分别写入 `''` 与 NULL；输入大写 `NULL` 仍按既有约定置空，输入小写 `null` 写入字符串 `"null"`。
- [ ] bigint 主键（`9007199254740993`）：更新定位不丢精度、不更新错行。
- [ ] 「应用修改并继续」成功但新查询失败：提示「修改已应用；查询失败，显示的是上一次结果」，并提供刷新。
- [ ] 主键行被外部删除后应用：事务成功但影响 0 行；刷新后行消失（本轮已知取舍，见 §4.5）。

### 7.7 回归与工程

- [ ] v0.3 查询标签执行、历史、CSV、AI 侧栏、结构标签、索引/外键/DDL、Redis 全部回归通过。
- [ ] 数据标签原有的服务端排序、分页、CSV、内联编辑、批量应用行为不回归（除新增保护）。
- [ ] `go test ./...`、`go vet ./...` 通过；`validateAndBuild(cols []ColumnInfo, q TableQuery)` 是纯函数，测试注入假列定义，不依赖真实 MySQL。
- [ ] builder 单测覆盖：所有操作符 × 类型矩阵、`escapeLike`、NULL/空串/引号/反斜杠/Unicode/NUL、分页边界、非法列/操作符/模式冲突、preview 与执行谓词一致。
- [ ] RawWhere 词法单测覆盖：顶层分号、字符串内分号/`--`、反引号标识符、顶层 `ORDER BY`/`GROUP BY`/`LIMIT`/`UNION`、子查询内 `LIMIT`、`/*!50000 ... */` 版本注释、列名 `order`/`group`、前导 `WHERE` 只去一次。
- [ ] `DBUpdateRows` / `buildRowUpdateSQL` 单测覆盖：反斜杠、单引号、NUL、emoji、复合主键、bigint 主键、NULL vs 空串、超过 1000 行、第二条失败整批回滚、PK 列集合不匹配、Set/PK 含未知列。
- [ ] `DescribeTable` 行解析是纯函数并单测：Comment 映射、Default 的 SQL NULL vs 空串、视图、列顺序。
- [ ] 错误码稳定：前端按 `code` 映射 i18n，不按英文 message 匹配；模拟每个 code 都能显示对应中文，未知 code 显示兜底 + 原文。
- [ ] 新增绑定后 Wails 生成文件已更新（3 个绑定），`scripts/dedupe_bindings.py` 已执行；构建通过。
- [ ] 前端最小 vitest 通过：`gridPrefs` 的 key 编码/损坏 JSON/版本不符/未知列求交/0 列修复，数字正则，pending→`RowUpdate` 转换（隐藏列/复合主键/NULL）。
- [ ] i18n 中英 key 对齐脚本通过（不靠肉眼）；`db.err.*` 两边都有。
- [ ] 日志中 grep 不到筛选值、原始 WHERE、结果行、PK/Set 值；错误日志经 `redactSQLError`。
- [ ] 所有新增文案 zh-CN/en-US 完整；无硬编码中文；无硬编码色值（使用 CSS 变量）。
- [ ] 窄窗口（960px）下工具栏不溢出；弹层不超出窗口；条件行可换行。
- [ ] 数据标签不出现 AI 按钮；查询标签的 AI 功能不受影响。

### 7.8 AI 协作

- [ ] 数据标签无 AI 入口、无 AI 设置按钮。
- [ ] 「查看已执行 SQL」→「在查询标签打开」：新建查询标签并填入 SQL，不自动执行；原数据标签状态不变；**不自动触发任何 AI 请求**。
- [ ] 查询标签的 AI 助手可正常使用；v0.3.2 没有新增自动发送路径（检查现有隐私测试仍通过）。
- [ ] 用户主动在查询标签对这段 SQL 点 explain/fix 时，发送前预览中能看到含值的 SQL 文本；这是用户主动行为，不是本功能自动发送。

---

## 8. 不做什么

| 不做 | 原因 |
| --- | --- |
| 结构化条件支持 OR / 嵌套分组 | 操作符面板会变复杂，语义与优先级容易误解；高级 WHERE 已覆盖。 |
| 保存/命名筛选方案、跨设备同步 | 增加存储、命名、冲突与迁移成本；用户需求未验证。 |
| 筛选条件持久化 | 静默筛选会让人误读数据范围；HeidiSQL 的旧筛选残留是前车之鉴。 |
| 客户端过滤 / 当前页本地筛选 | 与服务端分页语义冲突；会得到“本页没有 ≠ 表里没有”的错误结论。 |
| 自动 COUNT / 总页数 | 大表会锁表/卡死；沿用“满页即可下一页”。 |
| 列拖动排序 / 列宽持久化 | 需要保存顺序/宽度并与编辑索引、CSV、结构标签协调；本轮只做可见性。 |
| 服务端列投影（只 SELECT 可见列） | 会让隐藏列上的待提交编辑、排序/筛选与 CSV 语义复杂化；且每次切换可见性都要重新查询。后续可评估。 |
| 查询标签的结果列隐藏 | 查询结果是任意 SELECT，列集合不稳定；先只做数据标签。 |
| 跨列 OR 搜索（HeidiSQL 多列筛选框） | 与结构化 AND 语义冲突；快捷筛选 + 高级 WHERE 足够。 |
| 值下拉 / DISTINCT 值列表 | 需要额外查询、可能暴露/传输大量数据、性能不可控。 |
| 数据标签内的 AI（生成筛选、解释 0 行） | 价值未证明，隐私边界模糊；见 §5。 |
| AI 自动应用筛选 | 永不自动应用；未来也只能生成条件行供人工确认。 |
| 筛选查询取消按钮 | 需要请求生命周期与取消传播；本轮用 60s 超时 + 保留旧结果。 |
| `IN`、`BETWEEN`、`starts with`、`ends with`、`not contains`、正则 | 操作符/测试面扩大；高级 WHERE 可覆盖。 |
| Redis / PostgreSQL 的列与筛选 | 本轮仅 MySQL 数据标签；Redis 无列模型，PostgreSQL 未支持。 |
| 计算列/表达式的可视化构造 | 高级 WHERE 可写表达式；结构化面板只做原始列。 |
| CSV 导出全部筛选结果 / 隐藏列 | 当前页 + 可见列，与屏幕一致；全量导出另立项。 |
| 列隐藏的跨会话撤销栈 | 列面板可重新勾选；不做撤销历史。 |
| 编辑列注释 / 修改表结构 | 列面板只读元数据；DDL 仍在结构标签。 |
| 元数据跨请求缓存 | 本轮每次查询做一次 `SHOW FULL COLUMNS`，避免外部 DDL 与缓存失效问题；延迟明显时再评估。 |
| 写入的 RowsAffected 乐观锁/冲突检测 | 同值更新会误报；本轮用整批事务保证原子性，外部删行刷新后可见。 |
| 高级 WHERE 的 SQL 解析器 | 解析 OR/函数/子查询风险高；只做词法级安全校验与预填。 |

---

## 9. 需要用户确认

以下取舍会直接改变实现，建议逐项拍板：

1. **筛选条件不持久化，只持久化列可见性**（推荐：同意）。
   - 理由：静默筛选风险高；列隐藏有计数证据。
   - 若希望“切表再回来保留筛选”，可改为会话内按表记忆，但会引入陈旧筛选风险。

2. **结构化筛选与高级 WHERE 互斥**（推荐：同意）。
   - 结构化 → 高级可无损预填，但**预填不改变 Applied 来源**；只有改文本并应用后才走 RawWhere（避免 `NO_BACKSLASH_ESCAPES` 语义漂移）。
   - 高级 → 结构化不解析；未应用的修改只放弃草稿、不查库；已应用的 RawWhere 清除需确认并走保护。
   - 若希望两者同时 AND 生效，请明确；这会改变 SQL 预览与错误归因。

3. **条件之间只支持 AND**（推荐：同意）。
   - OR、嵌套、函数、子查询用高级 WHERE。
   - 若本轮必须支持 OR，需要重新设计条件分组与优先级，范围会增加。

4. **CSV 只导出当前页的可见列**（推荐：同意）。
   - 与屏幕一致，避免“隐藏了却还在 CSV 里”的困惑。
   - 若需要全量/全列导出，另开功能。

5. **列可见性只做显示/CSV，不改 `SELECT *`**（推荐：同意）。
   - 隐藏列仍会被查询，保证排序/筛选/隐藏列待提交编辑稳定。
   - 若要“隐藏大字段以减少传输”，需要列投影下推，属于另一个增量。

6. **不做列拖动排序/列宽持久化**（推荐：本轮不做）。
   - 只做显示/隐藏、搜索、全选/清空。

7. **快捷筛选遇到未提交修改的单元格，使用数据库当前值并在菜单项直接标出**（推荐：同意）。
   - 菜单项写成「按数据库值筛选（= O'Reilly）」并显示「该单元格有未提交修改」；比只 toast 更清楚。
   - 备选：禁用快捷筛选。前者更有用，但需要用户看一眼值。

8. **任何重新查询/换表/换连接前保护待提交编辑**（推荐：同意，必须做，PR0）。
   - 覆盖 `watch(activeId)` 清空全部标签、切换 Redis、关标签、结构标签 DDL。
   - 会改变现有“排序/翻页/换连接静默丢弃修改”的行为；这是修正，不是回归。

9. **是否包含「在查询标签打开」桥接**（推荐：包含）。
   - 成本低，价值明确；不涉及 AI，不自动执行。

10. **操作符集合按 §3.3.3 固定**（推荐：同意）。
    - 不提供 `IN/BETWEEN/starts/ends/not contains`；需要时用高级 WHERE。

11. **列注释改为 `SHOW FULL COLUMNS`**（推荐：同意）。
    - 现状 `DESCRIBE` 拿不到 Comment，注释搜索不成立。
    - 选择它而非 `information_schema.COLUMNS`：直接替代、顺序一致、权限不变。若担心额外列/兼容性，可改为 information_schema，但需补 schema/表名条件与测试。

12. **高级 WHERE 模式下快捷筛选禁用**（推荐：方案 a）。
    - 不弹确认、不自动切模式、不拼 RawWhere；提示「先切回结构化筛选」。
    - 备选方案 b：右键后确认「返回筛选器会清除当前高级 WHERE」，确认后切回并追加。交互更重，且用户可能误清条件。

13. **单元格写入改参数化事务**（推荐：同意，必须做，PR0）。
    - 前端不再拼 UPDATE；`DBUpdateRows` 整批事务，失败回滚。
    - 已知取舍：不做 RowsAffected 冲突检测（同值更新会误报）；外部删行时显示成功，刷新可见。

14. **错误码用 `code: message` 字符串**（推荐：同意）。
    - 沿用 `internal/app/ai.go` 约定，前端按 code 映射 i18n，不匹配英文 message。
    - 备选：绑定返回带 `errorCode` 的结构体；需要所有调用点改签名，收益不大。

15. **排期 2 周，保护先行**（推荐：同意）。
    - PR0（保护 + 参数化写入 + 最小 vitest）先合入，再上筛选 UI。
    - 若时间紧，砍「在查询标签打开」或列面板搜索/全选，不砍 PR0。

---

## 10. 参考来源

### 数据库客户端

- TablePlus Filter 文档：<https://tableplus.com/docs/gui-tools/filter>
- TablePlus 快捷键：<https://tableplus.com/docs/utilities/shortcut-keys>
- HeidiSQL 多列筛选建议：<https://www.heidisql.com/forum.php?t=26650>
- HeidiSQL 行筛选讨论：<https://www.heidisql.com/forum.php?t=6240>
- HeidiSQL 全列搜索：<https://www.heidisql.com/forum.php?t=17992>
- HeidiSQL 切换表后筛选残留 #1879：<https://github.com/HeidiSQL/HeidiSQL/issues/1879>
- HeidiSQL 多列筛选行为 #292：<https://github.com/HeidiSQL/HeidiSQL/issues/292>
- DataGrip Filter data：<https://www.jetbrains.com/help/datagrip/tables-filter.html>
- DataGrip Explore data in data editor：<https://www.jetbrains.com/help/datagrip/explore-data-in-data-editor.html>
- DBeaver Data Filters：<https://dbeaver.com/docs/team-edition/24.3/desktop/Data-Filters/>
- DBeaver Wiki Data Filters：<https://github.com/dbeaver/dbeaver/wiki/Data-Filters>

### MySQL 语义

- MySQL 8.0 `SHOW FULL COLUMNS`（含 `Comment`）：<https://dev.mysql.com/doc/refman/8.0/en/show-columns.html>
- MySQL 8.0 `INFORMATION_SCHEMA.COLUMNS`（对照方案）：<https://dev.mysql.com/doc/refman/8.0/en/information-schema-columns-table.html>
- MySQL 8.0 String Comparison Functions（LIKE / ESCAPE）：<https://dev.mysql.com/doc/refman/8.0/en/string-comparison-functions.html>
- MySQL 8.0 注释语法与可执行版本注释 `/*! ... */`：<https://dev.mysql.com/doc/refman/8.0/en/comments.html>
- MySQL JSON 与 `LIKE` 的隐式转换与限制（社区资料）：<https://stackoverflow.com/questions/46702159/mysql-like-query-in-json-field>

### 项目内

- 数据标签现状：`frontend/src/components/DBView.vue`（`buildTableSQL`、`loadTableData`、`applyEdits`、`watch(activeId)`、数据标签模板）
- 列定义：`internal/db/mysql.go`（`DescribeTable`、`ColumnInfo`、`Result`、`scanTopKeywords`）
- 参数化写入：`internal/db/mysql.go`（`RowUpdate`、`buildRowUpdateSQL`、`applyRowUpdates`、`UpdateRows`）
- 错误码先例：`internal/app/ai.go`（`aiError` / `aiErrorCode`）；`frontend/src/components/AIPanel.vue` 的 code 解析
- 设计系统：`frontend/src/styles/global.css`
- AI 助手设计：`docs/design/001-next-iteration.md`；v0.3.1 已发布
- K8s 面板：`docs/design/002-k8s-panel.md`（用户决定顺延，不在本轮）
