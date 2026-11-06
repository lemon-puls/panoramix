---
name: panoramix-qa
description: Panoramix 验收与测试技能包。包含验收方法、headless 浏览器验证脚本、核心回归路径与易漏边界。qa 在验收时使用。
---

# Panoramix 验收技能包

> 每次验收后追加：新的验证手法、易漏边界、历史缺陷模式。

## 一、必跑的门禁

```bash
cd /mnt/d/develop_project/vista
go vet ./...
go test ./internal/... -count=1
GOOS=windows GOARCH=amd64 go build -o /tmp/check.exe . && rm -f /tmp/check.exe
cd frontend && npx vue-tsc --noEmit
```

任一失败即视为阻塞缺陷。

## 二、前端验证（无需 Wails 窗口）

预览页支持 mock 数据，可独立渲染：

```bash
# 启动（先于截图）
cd frontend && npx vite --port 5241 --strictPort

# 截图 / 抓 DOM（Windows Chrome headless）
"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe" \
  --headless=new --disable-gpu --no-sandbox --incognito \
  --virtual-time-budget=25000 --window-size=1600,900 \
  --dump-dom "http://127.0.0.1:5241/preview.html?app=1&view=db&autodata=1" > /tmp/qa.html
```

可用参数：
- `?app=1` 渲染完整 App；`&view=db` 直接进数据库视图
- `&autodata=1` 自动展开第一个库并打开第一张表的数据标签
- `?panel=files|db` 单独渲染组件

**注意**：截图经 PNG 压缩后深色界面偏暗，判断布局用 DOM 测量更可靠：

```javascript
// 在页面注入后 dump-dom 读取
const tb = document.querySelector('.toolbar')
`tb=${tb.clientWidth}/${tb.scrollWidth}`
```

## 三、核心回归路径（每次发版前）

1. **SSH**：连接 → 终端输入 `ls` → vim 打开文件 → 断开重连 → 标签置灰
2. **SFTP**：双击表目录进入 → 上传小文件 → 下载校验内容 → 取消传输后重试（续传）
3. **监控**：打开监控面板 → 指标有数值 → 曲线绘制 → 断开后显示"已停止"
4. **数据库**：连接 → 库/表树 → 单击表（表+数据标签同时出现）→ 双击表（激活数据）→ 查询标签执行 SQL
5. **表结构编辑**：加字段 → SQL 预览 → 确认 → 字段出现 → 删除
6. **数据编辑**：改单元格 → ✓ 应用 → 数据刷新；再改 → ✕ 放弃

## 四、易漏边界

