---
name: panoramix-engineering
description: Panoramix 工程实现技能包。包含技术栈、架构约束与历史踩坑记录（Wails/Vue/Go 的具体坑与解法）。engineer 在实现前必读。
---

# Panoramix 工程技能包

> 每次遇到非显而易见的坑就追加。格式：现象 → 根因 → 解法。

## 一、技术栈与约束

- Go 1.27.x + Wails v2.17 + Vue3/TS + Naive UI + Pinia + vue-i18n
- SQLite（modernc，纯 Go）、keychain（zalando/go-keyring）
- 测试：`go test ./internal/...`；前端类型 `npx vue-tsc --noEmit`
- **依赖方向**：`app → ssh/sftp/store/monitor/db/xshell/fsx`；`internal/*` 禁止 import wails；`internal/*` 之间禁止横向 import（`ClientSource` 等接口注入）

## 二、Wails 相关

### 绑定文件重复声明
- 现象：Rollup 报 `Identifier X has already been declared`
- 根因：`wails dev` 自动重新生成 `frontend/wailsjs/go/app/*`，与手写绑定叠加
- 解法：改绑定后跑 `python3 scripts/dedupe_bindings.py`

### 模板无法解包 import 的 ref
- 现象：`v-if="someView === 'db'"` 恒为 false，界面不切换
- 根因：`<script setup>` 编译器不识别 import 进来的 ref 类型，模板生成裸变量比较（Ref 对象 ≠ 字符串）
- 解法：本地 `computed` 包装后再在模板使用：`const isDB = computed(() => mainView.value === 'db')`

### ref 数组中的对象被外部修改不响应
- 现象：`tab.loading = false` 后界面仍显示"加载中"
- 根因：`ref([])` 中 push 的是普通对象，外部持有原始引用，赋值绕过代理
- 解法：创建时用 `reactive({...})`，或从数组按索引取代理对象

### 文件拖放不触发
- 现象：`OnFileDrop` 永不回调
- 根因：Wails 需显式开启 `options.DragAndDrop{EnableFileDrop: true}`
- 解法：在 `wails.Run` 的 options 里配置

### 文件对话框签名
- `runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{...})` —— **传值不是指针**
- 字段名是 `DefaultFilename`（不是 DefaultFileName）

### 原生元素与组件语法
- `<textarea v-model="x">` 正确；`v-model:value` 只用于组件

### PTY 尺寸参数顺序
- `RequestPty(term, h, w)`、`WindowChange(h, w)` —— **行在前**
- 传反会导致 top/vim 等全屏应用交错刷新

## 三、CSS / 布局

### 类名冲突
- 现象：新按钮位置诡异、宽度被压成 10px
- 根因：复用了别处已定义的类名（如 `.edit-btn` 在 Redis 值查看器里是 `position:absolute`）
- 解法：新组件用带前缀的类名（`.cell-edit-btn`）；复用前先 Grep 全局

### 表格内输入框撑宽列
- 现象：编辑单元格时整表列宽错乱
- 根因：`width:100%` 在 `table-layout:auto` 的 td 内形成循环计算
- 解法：td `position: relative`，输入框 `position: absolute; inset: 1px`

### flex 挤压
- 现象：工具栏右侧按钮不可见
- 根因：`flex-shrink` 默认为 1，左侧 `flex:1` 元素抢占空间
- 解法：关键按钮组 `flex: 0 0 auto; min-width: max-content`；伸展元素设 `max-width`

## 四、跨平台

### WSL 下 Vite 收不到文件变更
- 解法：`vite.config.ts` 开启 `server.watch.usePolling: true`

### Windows/WSL 共用 node_modules
- 现象：`npm install` 报 `ENOENT mkdir @esbuild/linux-x64`；一边构建另一边坏
- 解法：哪个平台构建前端，就在哪个平台 `npm install`；Go 侧不受影响

## 五、后端要点

### SQLite 并发
- `db.SetMaxOpenConns(1)` + WAL + busy_timeout，避免 modernc 驱动的锁问题

### 迁移必须幂等
- 用 `PRAGMA table_info` 探测列是否存在后再 `ALTER TABLE ADD COLUMN`

### 凭据
- 只进 keychain；`HostInput.String()/GoString()` 必须脱敏；有专门测试守卫

### 传输队列调度
- `pump()` 选中任务必须在同一把锁内同步置为 running，否则会被重复调度（曾导致单任务双 worker）

### 断点续传
- 进度计数必须基于断点基数（`h.Transferred`），不是本次增量
- 取消/失败要**保留**半成品（否则无法续传）

### 外部命令与 PTY
- 构造测试服务端时要**忠实应用客户端声明的尺寸**，否则会掩盖客户端 bug

## 六、AI / 流式 / 预览（v0.3.1）

### Go raw string 不能包含反引号
- 现象：`go build` 报 `missing ',' in argument list`，行号指向 prompt 字符串
- 根因：用反引号原始字符串写 `` ```sql `` 代码块，反引号提前终止字面量
- 解法：prompt 里的 ``` 用双引号字符串 + `\n` 拼接，或拼接 `"```"`

### 手写绑定 + Wails 自动生成会重复
- 现象：Rollup `Identifier AICancel has already been declared`
- 根因：`wails dev` 重新生成 `frontend/wailsjs/go/app/*`，与手写声明叠加
- 解法：改完绑定立即跑 `python3 scripts/dedupe_bindings.py`（本次去重 8 个）

