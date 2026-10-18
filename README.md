# SSH-MCP (Go)

这是 `C:\Users\xiaobai\Desktop\ssh_mcp` 的原生 Go 迁移版。功能、WebUI API、MCP 工具名和本地数据目录保持兼容，不再需要 Python、uv 或 PyInstaller。

当前版本：**0.1.3**。

完整使用说明见 [`USAGE.md`](USAGE.md)，项目维护约定见 [`AGENTS.md`](AGENTS.md)。

源码仓库：<https://github.com/ackwrap/ssh-mcp>。安装包见 [GitHub Releases](https://github.com/ackwrap/ssh-mcp/releases)。

## 安装包与平台支持

| 平台 | 自动构建的安装包 | 安装后启动 |
| --- | --- | --- |
| Windows x64 | `SSH-MCP-Setup-<版本>-windows-amd64.exe` | 开始菜单 SSH-MCP，或 `ssh-mcp.exe --open` |
| macOS Intel / Apple Silicon | `SSH-MCP-Setup-<版本>-macos-universal.pkg` | 终端运行 `/usr/local/bin/ssh-mcp --open` |
| Linux x64（Debian / Ubuntu） | `SSH-MCP-Setup-<版本>-linux-amd64.deb` | 终端运行 `/usr/bin/ssh-mcp --open` |

三平台都支持 stdio MCP、WebUI 和 Streamable HTTP MCP。只有 Windows 提供原生托盘与开机自启；macOS/Linux 的 `--tray` / `--open` 在前台运行本地服务，后者尝试打开浏览器，按 Ctrl+C 退出。无桌面的 Linux 可运行 `ssh-mcp --tray`，再在本机浏览器访问 `http://127.0.0.1:8877`。

安装包目前未配置 Windows 商业签名或 macOS Developer ID 签名/公证，系统可能显示安全提示。安装不会自动启动服务，也不会删除已有用户配置。

## 已迁移功能

- Windows 托盘、单实例、打开/重启 WebUI、开机自启
- 原 WebUI 前端直接复用并嵌入单个可执行文件
- 主密码登录、锁定、改密、忘记密码、会话撤销
- 服务器和跳板机增删改查、复制、导入导出
- 自动检测 OpenCode、Claude Code、Codex、Kimi Code、ZCode、Cursor、VS Code 和 Claude Desktop，通过独立 stdio 子进程一键接入或取消接入
- 直连、链式多跳板、SOCKS5、HTTP CONNECT
- 密码、粘贴私钥、私钥文件及私钥口令认证
- 命令执行、批量执行、连接测试和 SFTP 文件操作；大文件上传使用有界流水线写入
- stdio MCP 与 Streamable HTTP MCP，共 16 个同名工具
- 配置 revision/CAS、跨进程文件锁及原子写入

## 数据兼容

Go 版继续使用：

```text
%LOCALAPPDATA%\ssh_mcp\config.json
%LOCALAPPDATA%\ssh_mcp\key.bin
%LOCALAPPDATA%\ssh_mcp\session.secret
```

macOS/Linux 默认使用 `~/ssh_mcp/` 中的同名文件；不更改原有加密格式和数据路径。

加密格式仍为 PBKDF2-HMAC-SHA256（300,000 次）派生 32 字节密钥，再使用 Fernet 加密完整配置。因此 Go 版可以直接读取 Python 版已有配置和完整加密导出，无需迁移脚本。

首次切换前仍建议从旧版 WebUI 导出一份完整配置。不要同时运行 Python 托盘版和 Go 托盘版；两者使用同一数据目录和默认端口。

## 开发运行

需要 Go 1.25+。

```powershell
go test -p 1 ./...
go run . --version
go run .
go run . --tray
go run . --open
```

直接运行 `go run .` 默认启动 stdio MCP；`--tray` 启动 WebUI 和 HTTP MCP（Windows 同时显示托盘），`--open` 还会尝试打开 WebUI。macOS/Linux 服务在前台运行。WebUI 默认地址为 `http://127.0.0.1:8877`。

## 构建

### Windows

```powershell
.\build.ps1
```

默认同时生成单文件应用和安装包：

```text
dist\ssh-mcp.exe
dist\installer\SSH-MCP-Setup-0.1.3.exe
```

只构建应用、不编译安装包：

```powershell
.\build.ps1 -SkipInstaller
```

安装器依赖 Inno Setup 6；缺少时构建脚本会尝试通过 `winget` 安装。开始菜单、桌面快捷方式和安装完成页都使用 `ssh-mcp.exe --open`，启动托盘后直接打开 WebUI；开机自启使用 `ssh-mcp.exe --tray`。构建产物为单文件 Windows GUI 程序，前端资源已通过 `embed` 打包；默认 stdio 模式仍通过继承的标准输入输出管道工作。

### macOS / Linux

在对应系统中运行（macOS 需要 Xcode Command Line Tools，Debian/Ubuntu 需要 `dpkg-deb`）：

```bash
bash ./build.sh
```