| 场景 | 检查点 |
| --- | --- |
| 无主键表 | 点击单元格应提示无法编辑，而不是静默失败 |
| 空结果集 | 显示"0 行"提示，而不是空白 |
| 表名/列名含反引号或引号 | SQL 转义正确（标识符用 `` ` `` 双写，字符串用 `''`） |
| 大表（>10万行） | 分页不卡死；不自动 COUNT |
| 重复点击 | 同一张表重复单击不重复请求 |
| 危险 DDL | 必须先出现 SQL 预览弹窗 |
| 凭据 | 日志与界面不出现密码；keychain 里能查到条目 |

## 五、历史缺陷模式（重点回归）

1. **布局类**：flex 挤压导致按钮不可见；类名冲突导致样式覆盖；表格内输入框撑宽列
2. **响应式类**：ref 中的普通对象修改不更新界面；import 的 ref 在模板不解包
3. **状态竞态**：加载标志卡死（loading 未复位）；重复请求
4. **数据安全**：UNIQUE 冲突导致更新失败（应用修改时要提示）

## 六、AI 助手（v0.3.1）验收手法

### 6.1 预览页参数

完整 App 请用 `?app=1&view=db&ai=1`（`view=db` 由 App.vue 读取；`db=1` 只改 store，
App 不认，别混用）。可用 mock：

- `&ai_setup=1`：未配置状态（会自动弹出设置弹窗）
- `&ai_cloud=1`：云端 endpoint（needsConsent=true，弹云端授权）
- `&autodata=1`：自动打开第一张表的数据标签
- `&locale=en-US`：预览页只会切到 en，不会切回 zh；i18n 会写 localStorage，
  同一个 Chrome profile 会一直保持 en。切回中文要清 localStorage 或换 profile。

### 6.2 用 CDP 做断言（Node 22 自带 WebSocket）

`--dump-dom` 只能看静态 DOM；交互/布局用 remote debugging + Node：

```javascript
// 1) 启动独立的 headless Chrome（端口不要用 9223，常被用户自己的 Chrome 占用）
//    chrome.exe --headless=new --remote-debugging-port=9333 --user-data-dir=C:\Temp\qa-profile ...
// 2) 从 /json/list 取 webSocketDebuggerUrl
// 3) Runtime.evaluate 注入；点击用 Input.dispatchMouseEvent（JS .click() 对部分组件不稳）
```

易错点：
- 点击前 `el.scrollIntoView({block:'center'})`，否则按钮在 AI 侧栏滚动区外，点击落空。
- 读布局用 `clientWidth/scrollWidth`：`scrollWidth > clientWidth` 就是溢出。
- 预览页 locale 会残留，选择器优先用英文文案或显式清 localStorage。

### 6.3 覆盖 AI 响应分支（不改产品代码）

wailsjs binding 在调用时才取 `window.go.app.Service.*`，所以可在页面里覆写：

```javascript
window.go.app.Service.AIGenerate = async () => ({
  sql: "UPDATE users SET email='x';", isWrite: true, insertAllowed: true,
  insertBlockReason: '', multiStatement: false, dangerous: false, uncertain: false,
  warnings: [{code:'write_operation'}], explanation:'', durationMs:1,
});
window.__dbq = 0;
window.go.app.Service.DBQuery = async () => { window.__dbq++; ... };
```

用它验证：写操作二次确认弹窗、多语句禁用插入、插入后 `__dbq === 0`（永不自动执行）。

### 6.4 隐私取证（必须自己做，不信自述）

写一个临时 `internal/app/qa_*_test.go`（验收后删除），用 `memSecrets` + `sqlite`
临时目录，保存 DB 连接（用户名/地址/密码）与 AI key，然后：

1. `AIPrepare` 后断言 payload 不含 DB 密码/地址/用户名/API key；
2. `log.SetOutput(&buf)` 捕获 Prepare+Generate 日志，断言不含 key、问题、DDL、响应；
3. `store.Close()` 后按字节读 `.db`/`.db-wal`/`.db-shm`，grep key/密码/问题/响应；
4. 注意 SQLite 自身 `sqlite_master` 含 "CREATE TABLE"，不要拿泛化字符串误报，
   搜具体 DDL 片段（如 ``CREATE TABLE `users```）。

边界结论（v0.3.1 实测）：
- 连接凭据、API key、主机地址、用户名不进 payload/日志/SQLite；
- 用户手写的问题、fix 模式的错误原文、表/列 COMMENT 会按设计进入 payload；
  这是 schema 元数据/用户输入，预览里可见、可编辑，属于残余风险，不是行数据泄漏。

### 6.5 AI 特有的易漏安全项

| 场景 | 检查点 |
| --- | --- |
| 清除云端授权后仍有待发送预览 | AIGenerate 必须按当前 consent 重查，不能只用 prepare 时的快照 |
| LAN endpoint 授权 | 设置页应能查看/撤销非 loopback（含 LAN）授权，不能只处理 cloud |
| 重定向 | 本机 endpoint 302/307 到公网必须拒绝，不能跟随 |
| 版本注释 | `/*!50000 ... */` 内容 MySQL 会执行；Analyze 已展开后判定（v0.3.1 修复） |
| 并发上限 | 已改为带等待队列的信号量；缩容不影响在途计数（v0.3.1 修复） |
| 停止按钮 | 预览/等待确认阶段点“停止”已等同 cancelPending；请求阶段迟到 delta/结果由 runToken 丢弃（v0.3.1 修复） |
| 写权限 | AIGenerate 发送时重查 AllowWrite；预览后关闭设置立即生效（v0.3.1 修复） |

### 6.6 预览 mock 的已知缺口

- `preview-main.ts` 只 mock 了 `DBShowCreateTable`，没有 `DBTableMeta` /
  `DBListIndexes` / `DBListForeignKeys`；打开结构标签会报
  `TypeError: window.go.app.Service.DBTableMeta is not a function`。
  这是预览 mock 缺口，不是产品缺陷；结构内容需真实 MySQL 或补 mock。
- 预览 mock 的 `AIGenerate` 永远返回只读 SQL，无法直接验证写/多语句分支，
  用 6.3 的覆写手法。

### 6.7 不要碰用户的 Chrome

`--remote-debugging-port=9223` 在本机常已被用户自己的 Chrome 占用，
误连会看到无关标签页。务必用独立端口 + 独立 `--user-data-dir`，
验收结束 `pkill -f <自己的 profile 名>` 清理。

## 七、v0.3.2 PR0（参数化写入 + 待提交保护）验收手法

### 7.1 真实 MySQL 集成测试

本机 3306 可能没有可用凭据（不要猜密码，分类器会拦截）。用 Docker 起独立实例：

```bash
# 本机 registry mirror 可能是 docker.1ms.run；library/mysql:8.4 已验证可用
docker pull docker.1ms.run/library/mysql:8.4
docker run -d --name panoramix-qa-mysql \
  -e MYSQL_ROOT_PASSWORD=qa_pass -e MYSQL_DATABASE=qa \
  -p 13306:3306 docker.1ms.run/library/mysql:8.4
