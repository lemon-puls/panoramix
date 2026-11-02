# Panoramix

> 一体化开发者运维客户端 —— SSH 终端 · 系统监控 · 文件传输 · 数据库 · Redis · K3s

**English**: Panoramix is an all-in-one open-source desktop client for developers — SSH terminal,
server monitoring, file transfer, MySQL, Redis and K3s, built with Go + Wails + Vue 3.
Currently pre-release, under active development.

## 特性路线

| 模块 | 状态 |
| --- | --- |
| SSH 终端（多标签、**分屏**、TOFU、keepalive、自动重连、会话日志、命令片段、登录脚本） | ✅ v0.2 |
| 连接管理（分组树、搜索、克隆、**跳板链**、**SSH Agent**） | ✅ v0.2 |
| 迁移导入（**Xshell** 与 **FinalShell** 批量导入） | ✅ v0.2 |
| SFTP 文件传输（**双面板**、断点续传、暂停/限速、**远程编辑**、拖拽互传、**cwd 联动**） | ✅ v0.2 |
| 系统监控（`/proc` 采集、GPU 探测、实时曲线） | ✅ v0.2 |
| MySQL + Redis 管理 | 📅 v0.3 |
| K3s/K8s 面板（经 SSH 自动发现） | 📅 v0.4 |
| AI 运维 | 💡 v0.5+ |

界面支持 **简体中文 / English** 切换（侧栏 ⚙）。

完整路线与验收标准见 [ROADMAP.md](ROADMAP.md)，架构设计见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 下载

前往 [Releases](https://github.com/lemon-puls/panoramix/releases) 下载 Windows 便携版
（单 exe + zip，解压即用）。国内访问可经 Gitee 镜像阅读源码（镜像地址见下）。

## 从 Xshell / FinalShell 迁移

连接管理器工具栏「从 Xshell 导入」支持两种来源批量迁移：

| 来源 | 配置目录 |
| --- | --- |
| Xshell | `%USERPROFILE%\Documents\NetSarang\Xshell\Sessions`（`*.xsh`） |
| FinalShell | `%APPDATA%\finalshell\conn`（`*_connect_config.json`） |

> **重要**：仅迁移 名称 / 主机 / 端口 / 用户名 / 认证方式。两家的密码均在本机
> 加密存储、密钥为专有格式，物理上不可解密迁移——导入的主机首次连接时会弹
> 出凭据补录窗口，填写后立即重连。

## 本地开发

依赖：Go ≥ 1.26（go.mod 的 toolchain 指令会自动拉取 1.27）、Node ≥ 20、Wails v2 CLI。

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest

wails dev      # 开发模式（热重载）
wails build    # 构建产物 build/bin/Panoramix.exe
```

注意：Windows 产物需在 Windows 上执行 `wails build`（WSL 内可用于跑单元测试与
`GOOS=windows go build` 编译校验）。

> **跨平台共用此目录（如 Windows + WSL）时**：`node_modules` 里的原生二进制
> （rollup/esbuild 等）是平台相关的，两侧共用一份会互相覆盖。原则：**哪个平台
> 构建前端，就先在那个平台 `npm install`**，构建完不要立刻去另一个平台跑前端
> 构建（Go 侧的 vet/test/build 不受影响）。CI 在独立环境安装，无此问题。
> 另外 vite dev 固定端口 5240（`frontend/vite.config.ts`），避开默认 5173 的冲突。

## 下载与安装

v0.1 尚未发布。发布后提供 Windows 便携版（单 exe + zip）。

> **SmartScreen 提示"未知发布者"属正常现象**：v0.x 阶段未购买代码签名证书，
> 点击「更多信息 → 仍要运行」即可；v1.0 正式版会启用签名。

## 安全说明

- 密码 / 私钥 passphrase 存放在操作系统凭据库（Windows Credential Manager），不落明文
- 主机指纹采用 TOFU（首次信任）策略，兼容 OpenSSH known_hosts 格式
- 断点续传按文件大小判断断点（与 WinSCP 一致）；覆盖已有文件时强制整文件重传
- v0.x 完全零遥测、零数据上报

## 镜像与许可

- GitHub 主仓库：<https://github.com/lemon-puls/panoramix>（Gitee 镜像仅同步源码，Issue/PR 请走 GitHub）
- [MIT](LICENSE)
