# SSH-MCP

**简体中文** | [English](README.en.md)

[![Build installers](https://github.com/ackwrap/ssh-mcp/actions/workflows/build.yml/badge.svg)](https://github.com/ackwrap/ssh-mcp/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/ackwrap/ssh-mcp)](https://github.com/ackwrap/ssh-mcp/releases)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

面向 AI 客户端的 SSH 与 SFTP 工具服务，基于 Go 和 Model Context Protocol（MCP）构建。

通过本地 WebUI 管理连接与凭据，再由 MCP 客户端执行远程命令、传输文件和管理服务器。支持 Windows、macOS 与 Linux，单个可执行文件内置 WebUI，无需 Python、Node.js 或独立前端服务。

[下载安装包](https://github.com/ackwrap/ssh-mcp/releases) · [完整使用说明](USAGE.md) · [问题反馈](https://github.com/ackwrap/ssh-mcp/issues)

## 功能概览

- **远程操作**：提供 16 个 MCP 工具，覆盖命令执行、批量执行、连接测试、文件传输和目录操作。
- **灵活连接**：支持直连、多级跳板链、SOCKS5 和 HTTP CONNECT 代理，以及密码、私钥文件、粘贴私钥和私钥口令认证。
- **本地管理**：WebUI 支持主密码登录、锁定、改密、导入导出、服务器分组与启用/停用管理。
- **凭据保护**：配置使用 Fernet 加密，采用 PBKDF2-HMAC-SHA256 派生密钥；通过文件锁、revision 校验和原子写入保护配置更新。
- **客户端接入**：内置 OpenCode、Claude Code、Codex、Kimi Code、ZCode、Cursor、VS Code 和 Claude Desktop 的检测与接入适配器。
- **传输与运行方式**：支持 stdio 和 Streamable HTTP MCP；SSH 连接复用，上传使用每文件 1 MiB 本地读缓存和有界流水线写入，保留兼容的 32 KiB SFTP 分包。

自动接入的可用性取决于客户端的安装方式和平台。工具列表默认全部不勾选，请主动选择并检查 WebUI 展示的工具与配置路径；接入或取消接入前会备份需要修改的配置文件。

OpenCode 一键接入通过 `--global` 写入用户全局配置，对所有项目生效，不修改当前项目的 `opencode.json(c)`。

## 平台与安装

从 [GitHub Releases](https://github.com/ackwrap/ssh-mcp/releases) 下载对应安装包。

| 平台 | 安装包 | 程序位置 / 启动方式 |
| --- | --- | --- |
| Windows x64 | `SSH-MCP-Setup-<版本>-windows-amd64.exe` | 开始菜单 **SSH-MCP**；或安装目录中的 `ssh-mcp.exe` |
| macOS Intel / Apple Silicon | `SSH-MCP-Setup-<版本>-macos-universal.pkg` | `/usr/local/bin/ssh-mcp` |
| Debian / Ubuntu Linux x64 | `SSH-MCP-Setup-<版本>-linux-amd64.deb` | `/usr/bin/ssh-mcp` |

Linux 安装示例（将文件名替换为下载的版本）：

```bash
sudo dpkg -i ./SSH-MCP-Setup-0.1.4-linux-amd64.deb
```

**平台差异：** Windows 提供原生托盘、单实例管理和可选开机自启。macOS/Linux 不提供原生托盘或自动启动服务，管理服务在终端前台运行，按 Ctrl+C 退出。三个平台均支持 stdio、WebUI 和 Streamable HTTP MCP。

**签名状态：** 当前 Windows 安装包未配置商业代码签名，macOS 安装包未进行 Developer ID 签名和公证，系统可能显示安全提示。

## 快速开始

### 1. 初始化本地配置

Windows 从开始菜单启动 **SSH-MCP**。macOS/Linux 在终端运行：

```bash
ssh-mcp --open
```

打开 `http://127.0.0.1:8877`，设置主密码并添加服务器。无桌面环境时使用 `ssh-mcp --tray` 启动服务，再通过本机浏览器访问 WebUI。

### 2. 连接 MCP 客户端

可在 WebUI 的 **AI 工具接入** 页面检测客户端并配置接入，也可手动配置 stdio MCP。以下示例适用于采用 `mcpServers` 配置格式的客户端：

```json
{
  "mcpServers": {
    "ssh-mcp": {
      "command": "/usr/local/bin/ssh-mcp"
    }
  }
}
```

- macOS 使用 `/usr/local/bin/ssh-mcp`。
- Linux 使用 `/usr/bin/ssh-mcp`。
- Windows 使用实际安装的 `ssh-mcp.exe` 完整路径，JSON 中的反斜杠需要转义，例如 `C:\\path\\to\\ssh-mcp.exe`。
- 不同客户端的顶层配置结构可能不同，具体步骤见 [USAGE.md](USAGE.md)。

**不传参数即运行 stdio MCP**，无需附加 `--mcp`。客户端会启动独立子进程；不要在 stdio 配置中使用 `--tray` 或 `--open`。初次配置或重新解锁凭据时使用 WebUI。

### 3. 可选：使用 HTTP MCP

保持本地管理服务运行，将支持 Streamable HTTP 的客户端连接到：

```text
http://127.0.0.1:8877/mcp/
```

健康检查地址为 `http://127.0.0.1:8877/api/health`。

## 运行参数

| 参数 | 行为 |
| --- | --- |
| 无参数 | 运行 stdio MCP 服务器 |
| `--mcp` | 兼容旧配置的 stdio 模式 |
| `--tray` | 启动 WebUI / HTTP MCP；Windows 使用托盘，macOS/Linux 前台运行 |
| `--open` | 启动管理服务，并尝试打开浏览器 |
| `--version` / `-v` | 显示版本并退出 |

`--mcp`、`--tray` 和 `--open` 不能组合使用。Windows 已运行的托盘服务会被复用；stdio 子进程彼此独立。

## MCP 工具

| 类别 | 工具 |
| --- | --- |
| 连接管理 | `ssh_list_servers`、`ssh_add_server`、`ssh_update_server`、`ssh_delete_server`、`ssh_add_jumphost`、`ssh_update_jumphost`、`ssh_delete_jumphost` |
| 远程执行 | `ssh_exec`、`ssh_exec_multi`、`ssh_test_connection` |
| 文件操作 | `ssh_upload`、`ssh_download`、`ssh_read_file`、`ssh_write_file`、`ssh_list_dir`、`ssh_stat` |

停用的服务器不会出现在 MCP 列表中，也不能被 MCP 操作；启用/停用仅在 WebUI 管理。服务器列表返回分组信息，但不返回密码、私钥等凭据。服务器、跳板机和代理地址默认隐藏，只有手动关闭 WebUI 中的 **隐藏 IP** 开关后才允许列表返回真实地址。

命令的 stdout/stderr 各有 4 MiB 上限；`ssh_read_file` / `ssh_write_file` 的文本内容上限为 4 MiB。该文本限制不适用于流式 `ssh_upload` / `ssh_download`。完整参数与限制见 [使用说明](USAGE.md)。

## 安全与数据

> WebUI、`/api/*` 与 `/mcp/` 仅供本机使用，只监听 `127.0.0.1`。不要通过端口转发或反向代理将其暴露到局域网或公网。

- **主机密钥**：默认不校验 SSH 主机密钥。生产环境建议设置 `SSH_MCP_STRICT_HOST_KEY=1`，并维护可信的 `known_hosts`。
- **配置加密**：使用 PBKDF2-HMAC-SHA256（300,000 次迭代）派生 32 字节密钥，再通过 Fernet 加密配置，兼容旧版数据格式。
- **解锁状态**：解锁后，本机 `key.bin` 缓存派生密钥。请保护本机账户和数据目录；通过 WebUI 锁定会删除该文件、撤销会话并关闭缓存连接。
- **备份与升级**：升级前建议导出完整加密配置。不要同时运行共享同一数据目录的旧版和新版，也不要将密钥、主密码或完整配置导出提交到代码仓库。

默认数据目录：

| 平台 | 目录 |
| --- | --- |
| Windows | `%LOCALAPPDATA%\ssh_mcp\` |
| macOS / Linux | `~/ssh_mcp/` |

主要文件为 `config.json`（加密配置）、`key.bin`（解锁密钥）和 `session.secret`（WebUI 会话密钥）。卸载程序时应保留用户数据，除非明确决定删除配置。

### 环境变量

| 变量 | 用途 |
| --- | --- |
| `SSH_MCP_PORT` | WebUI / HTTP MCP 端口，默认 `8877` |
| `SSH_MCP_PASSWORD` | stdio 模式无法读取缓存密钥时使用的主密码；避免长期保存在环境中 |
| `SSH_MCP_STRICT_HOST_KEY` | 设为 `1` 启用严格主机密钥验证 |
| `SSH_MCP_KNOWN_HOSTS` | 严格模式下附加的 `known_hosts` 文件路径 |

## 从源码构建

需要 **Go 1.25+**。WebUI 资源通过 `embed` 打包，无需 npm 或前端构建步骤。

### Windows

需要 Inno Setup 6；缺少时脚本会尝试通过 `winget` 安装。

```powershell
.\build.ps1
```

产物为 `dist\ssh-mcp.exe` 和 `dist\installer\SSH-MCP-Setup-<版本>.exe`。只构建程序时使用 `-SkipInstaller`。CI 发布的 Windows 安装包文件名另加 `-windows-amd64` 后缀。

### macOS / Linux

macOS 需要 Xcode Command Line Tools；Debian/Ubuntu 需要 `dpkg-deb`。在对应系统执行：

```bash
bash ./build.sh
```

产物为 `dist/ssh-mcp` 和 `dist/installer/` 中的安装包。macOS 合并 amd64/arm64 为通用二进制；Linux 按本机 amd64/arm64 架构生成 `.deb`。

### 开发检查

```bash
go fmt ./...
go test -p 1 ./...
go vet ./...
go mod verify
```

## GitHub Actions

[构建工作流](.github/workflows/build.yml) 在三个系统的原生 runner 上测试、打包并上传产物：

- **分支推送 / Pull Request / 手动运行**：构建三个安装包，保存在运行页面的 **Artifacts** 中，保留 14 天。
- **推送 `v<版本>` 标签**：三平台构建成功后发布 GitHub Release，附带三个安装包和 `SHA256SUMS.txt`。
- **版本校验**：标签必须与 `internal/mcpserver/server.go` 的版本一致，安装包版本从构建后的程序读取。
- **权限边界**：普通构建只使用仓库读取权限，只有发布任务拥有 `contents: write`。

例如，提交并推送版本 `0.1.4` 的代码后：

```bash
git tag -a v0.1.4 -m "Release v0.1.4"
git push origin v0.1.4
```

## 文档与反馈

- [使用说明](USAGE.md)：初始化、客户端接入、迁移、备份与故障排查。
- [维护约定](AGENTS.md)：项目结构、兼容性边界与验证要求。
- [GitHub Issues](https://github.com/ackwrap/ssh-mcp/issues)：报告缺陷或提出功能建议，请勿附带真实凭据。

## 许可证

本项目采用 [MIT License](LICENSE)。
