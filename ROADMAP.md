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
- 待办：rz/sz（ZMODEM）

## v0.3 —— 数据库与 Redis

- MySQL：连接管理 + SQL 编辑器（CodeMirror 6，表结构喂补全）+ 只读结果网格
  （LIMIT/OFFSET 分页 1000 行虚拟滚动，**不自动 COUNT**，复制为 CSV/JSON/INSERT、导出）
- Redis：GUI 键管理 —— SCAN 渐进（禁 `KEYS *`）、键树按 `:` 分层、TTL 查看/修改、
  string/hash/list/set/zset 查看器、二进制十六进制查看、JSON 自动格式化、大 value 截断加载
- 监控历史落盘视社区需求决定（YAGNI，默认不做）

## v0.4 —— K3s/K8s

- 接入：**SSH 自动发现为主**（检测 `/etc/rancher/k3s/k3s.yaml`，经 SSH 隧道转换为可用 kubeconfig），手动导入 kubeconfig 为辅
- 范围：NS/Workload/Pod/Event 浏览 + 日志流 + Pod 终端；Pod/Event 走 watch，其余轮询
- YAML 只读查看；全局"所有主机总览"面板（仅对在线会话采样）

## v0.5 —— YAML 编辑 + AI 运维（届时另开设计轮）

- YAML 编辑 + apply（diff 预览确认）；可编辑结果网格（DML 生成预览）
- AI 运维：自然语言查日志/报错、命令生成；数据模型从 v0.1 的 snippets 表演进
- 届时重新拷问：模型接入方式、隐私边界、成本

## Backlog（无排期，按社区反馈）

标签 tear-out 多窗口、多主机同步输入、PostgreSQL、Redis stream 查看器、macOS/Linux 产物、
文档站、自动更新器、v1.0 时代码签名证书、遥测（届时仅可关闭的匿名崩溃上报）、
连接配置加密导出/备份