脚本先测试、静态检查并校验依赖，再生成 `dist/ssh-mcp` 和 `dist/installer/` 中的原生安装包。macOS 合并 amd64/arm64 为通用二进制；Linux 按本机 amd64/arm64 架构生成 `.deb`。安装包版本从构建后的程序读取。

### GitHub Actions

工作流为 [`.github/workflows/build.yml`](.github/workflows/build.yml)：

- 分支推送、Pull Request、手动 **Run workflow**：在 Windows、macOS、Linux 原生 runner 上测试并构建，三个安装包保存在运行页面的 **Artifacts**，保留 14 天。
- 推送 `v<版本>` 标签：三个平台全部成功后自动发布 GitHub Release，附带安装包和 `SHA256SUMS.txt`。标签必须与 `internal/mcpserver/server.go` 中的应用版本一致，否则构建失败。
- 普通构建仅使用仓库只读权限；只有标签发布任务拥有 `contents: write`，无需另配个人访问令牌。首次使用需先将工作流提交、推送到 GitHub，并确保仓库启用 Actions。

例如当前版本为 `0.1.3`，提交并推送代码后可发布：

```bash
git tag v0.1.3
git push origin v0.1.3
```

## 参数

| 参数 | 用途 |
| --- | --- |
| `--version` / `-v` | 显示版本 |
| 无参数 | 运行 stdio MCP 服务器（默认） |
| `--mcp` | 运行 stdio MCP 服务器（兼容旧配置） |
| `--tray` | 启动 WebUI / HTTP MCP；Windows 使用托盘，macOS/Linux 前台运行 |
| `--open` | 同上，并尝试打开 WebUI |

`--mcp`、`--tray` 和 `--open` 不能同时使用，未知参数会被拒绝。

## 环境变量

| 环境变量 | 用途 |
| --- | --- |
| `SSH_MCP_PORT` | WebUI 和 HTTP MCP 端口，默认 `8877` |
| `SSH_MCP_PASSWORD` | stdio MCP 无法读取 `key.bin` 时使用的主密码 |
| `SSH_MCP_STRICT_HOST_KEY` | 设为 `1` 启用 SSH 主机密钥严格验证 |
| `SSH_MCP_KNOWN_HOSTS` | 严格模式附加的 `known_hosts` 文件 |

## MCP 端点

- stdio：`ssh-mcp.exe`（旧配置可继续使用 `ssh-mcp.exe --mcp`）
- Streamable HTTP：先用 `ssh-mcp.exe --tray` 启动托盘服务，再连接 `http://127.0.0.1:<端口>/mcp/`
- 健康检查：`http://127.0.0.1:<端口>/api/health`

macOS/Linux 使用安装后的 `ssh-mcp`，不带 `.exe`；通过 `ssh-mcp --tray` 或 `ssh-mcp --open` 启动管理服务。

HTTP 服务仅绑定 `127.0.0.1`。不要通过端口转发或反向代理将 WebUI、`/api/*` 或 `/mcp/` 暴露到局域网或公网。

## MCP 工具

| 类别 | 工具 |
| --- | --- |
| 管理 | `ssh_list_servers` `ssh_add_server` `ssh_update_server` `ssh_delete_server` `ssh_add_jumphost` `ssh_update_jumphost` `ssh_delete_jumphost` |
| 执行 | `ssh_exec` `ssh_exec_multi` `ssh_test_connection` |
| 文件 | `ssh_upload` `ssh_download` `ssh_read_file` `ssh_write_file` `ssh_list_dir` `ssh_stat` |

`ssh_list_servers` 返回服务器的 `group` 分组，默认将服务器、跳板机及代理的 `host` 显示为 `******`，不返回凭据。在 WebUI“设置 → MCP 地址隐私”中，只有手动关闭默认开启的“隐藏 IP”开关，MCP 列表才会返回真实 IP 或域名；重新开启后，后续列表请求恢复隐藏，无需重启 MCP。设置会持久保存，旧配置默认隐藏，导入配置不会改变本机隐私设置。

WebUI 按分组展示服务器，支持折叠、筛选与启用/停用；停用的服务器不会出现在 MCP 列表中，MCP 也无法执行、读写、修改或删除它们。启用/停用只能在 WebUI 管理，旧配置默认启用且未分组。WebUI 仍显示真实地址供管理，SSH 连接使用原始地址。

## 主要依赖

- MCP：官方 `github.com/modelcontextprotocol/go-sdk`
- SSH：`golang.org/x/crypto/ssh`
- SFTP：`github.com/pkg/sftp`
- Fernet：`github.com/fernet/fernet-go`
- 托盘：`github.com/getlantern/systray`
- 文件锁：`github.com/gofrs/flock`

## SSH 主机密钥

默认行为与原项目一致：自动接受未知主机密钥。设置 `SSH_MCP_STRICT_HOST_KEY=1` 后，程序加载用户目录下的 `.ssh\known_hosts` 以及 `SSH_MCP_KNOWN_HOSTS` 指定文件，并拒绝未知或已变化的主机密钥。

## License

MIT