```

在 `internal/db/` 写临时 `qa_*_integration_test.go`（验收后删除），用环境变量门控：

```bash
QA_MYSQL_ADDR=127.0.0.1:13306 QA_MYSQL_PASS=qa_pass \
  go test ./internal/db -run TestQARealMySQL -count=1 -v
```

必须覆盖：`C:\temp\new` / `C:\\` / `C:\` 读回、单双引号、中文、emoji、NUL、
空串 vs NULL、NULL 主键 `IS NULL`、复合主键、bigint >2^53、第二条失败整批回滚。
Dolt 镜像可作备胎，但默认只允许 `root@localhost`：加 `-e DOLT_ROOT_HOST='%'`。

### 7.2 参数化写入的「伪造请求」测试（易漏）

只测正常前端路径不够。后端必须自己校验 `PK 集合 == 全部 PRI 列`。测法：
建复合主键表，只传其中一列，若返回成功且多行被改，即数据安全阻塞缺陷。
v0.3.2 PR0 实测该缺陷成立（`DBUpdateRows` 无 `SHOW FULL COLUMNS` 校验）。

### 7.3 预览页的组件注册坑

- `DBView.vue` 模板用了 `<n-select>` 但只导入了 `NButton/NInput/NInputNumber`，
  页大小下拉框会渲染成未解析的 `<N-SELECT>` 自定义元素。判断方法：
  `document.querySelector('.toolbar > n-select')` 有标签但无 `.n-base-selection`。
  要验证 `changePageSize` 的保护逻辑，可走 Vue 内部：
  ```js
  const s = document.querySelector('.db-view').__vueParentComponent.setupState
  s.changePageSize(s.activeTab, 100)   // 直接触发 handler + 弹窗
  ```
- 读内部状态同样用 `setupState`（`tabs.find(t=>t.kind==='data')`），比猜 DOM 稳。

### 7.4 保护弹窗断言

- 保护弹窗按钮（v0.3.2 PR0）：`取消` / `继续（丢失修改）`；设计期望三按钮，
  断言 `buttons.includes('应用') === false` 可直接抓出「应用并继续」缺失。
- Naive UI 弹窗 leave 过渡不结束，节点会 `opacity:0` 残留。只选
  `getComputedStyle(d).opacity > 0 && d.getClientRects().length > 0` 的弹窗。
- 弹窗按钮列表里常有一个空文本按钮（关闭图标），断言前 `.filter(Boolean)`。
- AI 首次发送确认按钮文案是 **`确认发送`**，不是 `发送`。

### 7.5 保护入口测试矩阵（每条都要先造 pending）

| 入口 | 取消后必查 |
| --- | --- |
| 刷新 / 筛选回车 / 排序 / 翻页 / 页大小 | pending 保留、页码/排序/pageSize 不变 |
| 切表（单击结构 / 双击数据） | 数据标签 table 不变、active 不变、pending 保留 |
| 切连接（MySQL↔Redis） | activeId 不变、标签保留、Redis 未进入 |
| 切视图（数据库↔终端） | `db-view` 仍在、pending 保留 |
| 关标签 | 数据标签 pinned 无 ×；解除 pinned 后 `canCloseTab=true` |
| 结构标签 DDL | **必须**先 pending 弹窗；只出现 DDL 预览即缺陷 |

### 7.6 既有坑（v0.3.2 实测，别误判为本分支引入）

- Redis 选中 key 后右侧 workspace 为空（模板没有 Redis 值查看分支）；
  `main`/`feat/v0.3-database` 同样存在。
- 页大小 `<n-select>` 未注册（见 7.3），跨分支存在。
- CDP 端口 9333 常被其他 QA 会话占用：启动失败会打印
  `Cannot start http server for devtools`。换 9444/9445，并用
  `Get-CimInstance Win32_Process` 确认 `--user-data-dir` 是自己的再操作；
  清理用 `taskkill /PID <pid> /T /F` + Windows `cmd /c rmdir /s /q`。

### 7.7 v0.3.2 PR0 复审手法（2026-11-05）

- 三按钮弹窗用 `dialog.create` + 自定义 `action` 渲染；断言按钮文本为
  `取消 / 丢弃并继续 / 应用并继续`，不能只数 2 个。
- 「应用并继续」要分别验证：请求值原样、写后清 pending、随后原操作继续
  （刷新则 `__dbQueries` 增长；切表则 data tab 变化）。
- 失败路径用预览 mock `window.__dbUpdateFail = true`：断言原操作中止、
  pending 保留、目标表未查询、错误码可见。
- 写入冻结：`setupState.tabs.find(t=>t.kind==='data').loading = true` 后
  **等一个 Vue tick 再读** `button.disabled`，立刻读会得到 false（假失败）。
- DDL 保护的三个入口都要测：单击表打开结构、结构标签点 DDL、
  结构标签执行删列/ALTER；取消后分别断言 active tab、structView、pending。
- 「应用成功但重查失败」可用覆写 `DBQuery` 在 apply 后抛错复现；
  检查页面是否出现「修改已应用；查询失败」提示，而不是只看 toast。
- 复审不要只看提交说明：错误码名（`too_many_edits` vs `too_many_updates`）
  与设计文档逐字对齐；前端是否真的按 code 映射 i18n 也要单独 grep。

## 八、v0.3.2 PR1（列可见性 / 筛选 / 快捷筛选）验收手法

### 8.1 真实 MySQL 数据的字符集坑（必看）

`docker exec -i ... mysql < data.sql` 默认客户端字符集不是 utf8mb4 时，中文注释与
数据会被双重编码（`邮箱` → `é‚®ç®±`），随后所有「注释搜索/中文筛选」测试都会
假失败。加载数据必须加 `--default-character-set=utf8mb4`：

```bash
docker exec -i panoramix-qa-mysql mysql -uroot -pqa_pass --default-character-set=utf8mb4 qa < /tmp/grid_qa.sql
```

用 `SELECT HEX(COLUMN_COMMENT)` 验证（`邮箱` = `E982AEE7AEB1`）。另：MySQL 8 不支持
`IN (SELECT ... LIMIT n)`（错误 1235），设计里的这个验收示例本身不成立；验证
「子查询内 LIMIT 允许」要用派生表 `IN (SELECT id FROM (SELECT ... LIMIT 5) t)`。

### 8.2 空状态分支的「死代码」检查（本分支抓到）

模板外层若是 `<template v-if="result && result.columns.length > 0">`，那么
`v-else-if="result.rows.length === 0"` 的空状态永远不可达——真实 SELECT 返回 0 行时
`scanRows` 仍会带列名。验收必须用「有列、0 行」的结果实测，而不是只看模板文案：

```js
window.go.app.Service.DBTableData = async () => ({
  result: { columns: tab.result.columns, rows: [], affected: 0, durationMs: 2, truncated: false },
  columns: tab.columns, executedSql: '...',
})
await s.loadTableData(tab, true, 'discard')
// 断言 .result-empty 存在；实际是 grid 渲染但 tbody 为 0 行 => 缺陷
```

同理：`v-else` 里的空状态没有「刷新」按钮也要一并核对。

### 8.3 筛选预览的请求时机

设计要求「预览只在展开时请求」。在页面里覆写并计数：

```js
let n = 0
window.go.app.Service.DBPreviewTableSQL = async (...a) => { n++; return orig(...a) }
s.onDraftChange(tab, [{column:'status',op:'eq',value:'p'}])
```

若预览折叠时 `n` 仍增长，即每次编辑都打一次后端（真实实现是 `SHOW FULL COLUMNS`），
属于「修改过程中不查库」的违例。

### 8.4 弹窗 / 面板的可测试断言

- 保护弹窗文案：`dialogText.includes('行')` / `includes('隐藏')`，只看按钮不够；
  连接切换还应有「会关闭 N 个标签」的聚合文案。
- Esc / 点外关闭：不要只点「取消」。用
  `document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`
  与在弹层外元素上派发 `mousedown`+`click`，断言 `filterPanelOpen/columnsPanelOpen`。
- 两个 popover 互斥：先开筛选再开列面板，断言不能同时为 true。
- chip 聚焦：点第 2 个 chip 后，`[...document.querySelectorAll('.grid-filter-row')]
  .findIndex(r => r.contains(document.activeElement))` 应为 1（本分支实际为 0）。
- 加载态：`tab.loading=true` 时断言网格 `opacity`、结果元信息是否出现「筛选中」；
  只断言按钮 disabled 会漏掉视觉反馈缺失。

### 8.5 用 mock 覆写覆盖错误分支

```js
const orig = window.go.app.Service.DBTableData
window.go.app.Service.DBTableData = async () => { throw new Error('invalid_page: ...') }
// 断言 tab.page 是否真的回到 0（i18n 文案写“已重置”时尤其要查）
window.go.app.Service.DBTableData = orig
```

- `unknown_column`：覆写 `DBDescribeTable` 删掉一列，断言 `DBTableData` 只重试 1 次、
  条件被移除、提示出现。注意提示里的「已移除 N 条」会把 Applied 与 Draft 同一条
  重复计数。
- `table_metadata_unavailable`：断言筛选按钮禁用、列面板仍可打开；同时检查按钮
  tooltip 是否把原始 `code: message` 直接露出来（应走 i18n）。
- 应用成功但重查失败：先 `__dbUpdateFail=false`、再 `__dbQueryFail=true`，点
  「应用并继续」，断言出现「修改已应用，但刷新数据失败」。

### 8.6 快捷筛选核对清单（按菜单选项断言，不点真实菜单）

`setupState.cellMenuOptions` 可直接断言文案与 `disabled`：
文本/数值/日期/枚举/JSON/BLOB/NULL 六类各取一个单元格；再断言：
- pending 单元格标签用「数据库值」且底部有「该单元格有未提交修改」；
- raw 模式三项 disabled、编辑/复制仍可用；
- 菜单里是否真的有 JSON 性能/大小写警告（本分支缺失）；
- 快捷筛选后 `conditions` 不变、`conditionsDraft` 追加、`__dbQueries` 不增长。

### 8.7 列可见性核对清单

- 应用隐藏后 `__dbQueries` 不增长；`GetGridPref` 返回 `{"v":1,"hidden":[...]}`。
- CSV 用覆写 `URL.createObjectURL` 抓 Blob：`await blob.text()` 断言表头只含可见列、
  行数 = 当前页。
- 至少一列：面板逐个取消到只剩一列，再点最后一个 checkbox，断言回弹 + 提示；
  「清空」在无搜索词时保留第一列。
- 全部隐藏的持久化值：`SetGridPref` 写入全部列 → `loadTableData` 后应修复为
  `columns.slice(1)` 并回写。
- 隐藏列上的 pending：`startEdit(tab,0,7)` 后 `commitEdit`，断言 `DBUpdateRows`
  载荷的 `set` 仍是原列名（原始索引 7），工具栏出现 `⚠ N 处位于隐藏列`。

### 8.8 存储位置提示

v0.3.2 最终把列偏好从设计稿里的 localStorage 改成了 settings 表
（`grid.columns.<connId>.<db>.<table>`，绑定 `GetGridPref/SetGridPref/
DeleteGridPrefsByPrefix`）。设计文档 §3.2.4/§4.2 未同步，验收按 settings 表为准。

### 8.9 复审补充（2026-11-06，4 个修复提交）

- 空状态 mock：预览页支持 `?empty=1`（空表）、`?empty_after_filter=1`（有谓词时 0 行）、
  `?empty_page=1`（page>0 时 0 行）、`?delay_ms=1200`（慢查询）。三种空状态要分别
  验证文案与操作按钮，并点「清除筛选 / 回到第一页 / 刷新」确认恢复。
- 点外关闭必须用**真实鼠标事件**：`Input.dispatchMouseEvent`
  （mouseMoved/mousePressed/mouseReleased）在面板外坐标点击。合成
  `mousedown`/`click` 会被 Naive UI 的 clickoutside 漏判，得到假失败。
- 打开 NSelect 下拉选值（菜单 teleport 到 body）不会触发 popover 的 clickoutside；
  看到面板仍开 + 草稿列/操作符已变即为通过。
- 前导 WHERE：预览 mock 会把 `hadLeadingWhere` 回给前端；验证 toast、输入框归一化、
  刷新不重复提示，以及真实 MySQL 的 `ExecutedSQL` 不出现 `WHERE WHERE`。
- 行内错误：高级模式把 `filterError` 直接渲染在输入框下方；注意这个字段是原始
  `code: message`，若结果 banner 已中文而行内仍是英文，属于 i18n 映射遗漏。
- 查询反馈：`?delay_ms=...` 下抓 `.grid-progress`、`.grid-stale`
  （opacity/pointer-events）、元信息「筛选中…」；等结束后元信息应为「本页 N 行」。
- 列面板禁用时标题仍可能是「选择要显示的列」，与设计「数据加载后可用」不一致；
  属于轻微文案项，不要和“按钮未禁用”混淆。

### 8.10 clickoutside 与 teleport 菜单的交互坑（2026-11-06 第二轮复审）

- popover 加 `@clickoutside` 后，**右键**（button=2）与 **teleport 到 body 的
  dropdown/select 菜单左键**都会被判定为 outside。验收时必须测：
  「面板打开 + 未应用草稿 → 右键单元格 → 左键点快捷筛选菜单项」。
  期望草稿保留；若面板被先关闭、`appendQuickCondition` 以 Applied 为基底，
  就是草稿丢失回归。
- 正确做法是先快照：`openCellMenu`/`openHeaderMenu` 时保存 `conditionsDraft`，
  快捷筛选优先用快照；clickoutside 处理器忽略右键、`cellMenu.show` 与
  `.n-dropdown-menu/.n-base-select-menu` 内的点击。
- 该场景必须用真实 `Input.dispatchMouseEvent` 复测；合成 `.click()` 与
  `new MouseEvent` 的事件顺序/目标与真实不一致，会漏掉或误报。
- 菜单打开时点真正面板外：第一次点击关菜单、第二次关面板，属可接受行为；
  只要不是永久卡住。
- 回归时顺带测「点筛选触发按钮（面板已开且带草稿）」：当前实现会 close+reopen，
  草稿回填 Applied；设计未定义此交互，可作非阻塞观察记录。
