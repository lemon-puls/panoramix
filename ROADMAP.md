# ROADMAP —— 里程碑与验收标准

> 2026-11-01 与用户逐项拷问后定稿（设计树已闭合，无静默假设）。
> 节奏：业余时间 + AI 辅助编码，预估 v0.1 约 5–7 周。

## 版本策略

- SemVer 0.x；每个里程碑完成即打 tag、发 Release（GitHub Releases + Gitee 镜像说明）
- v0.1 产物形态：Windows 便携版（单 `Panoramix.exe` + zip），不做安装器
- 每个里程碑单独可用、可发布

---

## v0.1 —— MVP：连接管理 + SSH 终端 + SFTP

### M1 骨架（存储 + 连接管理）

- [ ] Wails v2 + Vue3 + TS + Naive UI + Pinia + vue-i18n 跑通，深色主题，桥接自检
- [ ] SQLite（modernc.org/sqlite）建库与迁移：groups / hosts / snippets / settings
- [ ] keyring 封装（zalando/go-keyring），密码/passphrase 不落库
- [ ] 连接分组树：增删改、排序、父子层级
- [ ] 连接表单：名称/主机/端口/用户名/认证方式（密码、私钥+passphrase、keyboard-interactive）/初始命令/keepalive 秒数/自动重连开关
- [ ] 搜索过滤、克隆、最近连接列表

### M2 终端

- [ ] SSH 连接：三认证方式，密码错误重试提示
- [ ] TOFU：首连指纹弹窗，known_hosts 以 OpenSSH 兼容格式存储，指纹变更红色警告拦截
- [ ] xterm.js 多标签独立会话；WebGL 渲染 + fit + search
- [ ] 验收用例：htop 交互无错位、vim 滚动 200 行文件无残影、tmux 分屏正常、中文宽字符对齐正确
- [ ] 智能剪贴板（有选中=复制/无选中=SIGINT，`Ctrl+Shift+C/V` 恒定）、`Ctrl+滚轮` 字号、`Ctrl+F` 搜索、右键菜单
- [ ] keepalive 默认 30s（可按连接配置）；断线标签置灰 + 自动重连（可关）
- [ ] 会话日志（可选，按天落文件，含时间戳）；初始命令按序执行
- [ ] Snippets 侧栏：CRUD、点击发送、`{host}` 占位符

### M3 传输

- [ ] 远程文件面板：目录树 + 列表 + 路径栏 + 右键（下载/删除/重命名/新建目录）
- [ ] 下载走系统"另存为"；上传支持拖拽
- [ ] 传输队列：并发默认 3、进度/速度/取消/失败重试（架构按"可暂停可恢复"设计，功能后开）
- [ ] 覆盖策略弹窗：覆盖/跳过/重命名，支持"全部应用"
- [ ] 目录递归、size 校验

### M4 发布

- [ ] Xshell `.xsh` 批量导入（仅非敏感字段：主机/端口/用户名/密钥路径；**密码不可迁移，
      导入页与 README 必须明示**）
- [ ] 日志脱敏断言测试；`go vet` / `go test` / `GOOS=windows go build` 全绿
- [ ] GitHub Actions：test + Windows build + Release 草稿
- [ ] README 完善、Gitee 镜像说明、AI 生成 Logo 初稿
- [ ] **v0.1 发布**

---

## v0.2 —— 系统监控

