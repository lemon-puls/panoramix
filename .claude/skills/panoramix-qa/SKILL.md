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
