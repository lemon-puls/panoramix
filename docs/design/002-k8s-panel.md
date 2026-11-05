# K8s / K3s 面板 —— v0.4 设计提案

> 文档编号：002-k8s-panel  
> 日期：2026-11-05  
> 作者：designer  
> 状态：**暂缓（2026-11-05）**——总监打回（技术方案有硬伤），用户决定先做 v0.3.2；恢复时须先完成 spike 与 10 个阻塞项修订

## 用户决策（2026-11-05）

| 决策项 | 结论 |
| --- | --- |
| 本轮优先级 | **先做 v0.3.2 数据标签增强**，K8s 顺延 |
| 是否先 spike | 恢复 K8s 前**必须先做技术 spike**（总监要求的隧道 + SPDY exec 验证） |
| 范围拆分 | **拆两个里程碑**：v0.4.0（集群发现+浏览+日志+YAML 只读）/ v0.4.1（Pod 终端+事件流） |
| 免密 sudo | **提供，但必须用户明确点击、展示将执行的命令、二次确认**（不记住授权） |
| 自动探测 | **自动探测但不自动导入**：发现后在树里提示可加入，需用户点击导入 |

> 恢复 K8s 时：先读 `002-review.md` 的 10 个阻塞项（SPDY Executor 不走 Dial、transport.DialHolder 缓存泄漏、Proxy=nil 无效、kubeconfig 任意读等），并先跑 spike。

> 状态：**待评审 / 待用户确认**  
> 对应版本：v0.4  
> 调研时间：2026-11-05，结论均附公开来源

## 结论先行

1. **K8s 是顶部栏第三个一级视图**，与「终端 / 数据库」并列，不塞进 264px 的连接树或终端标签。理由是资源浏览需要完整的宽度与高度，而且 `DBView` 已有「一级视图 + 左树 + 右工作区」的先例。
2. **差异化入口留在连接树**：SSH 主机右键「发现 Kubernetes 集群」；连上主机后后台自动探测一次，发现 K3s 时在主机节点显示 `K8s` 徽标并给一次性提示。用户点一下就从「这台机器」进入「这台机器上的集群」，不需要找 kubeconfig。
3. **K3s 的 127.0.0.1 证书问题用 SSH 隧道解决，不改远端证书 SAN、不落地重写后的 kubeconfig**：client-go 的 `rest.Config` 使用自定义 `Dialer`，把 `127.0.0.1:6443` 的 TCP 连接经 SSH channel 打到远端。远端 k3s 默认证书 SAN 已包含 `127.0.0.1`，因此 TLS 校验可正常通过。
4. **kubeconfig 视为最高级别凭据**：明文只在 Go 内存中存在；AES-256-GCM 加密后落 SQLite，每个集群的数据密钥存 OS keychain；永不下发前端、永不写日志、不提供「导出 kubeconfig」。keychain 不可用时降级为「仅本次会话可用」，不落明文。
5. **信息架构取 k9s 的「资源类型 → 列表 → 详情」、Lens 的三栏与 Pod 动作菜单、Headlamp 的 RBAC 显式降级**；放弃 k9s 的命令模式与 TUI 密度，放弃 Lens 的重型 Electron 与付费墙，放弃 Headlamp 的 web/插件体系。
6. **v0.4 严格只读**：浏览、日志、终端，没有删除/扩缩容/重启/编辑 YAML/端口转发。ROADMAP 里的「全局所有主机总览」建议顺延到 v0.4.1，理由见「不做什么」。
7. 最大的待确认项：一级视图方案、默认命名空间（建议「全部命名空间」）、自动探测的打扰程度、全局总览是否顺延。

---

## 用户场景

**谁**：在单台或少量服务器上跑 K3s 的独立开发者、小团队运维、homelab 用户；日常用 SSH 登机器，`kubectl` 只是偶尔用。

**典型场景 A：连上机器，想看看上面跑了什么**

> 用户连上一台云服务器，只想确认「上次部署的 API 服务起来了没有」。现在要：找 kubeconfig → 改 `server: 127.0.0.1:6443` 或开 SSH 本地转发 → 配 `KUBECONFIG` → 装 k9s/Lens → 导入。每一步都可能卡住，尤其是 k3s 自签证书的 SAN 问题。用户其实只知道「这台机器的 22 端口能登」。

**典型场景 B：Pod 起不来，要看日志和事件**

> `kubectl get pods` 显示 `CrashLoopBackOff`，用户要 `describe` 看事件、`logs --previous` 看上一次性为什么挂、再 `exec` 进去看配置。四条命令、三次复制 Pod 名；在 GUI 里则要在多个页面之间跳。

**典型场景 C：多命名空间排查**

> 一个 k3s 上跑了 `default`、`monitoring`、`app-prod` 三个命名空间。用户要跨命名空间看哪个 Pod 在重启，再切到对应命名空间看事件。每次 `kubens`/`-n` 都在打断思路。

**现在的痛**：

1. 接入成本高：kubeconfig 的路径、server 地址改写、证书 SAN、context 切换，对非 K8s 专家是四道坎。
2. k3s 的 `k3s.yaml` 常是 `0600 root`，普通用户读不到；很多人因此放弃。
3. 工具割裂：SSH 在一个客户端、K8s 在另一个客户端，主机和集群的对应关系靠人脑记。
4. 日志/终端/事件分散在 CLI 的多条命令里，复制 Pod 名是高频动作。
5. 云端 GUI（Lens/Headlamp）要么重、要么需要把 kubeconfig 交给另一个应用；用户对自己的凭据去向不放心。

---

## 竞品对比

> 以下为 2026-11-05 实际检索到的公开资料；「取/弃」是我们的判断。

### k9s（终端 UI）

