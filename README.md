# SSH-MCP (Go)

这是 `C:\Users\xiaobai\Desktop\ssh_mcp` 的原生 Go 迁移版。功能、WebUI API、MCP 工具名和本地数据目录保持兼容，不再需要 Python、uv 或 PyInstaller。

当前版本：**0.1.3**。

完整使用说明见 [`USAGE.md`](USAGE.md)，项目维护约定见 [`AGENTS.md`](AGENTS.md)。

## 已迁移功能

- Windows 托盘、单实例、打开/重启 WebUI、开机自启
- 原 WebUI 前端直接复用并嵌入单个可执行文件
- 主密码登录、锁定、改密、忘记密码、会话撤销
- 服务器和跳板机增删改查、复制、导入导出
- 自动检测 OpenCode、Claude Code、Codex、Kimi Code、ZCode、Cursor、VS Code 和 Claude Desktop，通过独立 stdio 子进程一键接入或取消接入
- 直连、链式多跳板、SOCKS5、HTTP CONNECT
- 密码、粘贴私钥、私钥文件及私钥口令认证
- 命令执行、批量执行、连接测试和 SFTP 文件操作
- stdio MCP 与 Streamable HTTP MCP，共 16 个同名工具
- 配置 revision/CAS、跨进程文件锁及原子写入

## 数据兼容

Go 版继续使用：

```text
%LOCALAPPDATA%\ssh_mcp\config.json
%LOCALAPPDATA%\ssh_mcp\key.bin
%LOCALAPPDATA%\ssh_mcp\session.secret
```

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

直接运行 `go run .` 默认启动 stdio MCP；`--tray` 启动托盘、WebUI 和 HTTP MCP，`--open` 还会在服务就绪后打开 WebUI。WebUI 默认地址为 `http://127.0.0.1:8877`。

## 构建

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

## 参数

| 参数 | 用途 |
| --- | --- |
| `--version` / `-v` | 显示版本 |
| 无参数 | 运行 stdio MCP 服务器（默认） |
| `--mcp` | 运行 stdio MCP 服务器（兼容旧配置） |
| `--tray` | 启动或连接托盘实例 |
| `--open` | 启动或连接托盘实例并打开 WebUI |

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

HTTP 服务仅绑定 `127.0.0.1`。不要通过端口转发或反向代理将 WebUI、`/api/*` 或 `/mcp/` 暴露到局域网或公网。

## MCP 工具

| 类别 | 工具 |
| --- | --- |
| 管理 | `ssh_list_servers` `ssh_add_server` `ssh_update_server` `ssh_delete_server` `ssh_add_jumphost` `ssh_update_jumphost` `ssh_delete_jumphost` |
| 执行 | `ssh_exec` `ssh_exec_multi` `ssh_test_connection` |
| 文件 | `ssh_upload` `ssh_download` `ssh_read_file` `ssh_write_file` `ssh_list_dir` `ssh_stat` |

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
