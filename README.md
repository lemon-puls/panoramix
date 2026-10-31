# Panorama

> 一体化开发者运维客户端 —— SSH 终端 · 系统监控 · 文件传输 · 数据库 · Redis · K3s

**English**: Panorama is an all-in-one open-source desktop client for developers — SSH terminal,
server monitoring, file transfer, MySQL, Redis and K3s, built with Go + Wails + Vue 3.
Currently pre-release, under active development.

## 特性路线

| 模块 | 状态 |
| --- | --- |
| SSH 终端（多标签、TOFU 指纹、keepalive、自动重连、会话日志、命令片段） | 🚧 v0.1 开发中 |
| 连接管理（分组树、搜索、克隆、Xshell 导入） | 🚧 v0.1 开发中 |
| SFTP 文件传输（并发队列、覆盖策略、递归） | 🚧 v0.1 开发中 |
| 系统监控（`/proc` 采集、GPU 探测） | 📅 v0.2 |
| MySQL + Redis 管理 | 📅 v0.3 |
| K3s/K8s 面板（经 SSH 自动发现） | 📅 v0.4 |
| AI 运维 | 💡 v0.5+ |

完整路线与验收标准见 [ROADMAP.md](ROADMAP.md)，架构设计见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 本地开发

依赖：Go ≥ 1.26（go.mod 的 toolchain 指令会自动拉取 1.27）、Node ≥ 20、Wails v2 CLI。

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest

wails dev      # 开发模式（热重载）
wails build    # 构建产物 build/bin/Panorama.exe
```

注意：Windows 产物需在 Windows 上执行 `wails build`（WSL 内可用于跑单元测试与
`GOOS=windows go build` 编译校验）。

## 下载与安装

v0.1 尚未发布。发布后提供 Windows 便携版（单 exe + zip）。

> **SmartScreen 提示"未知发布者"属正常现象**：v0.x 阶段未购买代码签名证书，
> 点击「更多信息 → 仍要运行」即可；v1.0 正式版会启用签名。

## 安全说明

- 密码 / 私钥 passphrase 存放在操作系统凭据库（Windows Credential Manager），不落明文
- 主机指纹采用 TOFU（首次信任）策略，兼容 OpenSSH known_hosts 格式
- v0.x 完全零遥测、零数据上报

## 许可证

[MIT](LICENSE)