| 维度 | 具体做法 | 来源 |
| --- | --- | --- |
| 信息架构 | 顶部是 context / namespace 头部 + crumb，主体是资源列表，底部是随资源变化的快捷键提示；`Enter` 下钻（Pods → Containers），`Esc` 返回 | [k9s 命令表](https://k9scli.io/topics/commands/)、[k9s README](https://github.com/derailed/k9s)、[Kubezilla cheatsheet](https://kubezilla.io/k9s-cheatsheet/) |
| 资源切换 | `:` 命令模式（`:pod`、`:deploy`、`:pod ns-x`、`:pod @ctx`），支持别名、自动补全；`/` 正则过滤；`?` 查看当前视图全部快捷键 | [k9s 命令表](https://k9scli.io/topics/commands/)、[ReleaseRun 参考](https://releaserun.com/k9s-reference/) |
| 命名空间 | 头部数字键 `0–9` 快速切命名空间，`0` 通常是全部；`:ns all`、`:ns prod` | [k9s 参考](https://releaserun.com/k9s-reference/)、[TUI case study](https://github.com/wimpysworld/nix-config/blob/5d6e2c75b6701394949cd24fc304396991ed3b23/home-manager/_mixins/agentic/assistants/skills/tui-design/references/exemplar-apps.md) |
| Pod 操作 | `l` 日志、`s` shell、`d` describe、`y` YAML、`e` 编辑、`Shift+F` 端口转发、`Ctrl+D` 删除；只读模式 `--readonly` 可屏蔽修改类命令 | [k9s 命令表](https://k9scli.io/topics/commands/)、[k9s 配置](https://k9scli.io/topics/config/) |
| 日志 | `Ctrl+T` 时间戳、`Ctrl+W` 自动换行、`Ctrl+S` 保存、`Ctrl+L` 清屏；多容器有容器选择器 | [k9s 命令表](https://k9scli.io/topics/commands/)、[Kubezilla cheatsheet](https://kubezilla.io/k9s-cheatsheet/) |
| 安全 | `--readonly` / `readOnly: true` 只隐藏本地修改命令，官方与社区都强调它**不能替代 RBAC** | [k9s 配置](https://k9scli.io/topics/config/)、[hoop.dev 配置说明](https://hoop.dev/blog/how-to-configure-configure-k9s) |

**取**：资源类型导航、按资源变化的快捷键提示、Pod 的「日志 / shell / YAML / 事件」动作、日志的时间戳/换行/保存、只读优先。
**弃**：TUI 的高密度与命令模式（`:pod`）；GUI 用户不应该先背一套语法。我们保留少量单键快捷键，但所有操作都有可点击入口。

### Lens / OpenLens（桌面 GUI）

> OpenLens 是 Lens 的旧开源版本（已归档）；当前 Lens Desktop 由 Mirantis 以商业产品维护。我们只借鉴交互，不引入其 Electron 实现或授权模式。

| 维度 | 具体做法 | 来源 |
| --- | --- | --- |
| 集群入口 | Catalog → Clusters，自动扫描 `~/.kube/config`，也支持手动 Add Cluster；多集群列表与快速切换 | [Lens 指南](https://lenshq.io/blog/simplify-kubernetes-monitoring-and-management-with-lens-desktop)、[Perficient 概览](https://blogs.perficient.com/kube-lens/) |
| 布局 | 左侧资源分类（Workloads → Pods/Deployments/StatefulSets/Jobs/CronJobs；Network；Config；Storage；Namespaces；Events），中间可排序/过滤的列表，右侧详情；命名空间是全局下拉 | [Lens 三栏笔记](https://notes.darshanraul.me/Kubernetes/guides/tools/lens)、[hashhackers 指南](https://blog.hashhackers.com/blog/lens-kubernetes-guide/) |
| Pod 动作 | 三点菜单：Details、Attach、Shell、Evict、Share、Get logs、Edit、Delete；详情内可直接看日志、事件、YAML | [Perficient 概览](https://blogs.perficient.com/kube-lens/)、[Mirantis 指南](https://www.mirantis.com/blog/simplify-kubernetes-monitoring-and-management-with-lens-desktop/) |
| 日志 | 实时流；过滤开关（`/`）、时间戳/日期、上一次性容器、自动换行、下载、时间范围、跳到最新事件、跳到控制该 Pod 的工作负载 | [Lens Log options 官方文档](https://docs.lenshq.io/k8slens/using-lens/log-options/) |
| 终端 | 内置「Smart Terminal」，自动绑定当前集群/context；Pod shell（exec）与 attach 分开 | [Lens 指南](https://lenshq.io/blog/simplify-kubernetes-monitoring-and-management-with-lens-desktop)、[Mirantis 指南](https://www.mirantis.com/blog/simplify-kubernetes-monitoring-and-management-with-lens-desktop/) |
| 许可 | 当前 Lens Desktop 为商业产品，免费 Personal 含多集群、日志、终端；团队/企业功能付费 | [Lens 定价](https://lenshq.io/pricing/) |

**取**：三栏布局、Pod 动作菜单、日志选项的完整清单、终端与日志都挂在 Pod 详情里。
**弃**：Electron 体量与商业授权；自动扫描本机 `~/.kube/config` 作为唯一入口（我们的第一入口是 SSH 主机）；Evict/Edit/Delete 等修改动作（v0.4 不做）。

### Headlamp（CNCF / SIG-UI web UI）

| 维度 | 具体做法 | 来源 |
| --- | --- | --- |
| 定位 | Kubernetes 官方在 Dashboard 归档后推荐的替代品，CNCF Sandbox、SIG-UI 维护；桌面版与 in-cluster 两种形态 | [Kubernetes 官方博客](https://kubernetes.io/blog/2026/06/01/dashboard-to-headlamp/)、[Headlamp GitHub](https://github.com/kubernetes-sigs/headlamp) |
| 资源浏览 | 左侧按 Workloads / Network / Config 分类，命名空间导航，多集群；支持 Projects（跨命名空间/集群的应用分组） | [Headlamp Projects 文档](https://headlamp.dev/docs/latest/learn/projects/) |
| RBAC | 控件跟随当前身份动态显示：无权限时隐藏 delete/update/scale/restart；日志与 exec 同样受 RBAC 限制 | [Headlamp GitHub](https://github.com/kubernetes-sigs/headlamp)、[Dashboard→Headlamp 迁移文](https://v1-36.docs.kubernetes.io/blog/2026/06/01/dashboard-to-headlamp/) |
| 日志/终端 | 详情页 Logs 标签实时流、多容器切换与过滤；Pod 动作里开浏览器内 exec | [Headlamp GitHub](https://github.com/kubernetes-sigs/headlamp)、[DeepWiki 概览](https://deepwiki.com/kubernetes-sigs/headlamp) |
| 权限边角 | 用户没有 cluster-wide `list namespaces` 权限时，侧栏命名空间来源需要可配置（OpenShift Projects 等），已有 issue | [Headlamp issue #7326](https://github.com/kubernetes-sigs/headlamp/issues/7326) |
| 插件 | 插件可注册路由、侧栏项、详情区块；插件市场丰富 | [Headlamp GitHub](https://github.com/kubernetes-sigs/headlamp) |

**取**：RBAC 失败必须显式解释而不是报「连接失败」；命名空间列表失败要有降级路径；日志/终端受同一套权限约束。
**弃**：web/in-cluster 形态、插件市场、Projects（v0.4 不做应用分组）。我们的单二进制 + 零遥测定位不适合插件运行时。

### kubectl / kubectx / kubens（CLI 习惯）

| 维度 | 具体做法 | 来源 |
| --- | --- | --- |
| 心智模型 | `kubectl get/describe/logs/exec` 动词 + 资源名；`-n` 命名空间、`-A` 全部、`-l` 标签选择器 | [kubectl logs 参考](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_logs/) |
| 日志参数 | `-f` 跟随、`--tail` 行数、`--timestamps`、`-p/--previous`、`-c` 容器、`--all-containers`、`--prefix` | [kubectl logs 参考](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_logs/) |
| 上下文切换 | `kubectx <name>`、`kubens <name>`；`-` 回到上一个 context/namespace；fzf 交互选择；`kubens -c` 查看当前 | [kubectx README](https://github.com/ahmetb/kubectx) |
| 提示符 | 常与 `kube-ps1` 搭配，持续显示 context/namespace，降低「在 prod 上敲错」的概率 | [kubectx README](https://github.com/ahmetb/kubectx) |

**取**：命名空间切换必须是一等公民，并「记住上次选择」；当前集群/命名空间必须始终可见（避免误操作）；日志参数按 kubectl 语义命名。
**弃**：把 context 当成用户要管理的对象；我们一个集群条目绑定一个 context，界面上只显示用户起的集群名 + 来源主机。

### 信息架构的三种模式与我们的选择

| 模式 | 代表 | 组织方式 | 代价 |
| --- | --- | --- | --- |
| 键盘优先、类型优先 | k9s | 顶部 context/namespace + 当前资源类型列表；`Enter` 下钻到容器/Pod 详情；底部随资源变化的快捷键 | 必须记命令与快捷键；没有全局树，跨类型跳转靠 `:` |
| 分类侧栏 + 列表/详情 | Lens / Headlamp | 左侧固定分类（Workloads/Network/Config/Events…）+ 全局命名空间下拉；中间列表；右侧/详情页 | 分类多、层级浅；命名空间与对象关系靠下拉，容易忘记当前作用域 |
| 完整资源树 | 部分 web dashboard | Namespace → 类型 → 对象逐层展开 | 点击次数多，大集群树加载慢；不适合作为主浏览方式 |

**我们的选择**：`集群（按来源主机）→ 命名空间选择器 → 资源类型 → 列表 → 详情标签`。集群是入口，命名空间是一等过滤器，资源类型是浅层列表（不展开 NS→类型→对象的多级树）。理由是：单机 K3s 用户通常集群数少、命名空间数少，深层树只会增加点击；而 k9s 的命令模式对 GUI 用户学习成本过高。日志与终端都放在 **Pod 详情标签的子标签**里（Lens/Headlamp 的做法），而不是 k9s 的全屏日志页；这样看日志时仍能保留 Pod 的上下文，多个 Pod 的日志可通过多个详情标签并存。

### 差异化结论

- k9s 快，但要求用户先学一套命令；Lens 全，但重且商业；Headlamp 是 web，凭据要交给另一个服务。
- Panoramix 的独特路径是：**用户已经信任这台 SSH 主机，Panoramix 就在这台主机上发现并连接集群**；kubeconfig 不出本机、不上传、不导出，隧道由已建立的 SSH 信任链承载。
- 在自动发现路径下，我们要做到「从连上主机到看到 Pod 列表」只需「查看发现结果 → 添加」两次点击；k9s/Lens 都做不到这一点。

---

## 信息架构

### 一级导航：第三个视图

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ ◆ Panoramix      [ 终端 ] [ 数据库 ] [ Kubernetes ]                    ⚙    │ 52px
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   KubernetesView（全屏，与 DBView 同级；不显示终端主机侧栏）                   │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

- `mainView` 类型从 `'terminal' | 'db'` 扩展为 `'terminal' | 'db' | 'k8s'`。
- 顶部栏第三个按钮图标用六边形/舵轮（内联 SVG，与现有 14px 线性图标一致）。
- 切走 K8s 视图不会销毁终端标签/数据库标签；K8s 工作区状态在内存中保留（见「连接生命周期」）。
- **不采用**「K8s 集成在连接树里、点主机展开集群、在终端区域开 K8s 标签」的方案：
  - 资源表 + 详情 + 日志需要至少 1000px 宽；终端区被 264px 侧栏和右侧面板挤压。
  - 现有终端标签模型是「一个标签 = 一个或多个 SSH pane」，塞入非 PTY 视图会污染 `terminals` store。
  - DB 已经用一级视图解决了同类问题，保持一致。

### K8s 视图总布局

```
┌────────────────────────────┬─────────────────────────────────────────────────┐
│ CLUSTERS            ＋  ⇩  │  k3s-prod · home-server        ● 就绪  v1.30.2 │
│                            │  [ 命名空间: 全部 ▾ ]        [刷新] [快捷键帮助]│
│ ▾ home-server · root@10..  │ ┌ tabs ────────────────────────────────────────┐ │
│   ● k3s-prod      v1.30.2  │ │ [Pods · 全部命名空间] [pod/api-7f9c ×] ...   │ │
│   ○ k3s-dev       已暂停   │ ├──────────────────────────────────────────────┤ │
│ ▾ 导入                      │ │ [筛选] name / labels…       [!] 仅看异常 2    │ │
│   ● prod-cluster            │ │ ┌──────────────────────────────────────────┐ │ │
│                            │ │ │ NAME        READY  STATUS   RESTARTS AGE │ │ │
│ ── 资源 ──────────────     │ │ │ api-7f9c…   1/1    Running  0        3d  │ │ │
│   Namespaces          4    │ │ │ api-8d2b…   0/1    Crash…   7        12m │ │ │
│   Workloads           6    │ │ │ ...                                      │ │ │
│   Pods               12    │ │ └──────────────────────────────────────────┘ │ │
│   Events          5 (⚠2)   │ └──────────────────────────────────────────────┘ │
│                            │                                                 │
│ [从 SSH 主机发现…]         │                                                 │
│ [导入 kubeconfig…]         │                                                 │
└────────────────────────────┴─────────────────────────────────────────────────┘
  264px                                                  剩余宽度
```

**左侧栏分两段**（类似终端视图「连接树 + Snippets」）：

1. **CLUSTERS（集群）**：按来源主机分组。主机名一行，下面挂该主机发现的集群；手动导入的集群在「导入」组。每行显示状态点、名称、版本或状态、hover 操作（刷新凭据 / 重命名 / 移除）。顶部 `＋` 菜单：`从 SSH 主机发现…` / `导入 kubeconfig…`；`⇩` 折叠全部。
   - 状态点：`●` 就绪（accent）、`◐` 连接中/降级（warning）、`○` 已暂停/未连接（muted）、`!` 错误（danger）。
   - 单击集群行即连接并激活（集群切换无破坏性，不需要双击确认）。
2. **资源（当前集群）**：`Namespaces`、`Workloads`、`Pods`、`Events`，带数量；Events 额外显示 warning 数。数量**随当前命名空间选择变化**。选中项高亮 `--accent`，与现有树一致。
   - CLUSTERS 区最高占侧栏 45% 并可独立滚动；资源区固定在下方，避免集群多时把资源区挤出屏幕。
   - `Workloads` 不展开子类型；右侧表格顶部用 kind chips 过滤（All / Deployments / StatefulSets / DaemonSets / Jobs / CronJobs），避免树太深。
   - 未选中集群时，资源区显示引导文案而不是空白。

**右侧工作区**：

- 顶部一条上下文栏：`集群名 · 来源主机`、连接状态、版本、命名空间选择器（可搜索；权限受限时可手动输入）、刷新、快捷键帮助。
- 「刷新」= 强制重列当前资源（Pods/Events 重建 watch；Workloads/Namespaces 立即轮询一次）；watch 本身无需手动刷新。
- 下方是固定标签栏：
  - 第一个标签是**浏览器标签**（固定、不可关闭），标题随资源类型/命名空间变化，例如 `Pods · 全部命名空间`、`Workloads · app-prod`。切换资源类型或命名空间时**原地更新并激活**这个标签，不新开标签（与 DB 的「表」固定标签一致）。
  - 之后是**详情标签**（可关闭）：`pod/api-7f9c`、`deploy/api`、`ns/app-prod`。同一对象重复打开只激活已有标签。
  - 标签栏不提供 `＋`：K8s 没有「新建」语义；只有浏览器标签与详情标签。

**详情标签内部子标签**：

| 对象 | 子标签 |
| --- | --- |
| Pod | 概览 / 日志 / 终端 / YAML / 事件 |
| Workload | 概览 / Pods / YAML / 事件 |
| Namespace | 概览 / Pods / YAML |
| Event | 行内展开详情，不单独开标签 |

- 子标签用 `v-show` 保持挂载，避免切回日志时丢失滚动位置、切回终端时重建 xterm；终端从隐藏变为可见时必须重新 `fit()` 并上报 resize，否则会出现 0×0 或行列错位。
- 终端与日志在标签失焦时保持连接（有上限，见「并发上限」）；关闭主标签即停止。

### 资源列表与详情

**通用表格**：

- 固定表头，行高 32px，虚拟滚动（目标 2000 行不卡顿）。
- 列可排序：Name、Namespace、Age、Restarts 等；排序状态只存前端。
- 筛选框：对 `name / namespace / labels` 做不区分大小写的子串匹配；不做 label selector 语法（v0.4）。
- `仅看异常`（Pods）：非 Running/Ready、Restarts > 0、或状态为 CrashLoopBackOff/Error/Pending/Terminating；`仅看 Warning`（Events）单独一个开关。
- 状态徽标：Running/Ready 用 `--accent`，Pending/Warning 用 `--warning`，CrashLoopBackOff/Error 用 `--danger`，Terminating/Unknown 用 `--text-muted`。
- Age 列每 10 秒刷新一次相对时间；hover 显示绝对时间。
- 单击选中（供键盘与右键操作），双击或 `Enter` 打开详情；与连接树「双击连接」的习惯一致，避免在列表里误触打开大量标签。右键菜单：`查看详情`、`查看日志`、`打开终端`（仅 Pod）、`查看 YAML`、`复制名称`。
- 空白状态、权限状态、加载状态分别设计（见「空状态与错误」）。

**Pods 表列**：`Name | Namespace（仅在全部命名空间时显示） | Ready | Status | Restarts | Age | IP | Node`。
**状态归一化**（避免 CrashLoopBackOff 显示成 Running）：`deletionTimestamp` → Terminating；容器 `waiting.reason`（CrashLoopBackOff/ImagePullBackOff/ErrImagePull 等）优先；init 容器未完成 → Init:x/y；否则用 `phase`（Running/Pending/Succeeded/Failed/Unknown）。
**Workloads 表列**：`Name | Kind | Namespace | Ready | Up-to-date | Available | Age`。
**Workloads 归一化**：Deployment/StatefulSet 用 `readyReplicas/desired`；DaemonSet 用 `numberReady/desiredNumberScheduled`；Job 用 `succeeded/completions`；CronJob 显示 `Suspend` 与最近调度时间。不可用字段显示 `—`，不伪造 0。
**Events 表列**：`Type | Reason | Object | Message | Count | Last Seen | Namespace`；Message 单行省略，hover 显示全文，点击展开行内详情；`⚠ 仅看 Warning` 开关。
**Namespaces 表列**：`Name | Status | Pods | Age`；双击命名空间 → 把命名空间选择器切到该 NS 并激活 `Pods` 浏览器标签。

### Pod 详情线框

```
┌ pod/api-7f9c9d8b4-x2k7p ────────────────────────────────────── × ┐
│ [概览] [日志] [终端] [YAML] [事件]                                │
├──────────────────────────────────────────────────────────────────┤
│ 状态  ● Running        Restarts 0        Age 3d 4h               │
│ Namespace app-prod     Node k3s-node-1   Pod IP 10.42.0.18       │
│ QoS Burstable          Start 2026-11-02 09:14:22                 │
│ Owner Deployment/api (via ReplicaSet/api-7f9c9d8b4)               │
│                                                                  │
│ 容器                                                             │
│ ┌ api ─────────────────────────────────────────────────────────┐ │
│ │ Image  ghcr.io/acme/api:1.8.2   ● Ready   Restarts 0         │ │
│ │ State  Running since 2026-11-02 09:14:25                      │ │
│ └───────────────────────────────────────────────────────────────┘ │
│ ┌ sidecar ─────────────────────────────────────────────────────┐ │
│ │ Image  envoyproxy/envoy:v1.31  ● Ready   Restarts 0           │ │
│ └───────────────────────────────────────────────────────────────┘ │
│                                                                  │
│ Conditions: Ready=True  ContainersReady=True  PodScheduled=True  │
│ Labels: app=api  tier=backend  version=1.8.2                     │
│ [复制 YAML] [查看日志] [打开终端]                                 │
└──────────────────────────────────────────────────────────────────┘
```

- 「查看日志」「打开终端」会切到对应子标签，并带上当前选中的容器。
- 事件子标签按 `involvedObject.uid` 过滤，展示该 Pod 相关事件（来自本地 watch 缓存，不额外请求）。
- Owner 链按 `Pod.ownerReferences` 解析：ReplicaSet 从缓存或一次 GET 得到，再显示 `Deployment/<name>`；无 owner（裸 Pod）显示 `<none>`；解析失败只显示直接 owner，不影响详情其余部分。

### 日志子标签线框

```
┌ 日志 ──────────────────────────────────────────────────────────────┐
│ 容器 [api ▾]  [跟随 ●]  行数 [500 ▾]  ☐ 时间戳  ☐ 上一次性  ☐ 换行  │
│ [搜索] 日志…            3/128              [暂停滚动] [清空] [导出]│
├────────────────────────────────────────────────────────────────────┤
│ 2026-11-05T10:12:01Z GET /healthz 200 3ms                          │
│ 2026-11-05T10:12:02Z GET /api/orders 500 41ms                      │
│ ...                                                                │
│                                          ┌──────────────────┐      │
│                                          │ 已暂停自动滚动    │      │
│                                          │ 回到底部 (23 新)  │      │
│                                          └──────────────────┘      │
└────────────────────────────────────────────────────────────────────┘
```

- 日志是**纯文本视图**，不是 xterm。理由：日志需要搜索/过滤/换行/时间戳/暂停滚动/复制/导出，不需要 VT 仿真；用 xterm 反而增加渲染成本且不利于长行。
- 遵循 kubectl 语义：`跟随` = `-f`，`行数` = `--tail`，`时间戳` = `--timestamps`，`上一次性` = `--previous`，`容器` = `-c`。
- `上一次性` 与 `跟随` 互斥：勾选「上一次性」自动关闭并禁用「跟随」，tooltip 说明「上一次容器实例的日志不能实时跟随」。
- 关闭 `跟随` 后，本次流读取完当前内容即结束，工具栏出现「刷新」按钮；再次打开才读取新内容。这避免用户以为「关掉跟随 = 暂停」，实际是快照。
- 切换容器 / 行数 / 时间戳 / 上一次性会**重新打开日志流**（这些是 apiserver 侧的读取参数）；重新打开时保留当前滚动位置不跳回顶部。
- 多容器时必须先选容器；默认选 spec 中的第一个容器，标题显示 `1/2`。
- 缓冲区上限 **10,000 行 / 4MB**，超出丢弃最旧并显示「已丢弃较早日志」；「导出」导出当前缓冲区（点击时弹系统保存对话框）。
- ANSI 转义序列在渲染前剥离（避免出现 `[32m`），v0.4 不做颜色渲染。
- 3 个并发日志流上限；第 4 个提示先关闭一个。

### 终端子标签线框

```
┌ 终端 ──────────────────────────────────────────────────────────────┐
│ 容器 [api ▾]   Shell [/bin/sh ▾]        ● 已连接   [断开]          │
├────────────────────────────────────────────────────────────────────┤
│ 你已进入容器 api-7f9c9d8b4-x2k7p/api · 操作会直接影响该容器          │
│ / # _                                                              │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

- 复用 `@xterm/xterm` + fit / webgl / search 与现有的剪贴板/快捷键行为（`Ctrl+Shift+C/V` 恒定、`Ctrl+F` 搜索、`Ctrl+滚轮` 字号）。
- 不直接复用 `TerminalView.vue`：它和 SSH 会话、文件面板、监控面板、断线重连、会话日志强耦合。新增 `K8sTerminal.vue`，先复制必要行为；如果工程师评估抽取成本低，再把 xterm 初始化抽成 `useXterm` composable。**v0.4 不允许为了复用而重构 SSH 终端。**
- Shell 默认 `/bin/sh`，可选 `bash` / `ash`；失败时显示容器返回的原因，并提示「该镜像可能没有 shell（distroless）」。
- 3 个并发终端上限；关闭标签 / 切换容器 / Pod 删除 / 隧道断开时明确结束态。
- K8s 终端**不写入 SSH 会话日志文件**（避免把容器内敏感输出落盘）；与「会话日志」全局开关隔离。

### 快捷键（表格获得焦点、且不在输入框内时生效）

| 键 | 动作 |
| --- | --- |
| `↑` / `↓` / `j` / `k` | 移动选中行 |
| `Enter` | 打开选中对象详情 |
| `l` | 打开选中 Pod 的日志 |
| `t` | 打开选中 Pod 的终端 |
| `y` | 打开 YAML 子标签 |
| `c` | 复制选中对象名称 |
| `/` | 聚焦筛选框 |
| `Esc` | 清空筛选；再按一次关闭当前详情标签（无未保存修改，v0.4 全只读） |
| `Ctrl+F` | 日志内搜索（日志子标签激活时） |
| `Ctrl+Alt+R` | 刷新当前资源（避免与 WebView 的 `Ctrl+R` 重载冲突） |
| `?` | 打开快捷键帮助 |

所有快捷键都有等价鼠标入口；帮助面板列出当前视图可用的键。

### 固定标签的一致性

- 浏览器标签固定、不可关闭；详情标签可关闭。
- 同一对象只开一个详情标签；重复打开只激活。
- 关闭有活动日志/终端的标签时，先停止后端流，再关闭；不弹「确认关闭」（只读、无数据丢失）。若用户正在终端里执行长命令，标签标题上的小圆点提示「有活动会话」，关闭后会话结束——这是可预期的行为，不阻断。
- 详情标签数量上限 12 个；超出时提示先关闭不用的标签（避免内存与流数量失控）。

### 与「危险操作三步」的关系

v0.4 **没有集群侧的破坏性操作**，因此不引入假的确认流程：

- 不提供删除/扩缩容/重启/编辑；不存在「表单 → 预览 → 确认」的 K8s 资源操作。
- **Pod 终端是唯一的「会改变运行环境」的入口**，但它不是自动执行的批量操作。按现有「AI 生成但永不自动执行」的安全哲学，采用**常驻提示**而不是每次弹窗：终端顶部固定一行「你已进入容器 … · 操作会直接影响该容器」，让用户始终知道自己在哪。
- 本地凭据删除（移除集群 / 删除有集群的主机）是破坏性操作，使用现有 `dialog.warning` 二次确认，并明确列出将删除的集群数量与「需重新发现/导入」。
- 若未来加入重启/删除，必须复用数据库的「危险操作三步」，不能直接调 API。

### 空状态与错误状态

| 状态 | 呈现 |
| --- | --- |
| 未添加集群 | 居中图标 + 「尚未添加 Kubernetes 集群」+ 两个主按钮 + 说明「连接 SSH 主机后会自动检测 K3s（可在设置中关闭）」 |
| 未选择资源类型 | 「从左侧选择一个资源」+ 提示双击资源打开详情 |
| 命名空间无资源 | 「此命名空间没有 Pods」+ 「查看全部命名空间」按钮 |
| 筛选无结果 | 「没有匹配 “x” 的 Pods」+ 「清除筛选」 |
| 无权限（列表级） | 锁形图标 + 「当前凭据无权访问 Pods」+ verb/resource + 「复制错误详情」 |
| 隧道断开 | 顶部黄色横幅 + 重连计数 + 「立即重试」；表格保留旧数据并标「最后更新 x 秒前」 |
| 集群未就绪 | 骨架屏 + 当前步骤（连接 SSH / 建立隧道 / 读取集群）；超过 15 秒给出「查看详情」 |
| 日志等待 | 「等待容器启动…」或「容器已结束，可查看上一次性日志」 |
| 终端连接中 | 居中 spinner + 「正在进入容器 …」；失败后显示原因与「重试」 |

空状态一律遵循现有模式：图标 + 一句主提示 + 一个可执行动作；不出现只有一行灰字的空白页。

---

## SSH 自动发现：完整流程

### 入口与触发

| 入口 | 触发 | 行为 |
| --- | --- | --- |
| 主机右键菜单 | `发现 Kubernetes 集群…` | 若无后台 SSH 会话则建立；打开发现弹窗，展示进度与候选 |
| 连接树节点徽标 | 自动探测发现候选 | 主机行右侧出现 `K8s` 小徽标；未添加 → 打开发现弹窗；已添加 → 切到 K8s 视图并选中该集群；凭据已更新 → 提示刷新 |
| 一次性提示 | 首次发现且未添加 | 非阻塞通知：「在 home-server 发现 K3s 集群」+「查看」；按「主机 + 候选路径集合」去重，只提示一次（未注册候选不读文件，无法用哈希去重） |
| K8s 视图 `＋` | `从 SSH 主机发现…` | 先选主机（可搜索），再走发现弹窗 |
| K8s 视图 `＋` | `导入 kubeconfig…` | 系统文件选择器，走导入预览 |

设置项 `连接时自动探测 K8s`（默认开启）控制自动探测，放在顶部齿轮设置面板中，与「会话日志 / 目录同步 / 限速」并列。关闭后只有手动入口。

### 自动探测做什么、不做什么

**每次 SSH 终端连接成功后**，后台执行一次**只读探测**（独立 channel，固定命令，不阻塞终端）。同一主机在探测结果未过期（60 秒）时复用，不因多开终端重复执行：

探测项（全部固定路径，不拼接用户输入）：

1. `/etc/rancher/k3s/k3s.yaml` — K3s
2. `/etc/rancher/rke2/rke2.yaml` — RKE2（低成本顺手支持）
3. `/etc/kubernetes/admin.conf` — kubeadm 常见位置（通常仅 root 可读）
4. `$HOME/.kube/config` — 常规 kubeconfig
5. `$KUBECONFIG` — 按 `:` 拆分，逐个检查可读性
6. `command -v kubectl` / `command -v k3s` — **仅作信息展示**，数据通路不依赖它们

> 说明：SSH exec 是非交互环境，**不会加载 `.bashrc` / `.zshrc`**；因此只检测登录环境已经导出的 `KUBECONFIG`。不主动执行 login shell（避免启动脚本副作用与注入面），`~/.kube/config` 作为兜底路径覆盖大多数情况。

每个候选返回四种状态：`可读` / `存在但不可读` / `不存在` / `路径无效`。

**隐私边界**：

- 对**尚未添加**的候选，自动探测**只做 `test -r`，不读取文件内容**；用户点开发现弹窗并确认后才读取。
- 对**已添加**的集群，每次主机连接后重新读取源 kubeconfig 并比较 SHA-256（用户此前已授权读取）。哈希变化时在集群行显示「凭据已更新」徽标，点击走「刷新凭据」确认，不自动替换。
- 不执行 `sudo`（除非用户在弹窗里显式点击「尝试免密 sudo 读取」）。
- 不执行 `kubectl` 子命令；所有 K8s 操作走 API。

**实现安全**：探测命令是固定字符串，经 SSH exec 运行；读取文件一律走 SFTP `Open(path)`，绝不把路径拼进 shell。`$KUBECONFIG` 中的路径经 `$HOME` 展开后同样走 SFTP，避免引号/空格注入。

### 发现弹窗

```
┌ 发现 Kubernetes 集群 · home-server (root@10.0.0.5) ─────────────────────┐
│ ✓ 连接主机            ✓ 探测文件            ◌ 验证 API                  │
│                                                                          │
│ ┌ 候选 ────────────────────────────────────────────────────────────────┐ │
│ │ [✓] K3s                                                              │ │
│ │     /etc/rancher/k3s/k3s.yaml · 可读                                 │ │
│ │     context: default · server: https://127.0.0.1:6443 · 证书认证      │ │
│ │     ✓ 已验证 v1.30.2+k3s1 · 18ms · 4 namespaces                      │ │
│ │     名称 [k3s-prod                        ]                          │ │
│ ├──────────────────────────────────────────────────────────────────────┤ │
│ │ [ ] kubeconfig                                                       │ │
│ │     /home/dev/.kube/config · 可读 · 2 contexts                       │ │
│ │       ☑ dev   https://10.0.0.5:6443   ⚠ 将绑定到当前 SSH 主机隧道    │ │
│ │       ☐ prod  https://api.example.com:6443                           │ │
│ ├──────────────────────────────────────────────────────────────────────┤ │
│ │ [!] /etc/kubernetes/admin.conf · 存在但不可读（权限不足）             │ │
│ │     仅 root 可读。可用 root 账户连接，或 [尝试免密 sudo 读取]         │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│ 环境信息：kubectl 未安装（不影响，Panoramix 直接调用 API）                │
│                                                  [取消] [添加选中项 (1)]  │
└──────────────────────────────────────────────────────────────────────────┘
```

**流程**：

1. 建立/复用该主机的后台 SSH 会话；期间复用现有 TOFU、认证、跳板链逻辑。
2. 执行探测；逐个展示候选，未读取的候选显示文件状态。
3. 读取勾选/展开的候选（≤1MB，5s 超时），Go 侧解析 kubeconfig：context 列表、server、auth 类型、CA/证书是否存在、是否 `insecure-skip-tls-verify`、是否使用 exec 插件。
   - kubeconfig 若用**文件路径**而不是 `*-data`（`certificate-authority` / `client-certificate` / `client-key`），SSH 来源经 SFTP 相对 kubeconfig 目录读取并内联为 data（单文件 ≤256KB，失败则标记候选无效）；本地导入按 kubeconfig 所在目录解析本地文件。
   - `tokenFile` 在 v0.4 不支持；遇到时禁用该候选并说明。
4. 对每个可解析候选做**验证**：建立隧道 → `GET /version`（必须成功）→ best-effort `GET /api/v1/namespaces?limit=1`。命名空间列表 403 **不判定为失败**，只显示「可连接，但无权列出命名空间」；证书、401、不可达才判定失败并显示结构化原因。
5. 用户可改显示名称（默认 `主机名` 或 kubeconfig 的 context 名；重名自动加 `-2`）。
6. 点「添加选中项」：**规范化后的、只含所选 context 的 kubeconfig**（文件路径凭据已内联）加密落库，切换到 K8s 视图并选中第一个添加的集群。
7. 卡片上的 `⚠ 将绑定到当前 SSH 主机隧道`：表示 server 指向 `127.0.0.1` / `localhost` / 私网地址，必须经该主机隧道访问。
8. **一个文件多个 context**：候选可展开为多个 context 行，各自勾选、验证、命名与添加；`source_path` 相同但 `context_name` 不同，刷新凭据时按各自的 context 重新规范化。

**验证失败的候选**允许「仍然添加」但标记「未验证」；进入视图后显示错误与「重试 / 刷新凭据」，不会假装可用。

### 手动导入 kubeconfig

```
┌ 导入 kubeconfig ─────────────────────────────────────────────────────────┐
│ 文件：D:\kube\config.yaml                      [重新选择]                 │
│ ┌ contexts ────────────────────────────────────────────────────────────┐ │
│ │ [✓] prod     https://api.prod.example.com:6443   证书认证   ✓ 已验证  │ │
│ │ [ ] staging  https://127.0.0.1:6443              证书认证             │ │
│ │              需要绑定 SSH 主机：[home-server ▾]                       │ │
│ │ [ ] eks      https://...          exec 插件（aws）— v0.4 不支持 ⓘ     │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│ 名称 [prod        ]   ⚠ insecure-skip-tls-verify 已开启，将不校验服务端证书│
│                                                  [取消] [导入选中项]      │
└──────────────────────────────────────────────────────────────────────────┘
```

规则：

- 支持证书认证（`client-certificate-data` / `client-key-data` / `certificate-authority-data`）与 bearer token；**不支持** `exec` 认证插件（EKS/GKE/AKS 常见）、`auth-provider`、basic auth（现代集群已移除）、`tokenFile` 与 `proxy-url`，卡片禁用并说明原因。
- server 指向 `127.0.0.1` / `localhost` / `::1` 时，必须绑定一台 SSH 主机；未绑定则无法验证、不能导入。
- server 是私网地址时，可选「经 SSH 主机访问」并绑定主机（强制走隧道）；不绑定则按本机直连验证。
- server 可达本机时可直接连接（不走 SSH 隧道）；仍会在导入时验证。
- 多 context 文件：每个勾选的 context 生成一个集群条目，各自保存**只包含该 context/cluster/user 的最小 kubeconfig**，避免把同一文件里的其他凭据一并复制。
- 文件名/路径只用于展示，不落库（避免泄漏本地目录结构）；如需「重新发现」，只有 SSH 来源的集群保存 `source_path`。

### 隧道与证书

**事实依据**：k3s 默认把 `/etc/rancher/k3s/k3s.yaml` 的 server 写成 `https://127.0.0.1:6443`，API 证书 SAN 只包含 `127.0.0.1`、`10.43.0.1`、`kubernetes` 等；直接把这台机器上的 kubeconfig 拷到本地会因证书 SAN 不匹配失败。绕开 SAN 改写的标准做法就是 SSH 本地/远程转发。[腾讯云 K3s 教程](https://cloud.tencent.cn/developer/article/2747124)、[StackOverflow 61913810](https://stackoverflow.com/questions/61913810)

> 与 ROADMAP 措辞的差异：我们**不生成一份「重写 server 后的 kubeconfig」文件**，而是运行时用原始 kubeconfig 构建 client + 自定义 Dialer。「可用」体现在连接层，避免把包含客户端私钥的 kubeconfig 副本再次落盘。

**我们的实现**（不监听本地端口）：

```
Go 进程
  client-go rest.Config
    Host = https://127.0.0.1:6443（来自 kubeconfig，不改写）
    TLS  = CA / client cert 来自 kubeconfig
    Dialer(ctx, network, addr) ──► ssh.Client.Dial("tcp", "127.0.0.1:6443")
                                        │ SSH channel
                                        ▼
                                  远端主机 127.0.0.1:6443（k3s apiserver）
```

关键点：

1. **不改写 kubeconfig 的 server**，`rest.Config.Host` 保持原值。TLS 的 ServerName 由 URL 主机名派生，`127.0.0.1` 在 k3s 证书 SAN 中，校验通过。
2. **不监听 `127.0.0.1:随机端口`**：自定义 `Dialer` 直接把每个 TCP 连接映射成 SSH channel，不暴露本地端口，也不会被本机其他进程借用。
3. 目标地址就是 kubeconfig 里的 `host:port`，**在远端解析/连接**；因此私网地址、`127.0.0.1`、远端 DNS 名都成立。
4. 跳板链复用现有 `ProxyJump` 的最终 `ssh.Client`；K3s 证书不因跳板而改变。
5. `rest.Config.Proxy` 置为 `nil`，避免系统代理劫持；`User-Agent` 设为 `Panoramix/<version>`。
6. client-go 使用 `Dialer` 接口（而非已废弃的 `Dial` 函数字段），以正确参与 transport 缓存。[Kubernetes PR #112144](https://github.com/kubernetes/kubernetes/pull/112144)、[client-go REST 配置说明](https://deepwiki.com/kubernetes/client-go/2.1-rest-configuration)
   - 推荐构建路径：`clientcmd.Load(raw)` → `NewNonInteractiveClientConfig(cfg, context, overrides, nil)` → `ClientConfig()` → 设置 `Dialer` / `Proxy=nil` / `UserAgent`。不要手拼 `rest.Config` 的认证字段。
7. **exec/attach 走同一条链路**：`remotecommand` 的 SPDY/WebSocket upgrade 通过同一个 `rest.Config` 传输；实现时需用真实 `kubectl exec` 场景验证升级、TTY、resize（见验收项）。
8. **证书信任**：只使用 kubeconfig 内的 CA，不把 CA 装进系统信任库；`insecure-skip-tls-verify: true` 允许但全程显示黄色 `TLS 未校验` 徽标。
9. **k3s 证书轮换 / 重装**：API 报 x509 时提示「刷新凭据」；SSH 来源的集群重新读取源文件，导入的集群提示重新导入。**不自动替换**。

**失败与降级**：

| 情况 | 用户看到 | 后端行为 |
| --- | --- | --- |
| 主机没有 k3s/kubeconfig | 空结果 + 「未发现 Kubernetes 配置」+ 手动导入入口 | 不添加任何东西 |
| `k3s.yaml` 存在但不可读 | 权限说明 + 「尝试免密 sudo 读取」+ 「用 root 连接」 | 不读取；用户显式点击后才 `sudo -n cat` 固定路径 |
| apiserver 未启动 / 端口变了 | 「无法连接 apiserver（connection refused）」+ 重试 | 不重试风暴；指数退避上限 30s |
| 证书 SAN 不匹配 | 「证书与地址不匹配」+ 服务端证书中的 SAN 摘要 + 「刷新凭据」 | 不跳过校验 |
| 401 | 「凭据已失效」+ 刷新/重新导入 | 不重试 |
| 403 | 「当前凭据无权限」+ 具体 verb/resource | 不重试 |
| SSH 断开 | 顶部横幅「SSH 隧道已断开 · 重连中 (2/5)」；旧数据灰显并标明「最后更新 x 秒前」 | 后台重连；恢复后重列并推送 reset |
| 免密 sudo 不可用 | 「免密 sudo 不可用」+ 手动步骤 | 不弹密码框、不提权 |

### 连接生命周期

- **后台 SSH 会话按主机维护**，与终端标签独立；一个主机可承载多个集群，共用一个 SSH 连接。这样关掉终端标签不会中断 K8s。会话按主机引用计数：取消发现/关闭某个集群不关闭仍被活动集群使用的连接。
- 复用现有认证来源（keychain / SSH agent / 私钥 + passphrase）、TOFU 与跳板链；不写新的认证逻辑。主机指纹未知/变更时弹出现有 TOFU 弹窗，K8s 视图不另做一套。
- 认证/TOFU/凭据补录弹窗上提到 App 级统一状态，由连接树与 K8s 视图共用；避免两套弹窗逻辑。
- **同一时间只激活一个集群**的连接与 watch（v0.4）。切换集群时：停止旧集群的日志/终端流 → 5 秒宽限后断开旧后台会话 → 连接新集群。
- 切到「终端 / 数据库」一级视图：若**没有**活动的日志/终端流，K8s 连接保留 **60 秒**后暂停（停止 watch/轮询、关闭 SSH 后台会话），标签状态保存在内存；回到 K8s 视图时自动重连当前集群，日志显示「已暂停，点击恢复」。
- 若切走时仍有活动流（用户在容器里跑长命令、或日志正在跟），**保持连接直到流结束或用户手动断开**，不因视图切换杀掉会话；此时顶部状态在回到 K8s 时显示「后台连接中」。
- 应用退出：关闭所有流与后台 SSH，不落任何运行态数据。
- 重连退避 1/2/5/10/20/30 秒，连续 5 次失败后置为「已断开」，提供手动「重连」。

### watch 与轮询

| 数据 | 机制 | 频率 / 说明 |
| --- | --- | --- |
| Pods | watch（informer，全部命名空间） | 增量合并后每 **250ms** 推一批；断线恢复后推送 reset 快照 |
| Events | watch（全部命名空间） | 同上；前端保留 1000 条、后端保留 2000 条 |
| Workloads | 轮询 list | K8s 视图活跃时 **5s**；页面隐藏或视图切走时暂停 |
| Namespaces | 轮询 list | **30s**，变更不频繁 |
| 集群版本 | 连接时一次 | `/version` |

- 命名空间**不做服务端重订阅**：Pods/Events 始终 watch 全部命名空间，前端按选择过滤。小集群（v0.4 目标）这样切换命名空间是瞬时的。
- 若全部命名空间的 list/watch 返回 403：降级为「当前选择命名空间」重新订阅（优先 `last_namespace`，否则 `default`），并提示「当前凭据只能访问部分命名空间」；命名空间选择器改为可输入的自由文本框。
- 若 `list namespaces` 403：选择器只显示 `default` 与手动输入；不阻塞 Pods 浏览。
- Pod/Event 事件推送使用增量批：`{clusterId, kind, reset, items:[{op, object}]}`；前端按 uid 维护 Map，避免重复行。
- 表格滚动/筛选在内存完成；不因筛选重新请求。

---

## 数据与接口

### 存储模型（SQLite schema 增量）

```sql
CREATE TABLE k8s_clusters (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  source            TEXT NOT NULL CHECK (source IN ('ssh', 'import')),
  host_id           TEXT,                         -- ssh 来源必填；import 可为空
  context_name      TEXT NOT NULL,
  server            TEXT NOT NULL,                -- 原文，不改写
  auth_type         TEXT NOT NULL,                -- cert | token
  insecure_tls      INTEGER NOT NULL DEFAULT 0,
  source_path       TEXT NOT NULL DEFAULT '',     -- ssh 来源的 kubeconfig 路径
  kubeconfig_sha256 TEXT NOT NULL,                -- 源文件原始内容哈希，用于变更提示
  kubeconfig_cipher BLOB NOT NULL,                -- AES-256-GCM 密文
  kubeconfig_nonce  BLOB NOT NULL,                -- 12 字节随机 nonce
  last_namespace    TEXT NOT NULL DEFAULT '',     -- 上次选择的命名空间；'' = 全部
  last_version      TEXT NOT NULL DEFAULT '',
  last_connected_at TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);
CREATE INDEX idx_k8s_clusters_host ON k8s_clusters(host_id);
```

- `settings` 新增 `k8s.auto_discover`（`0/1`，默认 `1`）。
- 不新增「事件/日志/终端」持久化；这些全部内存态。
- 迁移随现有 SQLite 迁移机制递增 schema 版本；旧库自动创建表。

**加密细节**：

- **每个集群一把数据密钥**：32 字节随机数，OS keychain（service=`Panoramix`，account=`k8s:{clusterId}:key`）。不用全局密钥，便于「删集群 = 删密钥」的加密擦除。
- AES-256-GCM；每条记录独立 12 字节 nonce；AAD = `k8s:{clusterId}:v1`，防止把 A 集群的密文换到 B 集群行。
- kubeconfig 明文只在 Go 内存中存在；不写临时文件、不写日志、不下发前端。
- keychain 不可用（Linux 无 Secret Service 等）：**不降级为明文**；集群仅存在内存，重启即消失，UI 明确提示「本机未提供安全存储，本次会话可用」。
- 写入顺序：先生成密钥写入 keychain，再插入数据库行；插行失败则删除刚写的密钥，避免孤儿密钥。
- 删除集群：**先删 keychain 密钥，再删数据库行**。密钥删除成功（或密钥本就不存在）而行删除失败时，把行标记为「已失效」并重试删除（此时密文已不可解）；密钥删除失败则中止，不删行。明确告知「删除后需重新发现/导入」。
- 删除主机：若该主机下有 N 个集群，删除确认文案必须列出「将同时移除 N 个集群的凭据」，事务内删除集群行（不依赖 SQLite 外键开关）。
- 不提供「导出 kubeconfig」「复制 kubeconfig」功能。

**安全边界（明示）**：

- **保护**：SQLite 文件被拷走（无 keychain 密钥则为密文）、前端/DevTools 拿不到原文、日志不落凭据、无遥测/无 AI 上传、无「导出」入口。
- **不保护**：本机已被完全控制（能读 keychain 与进程内存）、SSH 主机已被控制、用户主动复制 YAML/日志/终端里的敏感内容。
- k3s 默认 kubeconfig 是 **cluster-admin** 级别；UI 的「只读」是产品约束，不是安全边界。真正的权限边界仍由 K8s RBAC 决定。删除集群/主机时必须让用户意识到凭据被移除。

### 绑定方法

所有方法在 `internal/app.Service` 上，前端只拿摘要 DTO，永远拿不到 kubeconfig 内容。

| 方法 | 说明 |
| --- | --- |
| `K8sListClusters() []K8sClusterSummary` | 集群列表（按来源主机分组所需字段） |
| `K8sGetAutoDiscover() bool` / `K8sSetAutoDiscover(on bool)` | 自动探测开关 |
| `K8sStartDiscover(hostId, trustMode) {discoverId}` | **立即返回** discoverId 并在后台连接/探测/解析；进度走 `k8s:discover-progress`。必须在事件里先带 discoverId，避免长绑定调用阻塞 UI |
| `K8sGetDiscoverResult(discoverId) DiscoverResult` | 探测完成后取候选摘要；未完成返回 `pending=true` |
| `K8sCancelDiscover(discoverId)` | 关闭弹窗时取消后续步骤、释放连接 |
| `K8sAddDiscovered(discoverId string, selections []DiscoverSelection) []K8sClusterSummary` | 从服务端缓存的发现会话中添加；会话 5 分钟过期、单次使用 |
| `K8sPickKubeconfig() ImportSession` | 打开系统文件选择器，解析后返回 `importId` 与 context 预览；不保存 |
| `K8sImportClusters(importId string, selections []ImportSelection, trustMode string) []K8sClusterSummary` | 保存选中 context；每个 context 存最小 kubeconfig；绑定主机验证时复用 TOFU trustMode |
| `K8sCancelImport(importId)` | 取消导入、清空服务端缓存的 kubeconfig 字节 |
| `K8sRenameCluster(id, name)` | 重命名（非空、≤64 字符、去重） |
| `K8sRemoveCluster(id)` | 删除集群与密文 |
| `K8sRefreshCredentials(id, trustMode)` | SSH 来源：重读源路径、比较哈希、更新密文；导入来源：提示重新导入 |
| `K8sConnect(id, trustMode) K8sStatus` / `K8sDisconnect(id)` | 显式连接/断开当前集群；trustMode 与现有 TOFU 语义一致（`""` / `add` / `replace`） |
| `K8sGetStatus(id) K8sStatus` | 状态查询（重连后前端补拉） |
| `K8sListNamespaces(id) []K8sNamespace` | 命名空间列表；403 时返回 `forbidden=true` |
| `K8sListWorkloads(id, namespace) []K8sWorkload` | 轮询结果；namespace 为空 = 全部 |
| `K8sGetYAML(id, kind, namespace, name) string` | 只读 YAML，上限 2MB；kind 仅 `pod/workload/namespace` |
| `K8sOpenLogs(LogRequest) LogStream` | `{clusterId, namespace, pod, container, follow, tailLines, timestamps, previous}` → `{streamId}` |
| `K8sCloseLogs(streamId)` | 停止流 |
| `K8sStartExec(ExecRequest) ExecSession` | `{clusterId, namespace, pod, container, shell}` → `{sessionId}` |
| `K8sWriteExec(sessionId, dataB64)` | 键盘输入（base64） |
| `K8sResizeExec(sessionId, cols, rows)` | 终端尺寸 |
| `K8sCloseExec(sessionId)` | 结束会话 |

**发现/导入会话的生命周期**：

- `discoverId` / `importId` 为不透明随机串，服务端保存解析后的候选与 kubeconfig 字节，**单次使用、5 分钟过期**；前端只拿候选摘要（路径、context、server、auth 类型、警告、验证结果）。
- 失败原因只返回结构化 code 与用户可读文案，不返回原始 kubeconfig 或证书内容。
- 发现流程中验证可并发（最多 2 个候选），避免多集群同时打满 SSH channel。

### 事件（后端 → 前端）

| 事件 | 载荷 | 时机 / 节流 |
| --- | --- | --- |
| `k8s:status` | `{clusterId, state, message, version}` | 状态变化即发；`ready` = API + watch 正常；`degraded` = API 可用但 watch/轮询失败或权限受限（前端显示黄色「实时更新降级」）；`disconnected` / `error` 见错误码 |
| `k8s:pods` | `{clusterId, reset, done, items:[{op, pod}], truncated}` | informer 增量合并后 **250ms** 一批；reset 快照按 200 条/片推送，首片 `reset=true`，末片 `done=true` |
| `k8s:events` | `{clusterId, reset, done, items:[{op, event}], dropped}` | 同上；`dropped` 表示超出保留上限已丢弃最旧 |
| `k8s:log` | `{streamId, dataB64, droppedBytes}` | 每 **100ms** 或 **64KB** 一批；二进制安全（base64）；单流发送队列上限 256KB，超出丢弃最旧并计入 `droppedBytes`，不阻塞读取 |
| `k8s:log-end` | `{streamId, reason}` | 容器结束、Pod 删除、403、隧道断开、用户关闭 |
| `k8s:exec-output` | `{sessionId, dataB64}` | 每 **16ms** 合并一批（与终端一致） |
| `k8s:exec-exit` | `{sessionId, code, reason}` | exec 结束 |
| `k8s:discover-progress` | `{discoverId, step, status, detail?}` | step = `connect` / `probe` / `read` / `verify`；status = `running` / `done` / `error`；detail 不含凭据 |

**错误码**（绑定返回与事件 message 的结构化 code）：

| code | 含义 | UI 处理 |
| --- | --- | --- |
| `k8s_invalid_kubeconfig` | 解析失败 / 缺 context / 缺凭据 | 指向具体字段，阻止添加 |
| `k8s_unsupported_auth` | exec 插件 / auth-provider / basic auth / tokenFile | 禁用候选并解释 |
| `k8s_auth_failed` | 401 | 刷新凭据 / 重新导入 |
| `k8s_forbidden` | 403，带 verb/resource | 权限状态，不重试 |
| `k8s_not_found` | 404 | 资源已删除，刷新列表 |
| `k8s_tls_error` | x509 / SAN 不匹配 | 显示证书摘要，刷新凭据 |
| `k8s_api_unreachable` | 连接拒绝 / 超时 | 退避重试 + 手动重试 |
| `k8s_tunnel_down` | SSH 断开 | 顶部横幅 + 自动重连 |
| `k8s_exec_not_supported` | 镜像无 shell / 命令不存在 | 提示换 shell 或镜像无 shell |
| `k8s_log_unavailable` | 容器未启动 / previous 不存在 | 等待态 / 解释 |
| `k8s_too_many_streams` | 超过并发上限 | 提示关闭一个 |

### 后端模块与依赖

- 新增 `internal/k8s`：
  - kubeconfig 解析与 `rest.Config` 构建；
  - `Dialer` 接口（`DialContext(ctx, network, addr) (net.Conn, error)`）由 `app` 注入，`k8s` 不 import `internal/ssh`；
  - Pod informer、Workloads 轮询、日志流、exec 流；
  - Events 使用**自建 watch + 环形缓冲**（保留最近 2000 条），而不是 SharedInformer 的全量 store，便于强制上限与裁剪；
  - client-go 选择包含 `rest.Config.Dialer` 接口的较新版本（建议 v0.31.x 或项目锁定的兼容版本）；
  - 只依赖 client-go / apimachinery / yaml 库与标准库，不 import 其他 internal 包。
- `internal/ssh`：新增**无 PTY 的后台连接**（复用 `dialChain` / `hostKeyCallback` / keepalive / 认证来源），供 K8s 隧道与探测使用；不改变现有 PTY `Session` 的行为。
- `internal/app`：发现编排（SSH exec 固定探测、SFTP 读文件）、加密存储、DTO 映射、事件发射。
  - 单文件读取复用 app 层已有的 SFTP-over-SSH client；若 `TransferQueue` 不暴露只读 `Open/Read/Close`，在 app 层增加受限 helper（1MB/256KB 上限、5s 超时），不把路径拼进 shell。
- `internal/store`：新表、迁移、数据密钥的 keychain 读写。
- 依赖方向：`app → k8s / ssh / sftp / store`；`k8s` 不横向依赖其他 internal 包。
- 建议新增依赖：`k8s.io/client-go`、`k8s.io/api`、`k8s.io/apimachinery`；exec 用 `k8s.io/client-go/tools/remotecommand`。client-go 体积会增加二进制，但单二进制定位可接受。

### 建议实现顺序（供 engineer 拆分提交）

1. **后端底座**：kubeconfig 解析、加密存储、后台 SSH 连接、自定义 Dialer、`/version` + namespaces 列表。
2. **发现/导入**：探测、SFTP 读取、候选验证、发现弹窗、集群树。
3. **浏览**：Pods/Workloads/Namespaces 表格与详情、YAML、命名空间切换、轮询。
4. **实时**：Pod informer + Event watch、增量推送、重连恢复。
5. **日志**：流、选项、搜索、暂停、缓冲上限、导出。
6. **终端**：exec、resize、结束态、并发上限。
7. **降级与打磨**：RBAC 状态、隧道断开、空状态、i18n、快捷键、性能。

每一步都可独立验证；不要把「日志 + 终端 + watch」压成一个大提交。

---

## 边界情况

| 场景 | 设计 |
| --- | --- |
| 没有任何集群 | 空状态：图标 + 「尚未添加集群」+ 两个按钮（从主机发现 / 导入）+ 说明「连接 SSH 主机后会自动检测」 |
| 集群已添加但主机离线 | 集群行灰显「主机离线」；点击提示先连接主机；不自动弹认证框（除非用户主动连接） |
| 主机没有 K3s / kubeconfig | 发现弹窗显示「未发现 Kubernetes 配置」，提供手动导入与路径提示 |
| k3s 使用自定义 `--write-kubeconfig` 路径 | 常规路径探测不到；提示「未发现」并引导手动导入/把路径加入 `KUBECONFIG`；v0.4 不做任意路径扫描 |
| kubeconfig 不可读 | 解释权限；提供 root 连接说明与「尝试免密 sudo」；绝不弹 sudo 密码框 |
| 主机未启用 SFTP 子系统 | 显示「该主机无法通过 SFTP 读取 kubeconfig」+ 手动导入/取回文件指引；不回退到 shell `cat` |
| kubeconfig 用文件路径引用证书，且文件读不到 | 该候选标记无效并指出缺失文件；不添加半可用集群 |
| 命名空间为空（全部/单个） | 表格空状态 + 「查看全部命名空间」/「切换到 default」 |
| Pod 数量很大（500–2000） | 虚拟滚动；watch 增量；筛选/排序在内存；不因滚动重新请求 |
| Event 大量涌入 | 后端保留 2000、前端 1000，丢弃最旧并显示「仅显示最近 N 条」；不清空表格 |
| 资源被删除 | 删除增量到达后从列表移除；已打开的详情标签显示「资源已不存在」+ 关闭按钮；日志/终端显示结束原因 |
| 多容器 Pod | 日志/终端必须先选容器；默认第一个；标题显示容器序号 |
| 容器尚未启动 | 日志显示「等待容器启动…」；尝试 `previous` 显示「没有上一次容器实例」 |
| 容器无 shell（distroless） | 终端显示 exec 失败原因 + 「该镜像可能没有 shell」；不提供自动换镜像 |
| 日志包含 ANSI / 非 UTF-8 | 剥离 ANSI 转义；非法 UTF-8 以替换字符显示；不崩溃 |
| 日志超长行 | 单行折行显示（可切换），复制保留原文；不撑破布局 |
| YAML 超过 2MB | 只显示前 2MB 并提示「内容过大，建议用 kubectl 查看」；不阻塞其他子标签 |
| Pod 日志 403 | 日志子标签显示「无 pods/log 权限」+ 复制错误详情；不循环重试 |
| exec 403 | 终端子标签显示「无 pods/exec 权限」；重试按钮保留 |
| `list namespaces` 403 | 命名空间选择器降级为 `default` + 手动输入；提示权限受限 |
| 全部命名空间 watch 403 | 自动降级为单命名空间订阅，并提示；记录用户选择的命名空间 |
| watch 断线（410 Gone） | informer 自动重新 list；前端 reset，不产生重复行 |
| SSH 隧道断开 | 横幅 + 重连计数；表格灰显「最后更新 x 秒前」；恢复后 reset |
| apiserver 挂掉（k3s 停服） | `k8s_api_unreachable`，退避重试，显示原因与「重试」 |
| 证书过期 / k3s 重装 | `k8s_tls_error`，提示刷新凭据/重新导入 |
| kubeconfig 被替换（新证书） | 已添加集群检测到哈希变化，显示「凭据已更新」徽标；刷新需确认 |
| 导入的 kubeconfig 指向 127.0.0.1 且未绑定主机 | 导入页阻止保存，提示选择 SSH 主机 |
| 同一主机多个集群 | 共用后台 SSH 会话；同一时间只激活一个集群的 API 连接 |
| 同一 context 名重复 | 显示名自动加后缀；ID 独立 |
| 集群重命名冲突 | 阻止并提示；不自动改名 |
| 删除正在使用的集群 | 确认弹窗；先断开流、关闭标签、再删密文 |
| 删除有集群的主机 | 确认文案列出集群数量与后果；事务删除 |
| keychain 不可用 | 集群仅本次会话可用，提示「重启后需重新发现」；不落明文 |
| keychain 密钥丢失/损坏 | 集群显示「凭据不可用」，只能移除后重新发现/导入；不尝试绕过解密，不把它当成网络错误 |
| 前端崩溃/重载 | 后端流随视图销毁关闭；重启后集群仍在（加密存储），运行态不恢复 |
| 控制字符 / 恶意日志内容 | 以纯文本渲染，不解释 HTML/ANSI；避免 XSS |
| 系统时区/时钟偏差 | Age 用本地时钟相对服务端时间；事件时间显示绝对时间，避免误判 |
| 屏幕较窄（<1100px） | 左侧栏可折叠；详情子标签横向滚动；日志工具栏换行；不隐藏关键操作 |

---

## 验收标准

> QA 可按编号逐项执行；每项都应是可观察的行为，不依赖实现细节。
> 建议环境：一台 root 可登录的 k3s 主机、一台普通用户主机的 k3s（文件不可读）、一台无 K8s 主机、一个 `view` 角色且无 `pods/log` / `pods/exec` 的 kubeconfig、一个多容器 Pod、一个 CrashLoopBackOff Pod、一个 600+ Pod 的压测命名空间。

### A. 导航与信息架构

- [ ] A1 顶部栏出现第三个「Kubernetes」视图；zh-CN / en-US 文案完整，图标与现有线性图标风格一致。
- [ ] A2 在三个一级视图之间切换，终端标签、终端输出、数据库标签与查询结果均不丢失。
- [ ] A3 K8s 视图为全屏布局，不显示终端主机侧栏；左侧栏宽度约 264px，可折叠。
- [ ] A4 左侧栏集群按来源主机分组；手动导入的集群在「导入」组。
- [ ] A5 浏览器标签固定不可关闭；切换资源类型/命名空间时标题原地更新并激活，不新开标签。
- [ ] A6 详情标签可关闭；同一对象重复打开只激活已有标签；最多 12 个。
- [ ] A7 资源区显示 Namespaces / Workloads / Pods / Events 及数量；Events 显示 warning 数。
- [ ] A8 双击 Pod 行或按 Enter 打开详情；右键菜单包含详情/日志/终端/YAML/复制名称。
- [ ] A9 `?` 打开快捷键帮助；所有快捷键都有等价鼠标入口。
- [ ] A10 空状态符合设计系统：图标 + 主提示 + 可执行指引，不是纯空白。
- [ ] A11 单击集群行即连接并激活；切换集群不弹确认，旧集群流停止。

### B. SSH 自动发现

- [ ] B1 root 登录 k3s 主机后，主机行出现 `K8s` 徽标；首次发现弹一次性提示，第二次连接不再弹。
- [ ] B2 右键主机 →「发现 Kubernetes 集群」，显示连接/探测/验证进度，最终列出 `/etc/rancher/k3s/k3s.yaml`。
- [ ] B3 候选显示 context、server、认证类型、版本、命名空间数（无权限时显示「无权列出命名空间」而非验证失败）；名称可编辑。
- [ ] B4 添加后自动切到 K8s 视图并选中该集群，Pods 列表可见。
- [ ] B5 普通用户且 `k3s.yaml` 为 `0600 root`：显示「权限不足」与操作指引；不崩溃、不弹密码框。
- [ ] B6 配置免密 sudo 后点击「尝试免密 sudo 读取」可成功；未配置时提示不可用。
- [ ] B7 无 K8s 主机：显示「未发现 Kubernetes 配置」，不添加任何集群。
- [ ] B8 设置 `KUBECONFIG=/home/dev/.kube/config` 时，该候选被列出并可添加。
- [ ] B9 `~/.kube/config` 作为候选列出并可添加。
- [ ] B10 RKE2 的 `/etc/rancher/rke2/rke2.yaml` 被识别（如环境具备）。
- [ ] B11 关闭「连接时自动探测」后，连接主机不再探测；手动入口仍可用。
- [ ] B12 已添加集群的 kubeconfig 被替换后，连接主机显示「凭据已更新」；确认刷新后新凭据生效。
- [ ] B13 发现流程失败（apiserver 未启动）时，候选可「仍然添加」但标记未验证，进入后显示错误与重试。
- [ ] B14 发现/导入会话 5 分钟后过期；过期后添加返回明确错误。
- [ ] B15 含多个 context 的 kubeconfig 可展开为多行，各自勾选、命名、验证并添加。
- [ ] B16 kubeconfig 用文件路径引用 CA/客户端证书时，能读取并内联；文件缺失时候选被标记无效而不是添加半可用集群。

### C. 手动导入

- [ ] C1 文件选择器只接受 yaml/yml/config；>1MB 文件被拒绝并说明。
- [ ] C2 多 context 文件展示全部 context；可多选，每个 context 生成独立集群条目。
- [ ] C3 server 为 127.0.0.1 的 context 必须绑定 SSH 主机，否则不能导入。
- [ ] C4 `exec` 插件认证的 context 被禁用并说明「v0.4 不支持」。
- [ ] C5 `insecure-skip-tls-verify: true` 的 context 导入后显示黄色 `TLS 未校验` 徽标。
- [ ] C6 导入后重启应用，集群仍在且可连接。
- [ ] C7 重复导入同名 context 不覆盖已有条目；显示名加后缀。

### D. 隧道与连接

- [ ] D1 本机无法直接访问集群 6443 端口，但 Panoramix 能列出 Pods（证明走 SSH 隧道）。
- [ ] D2 不监听本地 6443 或随机端口：连接期间本机 `ss -lntp` 无新增监听。
- [ ] D3 经 1 跳/2 跳 ProxyJump 主机仍能连接集群。
- [ ] D4 首次连接未知主机指纹弹 TOFU；确认后继续；指纹变更走现有红色警告流程。
- [ ] D5 k3s 重启/证书变化后显示证书错误与「刷新凭据」，刷新后恢复。
- [ ] D6 拔网线/断 SSH：显示「SSH 隧道已断开 · 重连中」，旧数据灰显；恢复后自动重连并刷新。
- [ ] D7 连续 5 次重连失败后状态为「已断开」，提供手动重连。
- [ ] D8 切换集群后旧集群的流停止；新集群可正常浏览。
- [ ] D9 无活动流时切到终端视图超过 60 秒再回来，集群自动重连；标签状态保留，日志显示「已暂停」；若有活动终端流，切走后连接与流保持，不被 60 秒规则杀掉。
- [ ] D10 应用退出后无残留 SSH 连接/本地端口/临时 kubeconfig 文件。

### E. 浏览与实时

- [ ] E1 命名空间下拉列出全部命名空间；切换后表格过滤正确。
- [ ] E2 默认「全部命名空间」；选择会按集群记住，重启后恢复。
- [ ] E3 `kubectl create` 一个 Pod，2 秒内出现在列表；删除后 2 秒内消失。
- [ ] E4 `kubectl scale` Deployment 后，Workloads 的 Ready/Up-to-date 在 6 秒内更新。
- [ ] E5 产生一条 Warning Event，2 秒内出现在 Events 表；`仅看 Warning` 过滤正确。
- [ ] E6 表格筛选对 name/namespace/labels 生效；无结果时显示清除入口。
- [ ] E7 点击列头排序稳定；Age 每 10 秒更新；hover 显示绝对时间。
- [ ] E8 600 Pods 时滚动流畅（无 >200ms 卡顿），筛选即时。
- [ ] E9 Pod 详情字段与 `kubectl get pod -o wide` / `describe` 一致（状态、Ready、Restarts、IP、Node、Owner、容器镜像）。
- [ ] E10 多容器 Pod 详情列出全部容器及各自状态。
- [ ] E11 Workload 详情列出其标签选择器匹配的 Pods；点击 Pod 可进入详情。
- [ ] E12 YAML 子标签内容与 `kubectl get -o yaml` 一致；没有编辑/保存/应用按钮；可复制。
- [ ] E13 命名空间表格显示 Pods 计数；双击切换命名空间并跳到 Pods。
- [ ] E14 全部命名空间 watch 返回 403 时，自动降级到单命名空间并提示，不显示空白错误页。

### F. 日志

- [ ] F1 打开日志后 1 秒内出现历史行；新日志持续跟随。
- [ ] F2 `行数` 切换 200/500/1000 后重新打开流，行数符合预期。
- [ ] F3 `时间戳` 开关生效；关闭/重新打开后设置保持（本次会话）。
- [ ] F4 多容器切换后日志来自所选容器。
- [ ] F5 重启过的容器可看「上一次性」日志；此时「跟随」被禁用并有说明。
- [ ] F6 搜索高亮全部匹配并显示 `n/m`；上下跳转可用。
- [ ] F7 向上滚动暂停自动滚动，显示「回到底部 (N 新)」；点击回到底部。
- [ ] F8 `清空` 只清视图，不停止流；新日志继续出现。
- [ ] F9 超过 10,000 行时显示丢弃提示，UI 不卡顿；内存不持续增长。
- [ ] F10 导出保存当前缓冲区，文件内容与视图一致。
- [ ] F11 容器未启动时显示等待态；`previous` 无数据时解释原因。
- [ ] F12 无 `pods/log` 权限时显示权限状态，不循环重试；错误详情可复制。
- [ ] F13 关闭日志标签后 2 秒内后端停止推送（无新增 `k8s:log`）。
- [ ] F14 同时打开 3 个日志流正常；第 4 个提示关闭一个。
- [ ] F15 ANSI 转义不显示为 `[32m`；非 UTF-8 字节不导致崩溃。
- [ ] F16 关闭「跟随」后本次流结束并出现「刷新」；点击刷新读取到新内容，不会假装仍在实时更新。

### G. Pod 终端

- [ ] G1 打开终端后可执行 `tty`、`ls`、`env`；输出与 `kubectl exec` 一致。
- [ ] G2 拖动窗口改变尺寸后 `stty size` 与新尺寸一致，`top` 无错位。
- [ ] G3 多容器可选择容器并进入对应容器。
- [ ] G4 `exit` 后显示退出码与「会话已结束」遮罩。
- [ ] G5 容器无 `/bin/sh` 时显示明确错误与「镜像可能无 shell」提示。
- [ ] G6 无 `pods/exec` 权限时显示权限状态，不重试风暴。
- [ ] G7 Pod 被删除/重启时终端结束并说明原因。
- [ ] G8 隧道断开时终端结束并说明「连接已断开」；重连后需手动重新打开。
- [ ] G9 同时打开 3 个终端正常；第 4 个提示关闭一个。
- [ ] G10 终端内 `Ctrl+Shift+C/V`、`Ctrl+F`、`Ctrl+滚轮` 行为与 SSH 终端一致。
- [ ] G11 K8s 终端不生成 SSH 会话日志文件（即使全局会话日志开启）。
- [ ] G12 终端顶部始终显示集群/命名空间/Pod/容器与「操作会影响该容器」提示。

### H. 安全与持久化

- [ ] H1 退出应用后，用 `strings` 或十六进制查看 SQLite 文件，**不出现** `client-key-data`、`BEGIN CERTIFICATE`、`BEGIN PRIVATE KEY`、token 明文。
- [ ] H2 前端任何绑定返回值、DevTools 网络/状态中都不含 kubeconfig 内容。
- [ ] H3 应用日志中不出现 kubeconfig、token、证书、私钥、exec 命令内容。
- [ ] H4 删除集群后，SQLite 中该行与密文消失，且该集群的 keychain 密钥条目（`k8s:{id}:key`）被删除；再次启动不再出现。
- [ ] H5 删除有 N 个集群的主机时，确认文案列出 N 个集群；确认后集群与凭据一并删除。
- [ ] H6 keychain 不可用时，发现/导入仍可用但提示「仅本次会话」；重启后集群不存在。
- [ ] H7 没有「导出/复制 kubeconfig」入口。
- [ ] H8 导入 `insecure-skip-tls-verify` 集群后，徽标持续可见，不静默忽略。
- [ ] H9 发现页/日志页展示的路径、context、server 不含私钥/证书内容。
- [ ] H10 应用不产生任何出站遥测/AI 请求（抓包或代码审查确认）。
- [ ] H11 Pod 日志与终端输出不写任何本地文件（用户点击「导出」除外）；即使全局 SSH 会话日志开启也不落盘。

### I. 错误与降级

- [ ] I1 apiserver 停服：显示「无法连接 apiserver」+ 重试；不无限 spinner。
- [ ] I2 401：显示「凭据失效」+ 刷新凭据；不自动重试。
- [ ] I3 403：显示具体 verb/resource 与权限说明；不显示为「网络错误」。
- [ ] I4 404：资源已删除；列表刷新；详情显示不存在。
- [ ] I5 watch 410：自动重列，无重复行、无丢失行。
- [ ] I6 主机离线：集群行灰显；点击不卡死；给出连接主机指引。
- [ ] I7 事件超过上限：显示「仅显示最近 N 条」，不静默丢数据。
- [ ] I8 所有错误状态都有「复制详情」或可展开的原始信息，便于排障。

### J. 设计一致性与 i18n

- [ ] J1 所有颜色来自 `global.css` 变量，无硬编码色值（代码审查）。
- [ ] J2 日志/YAML/终端使用 `--font-mono`；正文使用 `--font-ui`。
- [ ] J3 危险/警告/成功状态分别使用 danger/warning/accent 语义色。
- [ ] J4 zh-CN / en-US 无缺失 key、无占位符泄漏；切换语言即时生效。
- [ ] J5 标签、树、空状态、弹窗样式与现有终端/数据库视图一致（圆角、间距、hover）。
- [ ] J6 屏幕宽度 1100px 时布局不重叠；左侧栏可折叠；日志工具栏可换行。
- [ ] J7 表格/树可键盘导航，焦点样式可见；纯图标按钮有 `title`/`aria-label`。

### K. 回归

- [ ] K1 未添加任何 K8s 集群时，应用启动、终端、SFTP、监控、数据库、AI 助手行为不变。
- [ ] K2 连接树原有右键菜单/连接/导入功能不受新增菜单项影响。
- [ ] K3 删除主机、导入主机等既有流程不回归。
- [ ] K4 `go vet ./...`、`go test ./...`、`GOOS=windows go build ./...` 全绿。
- [ ] K5 关闭 K8s 视图后无 goroutine 泄漏（`go test` 的泄漏检测或运行时 goroutine 数量对比）。

---

## 不做什么（v0.4 范围外）

以下条目明确排除，防止范围蔓延；如用户要求，另开版本。

1. **任何修改类操作**：删除 Pod/Workload、扩缩容、重启/rollout restart、cordon/drain、编辑/apply YAML、创建资源、Evict、端口转发。v0.4 全部只读（终端 exec 除外，它是用户主动进入容器）。
2. **YAML 编辑与应用**：v0.5 再做；届时按「编辑 → diff 预览 → 确认 apply」三步走。v0.4 只有只读查看。
3. **自定义资源 / CRD / Helm / Operators**：只做内置的 Namespace、Workload、Pod、Event；不列 CRD，不装 Helm chart。
4. **Nodes / Services / Ingress / ConfigMap / Secret / PVC 浏览**：不在本轮；Secret 尤其不做，避免把敏感值带进 UI。
5. **全局「所有主机总览」**（ROADMAP 中提到的聚合面板）：本轮不做。它会强制对多台主机同时建立 SSH + API 连接，成本与失败面成倍增加；且 v0.4 的目标是先把单集群路径做扎实。建议顺延到 v0.4.1，用「用户手动固定的 3–5 个集群」做轻量聚合，而不是所有在线主机。
6. **多集群同时 watch / 并排对比**：同时只激活一个集群；切换保留标签状态但停止流。
7. **exec 认证插件 / 云厂商 kubeconfig**（EKS/GKE/AKS 的 `exec`、OIDC、SSO）：v0.4 只支持静态证书/token。导入时明确禁用。
8. **端口转发**：需要本地 listener 与生命周期管理，另有安全边界；放到 v0.4.1 候选。
9. **日志聚合**：Workload 级多 Pod 合并日志、`--all-containers` 交错日志、日志持久化/历史检索。v0.4 只做单 Pod 单容器实时流 + 当前缓冲区导出。
10. **调试容器 / ephemeral container / 文件浏览 / 上传下载到 Pod**：不做。
11. **K8s 事件的全文搜索、跨集群事件聚合**：不做；只做当前集群内存中的过滤。
12. **自动添加集群、自动信任证书、自动跳过 TLS 校验**：不做；所有凭据添加与 TLS 例外都需要用户显式操作。
13. **kubeconfig 导出、复制到剪贴板、生成分享链接**：不做。
14. **插件、脚本、自定义资源模板**：不做；单二进制、无插件运行时。
15. **Windows SSH 主机的 K8s 发现**：K3s 不运行在 Windows；探测针对 Linux 主机（macOS 主机不做保证）。
16. **AI 参与 K8s 排障**：v0.5 再评估；本轮不把 Pod 日志/事件发送给任何模型。
17. **大规模多租户集群优化**：v0.4 目标为单机/小型 K3s（约 <2000 Pods）；不做服务端字段裁剪、分页游标、按需 informer、跨集群聚合。更大规模另开性能专项。

---

## 需要用户拍板

| # | 决策 | 推荐 | 备选 / 影响 |
| --- | --- | --- | --- |
| 1 | K8s 是否作为第三个一级视图 | **是**（与终端/数据库并列，全屏） | 若集成到连接树/终端标签，资源表宽度不足，且会污染终端标签模型 |
| 2 | 默认命名空间 | **全部命名空间**（小集群信息更全；选择按集群记住） | 选 `default` 更贴近 kubectl 默认，但用户常在自定义 NS，容易「看不到东西」 |
| 3 | 连接主机时自动探测 K8s | **默认开启，只做存在性检查；首次发现一次性提示** | 若担心打扰，可改为只显示徽标不弹提示；再保守则默认关闭 |
| 4 | ROADMAP 的「全局所有主机总览」 | **顺延到 v0.4.1**，本轮只做单集群浏览 | 若必须本轮做，需要额外设计多主机连接与聚合口径，工期约 +1–2 周 |
| 5 | 不可读 kubeconfig 的免密 sudo | **提供显式按钮 `sudo -n cat`，绝不询问 sudo 密码** | 若安全上不接受任何 sudo，则只给手动指引 |
| 6 | v0.4 是否严格只读 | **是** | 若要加「重启 Pod / 删除 Pod」，必须按危险操作三步并单独评审 |
| 7 | 云厂商 kubeconfig（exec 插件） | **v0.4 不支持**，导入时禁用 | 支持需要处理外部命令执行与凭据刷新，安全面大，建议单列 |
| 8 | 是否顺手支持 RKE2 路径 | **支持探测，不承诺完整验证** | 只做 K3s 可减少测试面；但 RKE2 路径几乎零成本 |

---

## 风险与开放问题（实现前需要确认）

1. **client-go 体积**：新增依赖会让 Windows 单文件明显变大（预计 +8–15MB）。产品定位可接受，但发布说明需更新体积。
2. **SPDY/WebSocket exec 穿过自定义 Dialer**：需要在真实 k3s 上验证 TTY、resize、长连接；若 SPDY 有问题，评估启用 client-go 的 WebSocket executor（需服务端支持）或改用 `attach`。
3. **SSH channel 并发**：watch + 日志 + 终端共用一个 SSH 连接，channel 数量与窗口大小需要压测；日志流设置读缓冲与背压，避免慢 UI 拖垮 SSH。
4. **Event 缓存内存**：全命名空间的事件流可能持续增长；必须按「保留最近 N 条」裁剪，不能只靠无界切片或 informer 默认缓存。
5. **Linux keychain**：无 Secret Service 的桌面环境较常见；「仅本次会话」是诚实降级，但用户体验有落差。是否在 v0.4.1 补主密码回退需用户决定。
6. **Windows 上的 `sigs.k8s.io/yaml` 与路径**：无特殊风险，但仍需 `GOOS=windows go build` 验证。
7. **命名空间权限受限**：全命名空间 watch 403 的降级路径需要在真实受限 kubeconfig 上验证，不能只靠单测。
8. **企业代理**：v0.4 明确不读取系统代理（`rest.Config.Proxy = nil`），避免凭据经代理转发；需要代理才能到达云 apiserver 的用户会连接失败。这是有意的安全取舍，发布说明需写清。
9. **k3s 证书 SAN 的例外**：若用户自定义了 `--tls-san` 且移除了 `127.0.0.1`，隧道方案会失败；此时提示用户使用可匹配的 server 地址/证书，不自动跳过校验。

---

## 参考来源

- k9s：[命令与快捷键](https://k9scli.io/topics/commands/)、[配置/只读模式](https://k9scli.io/topics/config/)、[GitHub README](https://github.com/derailed/k9s)、[Kubezilla cheatsheet](https://kubezilla.io/k9s-cheatsheet/)、[ReleaseRun 参考](https://releaserun.com/k9s-reference/)
- Lens：[Log options 官方文档](https://docs.lenshq.io/k8slens/using-lens/log-options/)、[Lens Desktop 指南](https://lenshq.io/blog/simplify-kubernetes-monitoring-and-management-with-lens-desktop)、[Mirantis 指南](https://www.mirantis.com/blog/simplify-kubernetes-monitoring-and-management-with-lens-desktop/)、[三栏布局笔记](https://notes.darshanraul.me/Kubernetes/guides/tools/lens)、[hashhackers 指南](https://blog.hashhackers.com/blog/lens-kubernetes-guide/)、[定价](https://lenshq.io/pricing/)
- Headlamp：[GitHub](https://github.com/kubernetes-sigs/headlamp)、[Projects 文档](https://headlamp.dev/docs/latest/learn/projects/)、[命名空间权限 issue #7326](https://github.com/kubernetes-sigs/headlamp/issues/7326)、[Kubernetes 官方迁移文](https://kubernetes.io/blog/2026/06/01/dashboard-to-headlamp/)
- kubectl / kubectx：[kubectl logs 参考](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_logs/)、[kubectx/kubens README](https://github.com/ahmetb/kubectx)
- k3s / 隧道：[K3s 环境变量（kubectl symlink）](https://docs.k3s.io/reference/env-variables)、[K3s kubeconfig 与 SAN 实战](https://cloud.tencent.cn/developer/article/2747124)、[StackOverflow 61913810](https://stackoverflow.com/questions/61913810)
- client-go：[PR #112144 Dialer 接口](https://github.com/kubernetes/kubernetes/pull/112144)、[REST 配置说明](https://deepwiki.com/kubernetes/client-go/2.1-rest-configuration)