### 预览 = 实发必须靠缓存，不能靠“再构建一次”
- 现象：预览后改问题/表结构，实际发送与预览不一致
- 根因：preview 与 generate 各自构建 payload
- 解法：`AIPrepare` 生成 `prepareId` 并缓存 system/user 原文；`AIGenerate` 只取缓存
  发送（单次使用 + 5 分钟过期）；测试用 httptest 捕获消息体断言逐字相等

### 取消 map 以 requestId 为键：不同请求必须用不同 ID
- 现象：两个标签并发生成，其中一个莫名 `canceled`
- 根因：同一 requestId 的“新请求取消旧请求”逻辑被不同标签复用
- 解法：前端每个请求生成独立 ID（`crypto.randomUUID` 不存在时用时间戳 fallback）

### NInput 上的 keydown 监听可能到不了内部 textarea
- 现象：AI 输入框 Ctrl+Enter 不触发生成
- 解法：需要键盘交互的输入用原生 `<textarea v-model>`，事件监听可靠
- 插入编辑器用 `setRangeText` + `input` 事件（保留 undo），不要在前后无空白时直接拼接

### 禁止环境代理 / 禁止重定向
- 现象：本机 endpoint 的 schema 被 307 转发到公网
- 解法：`http.Transport{Proxy: nil}` + `CheckRedirect` 返回 `http.ErrUseLastResponse`；
  3xx 一律判错。`httptest` 起第二个 server 断言命中数为 0

### 本地 7B 冷启动超时
- 60s 不够（首次加载模型可能 90s+）；总超时默认 120s，`ResponseHeaderTimeout` 60s，
  错误码区分 `timeout` / `connection` / `model_not_found`

### 预览页验证 AI 侧栏
- `preview.html?app=1&view=db&ai=1` 会自动展开 AI 侧栏（mock provider，不联网）
- 用 Chrome `--remote-debugging-port=9222` + Node 全局 WebSocket 可跑完整交互：
  填问题 → 生成 → 首次发送确认 → 插入编辑器；同时用 `clientWidth/scrollWidth` 查溢出
- 额外场景参数：`&ai_cloud=1` / `&ai_lan=1` / `&ai_setup=1` / `&ai_consent=1` / `&ai_sensitive=1`

### Prepare 快照不能替代发送时的授权检查
- 现象：预览后清除云端授权，旧 prepareId 仍能发送
- 根因：AIGenerate 只信 Prepare 时的 NeedsConsent 快照
- 解法：发送前按当前 `hasCloudConsent(endpointHash)` 重判；无授权返回 `consent_required`，
  用户确认（consent=true）时才重新记录

### 信号量缩容不能替换 channel
- 现象：并发 2→1 后短暂出现 2 个请求同时打到 provider；或旧 token 释放后计数错乱
- 根因：channel 被替换，但在途 token 还留在旧 channel；仅判断 `len==0` 仍有竞态
- 解法：用带等待队列的信号量（`active/limit/waiters` + wake channel）。
  resize 只改 limit：缩容不影响在途请求，扩容立即唤醒 waiter；被唤醒者重新竞争。
- 取消路径尤其小心：waiter 可能已被 release 唤醒（槽位已转交），随后 ctx 同时取消；
  取消时必须 `wakeLocked()` 把槽位继续交给下一个 waiter，否则后续请求永久 queued。
  不要用“替换 buffered channel”的方案

### MySQL 版本注释是可执行 SQL
- 现象：`/*!50000 UPDATE ... */` 被当成普通注释，判定为只读
- 根因：词法扫描把所有 `/* ... */` 都跳过
- 解法：扫描前展开 `/*!NNNNN ... */`（去掉标记与版本号，保留内容）；
  普通 `/* ... */` 仍然跳过。`SELECT 1 /*!50000 INTO OUTFILE ...*/` 必须判危险

### 输入框宽度溢出
- 现象：textarea `width:100%` + 左右 padding 后比容器宽 20px
- 解法：`box-sizing: border-box`；用 CDP 在 1600/960 视口断言
  `clientWidth === scrollWidth` 且 `documentElement` 无横向溢出

## 七、前端验证 / 测试环境

### headless Chrome 下 Naive UI 弹窗节点不会卸载
- 现象：脚本点击“取消”后 `.n-dialog` 仍留在 DOM（computed `opacity: 0`），
  等待节点消失会超时；后续操作容易点到过渡中的旧弹窗
- 根因：headless 没有可见渲染帧，`fade-in-scale-up-transition-leave-*` 的
  `transitionend` 不触发，Vue 不会卸载节点；真实浏览器正常
- 解法：不要以“节点消失”作为关闭条件。用 `WeakSet` 标记已点击的弹窗，
  每次只取新出现的 `.n-dialog`；判断逻辑正确性时看 pending/页码等业务状态

### WSL 下补装前端平台二进制时目录冲突
- 现象：`npm i --no-save @rollup/rollup-linux-x64-gnu@x` 报 `ENOENT mkdir`；
  同一路径 `ls` 看不到、`mkdir` 却报 `File exists`，`rm -rf` 报 `Is a directory`
- 根因：`/mnt/d`（Windows 盘）上 npm 失败留下的目录项在 DrvFs 中状态不一致
- 解法：`npm pack` 下载后 tar 解包，放到包内私有
  `node_modules/rollup/node_modules/@rollup/rollup-linux-x64-gnu`（Node 解析优先命中）；
  esbuild 同理放 `node_modules/esbuild/node_modules/@esbuild/linux-x64`