- 采集：经独立 SSH channel 读 `/proc` + `/sys`（CPU/负载/内存+swap/磁盘挂载点+IO/网络速率+流量/Top 进程表）
- GPU：`nvidia-smi --query-gpu=csv` 探测，检测不到则隐藏整个卡片
- 采样：默认 2s 可配；失败静默降级（回退 `top`/`df` 解析），不报错刷屏
- 呈现：uPlot 实时曲线 + 内存环形缓冲（最近 1 小时，断开即弃；不落盘）
- ✅ 系统监控（`/proc` 采集、GPU 探测、ECharts 实时曲线、2×2 指标卡）
- ✅ FinalShell 导入（conn/*_connect_config.json，仅非敏感字段）
- ✅ SSH agent 认证（Unix SSH_AUTH_SOCK / Windows OpenSSH agent 命名管道）
- ✅ 跳板链 ProxyJump（最多 3 跳，逐跳 TOFU 校验，环检测）
- ✅ SFTP 暂停/恢复 + 全局限速
- ✅ 远程编辑（下载 → 系统编辑器 → 保存自动回传，冲突检测）
- ✅ en-US 英文界面 + 语言切换
- ✅ expect 式登录脚本（正则匹配输出并自动应答）
- ✅ 终端分屏（标签内多 pane 平铺，左右/上下切换）
- ✅ 双面板（本地/远程）+ 拖拽互传
- ✅ 终端 cwd 联动（OSC 7 + 可选 bash 目录同步注入）
- ✅ rz/sz 智能接管（终端内输入 rz/sz 时自动改用内置 SFTP，带进度/续传/冲突策略；
  不实现 ZMODEM 协议本身——协议流与终端输出混杂、需带超时 Reader 桥接 PTY 事件流，
  可靠性与测试成本高，而 SFTP 已覆盖同等目标且体验更好）

## v0.3 —— 数据库与 Redis

- ✅ MySQL（HeidiSQL 式多标签工作区）：
  · 连接管理（keychain 存密码）、库/表树（information_schema 行数估算）
  · **数据标签**（双击表）：默认 1000 条、WHERE 片段筛选、服务端排序、
    分页（100/500/1000/5000）、导出 CSV
  · **查询标签**（＋新建，可多开）：SQL 编辑器、Ctrl+Enter 执行、结果网格
    （本地排序/复制/导出）、查询历史
  · **结构标签**（单击表）：列定义与 DDL
  · **不自动 COUNT**（大表会卡死）
- ✅ Redis：SCAN 渐进（禁 `KEYS *`）、**键树按 `:` 分层**（树形/平铺切换）、
  TTL 查看/修改、string/hash/list/set/zset 查看器、string 值编辑、删键、大 value 截断
- 待办：SQL 编辑器语法高亮与补全（CodeMirror 6，依赖安装受 WSL/Windows 共用
  node_modules 阻塞）、结果分页与数据编辑（v0.4）、二进制/JSON 格式化视图、
  Redis stream 类型、监控历史落盘（YAGNI，按需）

## v0.3.1 —— AI SQL 助手（2026-11-04 插入）

> 设计见 `docs/design/001-next-iteration.md`，评审见 `docs/design/001-review.md`。
> 在 v0.3 与 v0.4 之间插入；K3s 时间点顺延约 3–4 周。

- ✅ 三种模式共用一套 provider / 流式输出 / 插入逻辑：**生成、解释、修复**
- ✅ Provider：本地 Ollama / LM Studio（默认）与 OpenAI-compatible（云端需显式授权）
- ✅ 两阶段协议：`AIPrepare` 构建并缓存 payload 返回预览；`AIGenerate` 只发送缓存原文
  （单次使用、5 分钟过期），保证“预览 = 实发”
- ✅ 隐私：只发 schema 元数据（SHOW CREATE TABLE 脱敏）、库名、MySQL 版本、问题；
  不发任何行数据/结果集/查询历史/连接主机/用户名/密码；视图排除在上下文外
- ✅ API key 存 keychain，按规范化 endpoint 绑定；云端授权按 endpoint 记录；
  切换 endpoint 删除旧 key、要求重新授权
- ✅ HTTP 客户端收紧：禁止重定向、禁止 URL userinfo、响应体 2MB 上限、
  连接/首包/总超时分离、禁用环境代理
- ✅ DDL 脱敏（DEFINER/AUTO_INCREMENT/版本注释）；修正视图 SHOW CREATE 解析；
  收紧 `isQuery`（WITH ... UPDATE、SELECT ... INTO OUTFILE）
- ✅ SQL 静态判定：多语句、写操作、危险结构、未知表；写操作仅允许人工确认后插入
- ✅ 全局并发 1（可配 1–2）、取消、超时、错误映射、流式 `ai:delta` / `ai:status` 事件
- ✅ 确定性测试：httptest 假 provider、payload 快照、重定向/超时/取消/并发/授权绑定
- ✅ 前端：查询标签 AI 侧栏（320px）、配置弹窗、首次发送预览、云端授权弹窗、
  插入编辑器（保留 undo）、zh-CN / en-US

## v0.3.2 —— 数据标签增强（计划）

- 列显示/隐藏（按连接+库+表持久化）、多条件筛选、单元格/列右键快捷筛选
- 服务端过滤/排序/分页；不做 OR/嵌套条件、不做保存筛选方案

## v0.4 —— K3s/K8s

- 接入：**SSH 自动发现为主**（检测 `/etc/rancher/k3s/k3s.yaml`，经 SSH 隧道转换为可用 kubeconfig），手动导入 kubeconfig 为辅
- 范围：NS/Workload/Pod/Event 浏览 + 日志流 + Pod 终端；Pod/Event 走 watch，其余轮询
- YAML 只读查看；全局"所有主机总览"面板（仅对在线会话采样）

## v0.5 —— YAML 编辑 + AI 运维（届时另开设计轮）

- YAML 编辑 + apply（diff 预览确认）；可编辑结果网格（DML 生成预览）
- AI 运维：终端**用户选中文本**的报错解释与命令建议（只插入、不回车）；复用 v0.3.1
  的 provider / keychain / 预览与脱敏基础设施。SQL 助手已在 v0.3.1 落地
- 届时重新拷问：模型接入方式、隐私边界、成本

## Backlog（无排期，按社区反馈）

标签 tear-out 多窗口、多主机同步输入、PostgreSQL、Redis stream 查看器、macOS/Linux 产物、
文档站、自动更新器、v1.0 时代码签名证书、遥测（届时仅可关闭的匿名崩溃上报）、
连接配置加密导出/备份
