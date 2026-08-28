# SSH-MCP Go Agent Guide

本文档用于指导在本仓库中工作的开发者和 AI Agent。修改代码前应先理解兼容性、安全边界和验证要求。

## 项目目标

本项目是 `C:\Users\xiaobai\Desktop\ssh_mcp` Python 项目的原生 Go 迁移版。

核心目标：

- 不依赖 Python、uv 或 PyInstaller 即可运行。
- 保留原 WebUI、HTTP API 和 MCP 工具契约。
- 直接读取 Python 版已有的加密配置。
- 提供 Windows 托盘、stdio MCP 和 Streamable HTTP MCP。
- 支持 SSH、SFTP、跳板链、SOCKS5 和 HTTP CONNECT。

## 技术栈

| 用途 | 实现 |
| --- | --- |
| 语言 | Go 1.25+ |
| MCP | `github.com/modelcontextprotocol/go-sdk` |
| SSH | `golang.org/x/crypto/ssh` |
| SFTP | `github.com/pkg/sftp` |
| Fernet | `github.com/fernet/fernet-go` |
| 托盘 | `github.com/getlantern/systray` |
| 文件锁 | `github.com/gofrs/flock` |
| HTTP | Go 标准库 `net/http` |
| 前端 | 原生 HTML、CSS、JavaScript，通过 `embed` 打包 |

## 目录结构

| 路径 | 职责 |
| --- | --- |
| `main.go` | CLI 参数解析及 stdio MCP/托盘模式分发 |
| `internal/config` | 配置模型、校验、加密存储、CRUD、导入导出 |
| `internal/secure` | PBKDF2、Fernet、密钥文件和会话密钥 |
| `internal/sshclient` | SSH、SFTP、连接池、跳板和代理 |
| `internal/mcpserver` | 16 个 MCP 工具及共享运行时 |
| `internal/web` | WebUI API、认证、静态资源和 HTTP MCP |
| `internal/web/ui` | 从 Python 项目复用的 WebUI |
| `internal/tray` | Windows 托盘、单实例和开机自启 |
| `build.ps1` | 测试并构建 Windows 应用及 Inno Setup 安装包 |
| `installer.iss` | 安装目录、快捷方式、开机自启和卸载策略 |
| `USAGE.md` | 面向用户的完整使用说明 |

## 必须保持的兼容性

### 数据目录

不得随意修改以下默认路径：

```text
%LOCALAPPDATA%\ssh_mcp\config.json
%LOCALAPPDATA%\ssh_mcp\key.bin
%LOCALAPPDATA%\ssh_mcp\session.secret
```

### 加密格式

必须继续兼容 Python 版格式：

```text
PBKDF2-HMAC-SHA256
迭代次数：300000
派生密钥：32 字节
业务配置：Fernet 加密
```

`config.json` 的顶层字段为：

```json
{
  "version": 1,
  "revision": 1,
  "salt": "<hex>",
  "master_hash": "<hex>",
  "payload": "<Fernet token>"
}
```

修改 `internal/secure` 或 `internal/config` 时，必须保留 Python Fernet 兼容测试。

### API 和 MCP

不得在没有明确迁移方案的情况下删除、重命名或改变现有 HTTP API。

必须保留以下 16 个 MCP 工具名：

```text
ssh_list_servers
ssh_add_server
ssh_update_server
ssh_delete_server
ssh_add_jumphost
ssh_update_jumphost
ssh_delete_jumphost
ssh_exec
ssh_exec_multi
ssh_test_connection
ssh_upload
ssh_download
ssh_read_file
ssh_write_file
ssh_list_dir
ssh_stat
```

修改工具参数、输出结构或错误语义时，应同步更新测试、README 和 USAGE。

### WebUI

`internal/web/ui` 是从参考项目复用的无构建前端。除非需求明确，不要引入 Node.js、npm 或前端构建系统。

修改前端请求路径时必须同步修改 `internal/web/server.go`。WebUI 必须保持桌面和移动端可用。

## 安全边界

- HTTP 服务只能绑定 `127.0.0.1`。
- 不得记录主密码、服务器密码、私钥、私钥口令或代理密码。
- MCP 列表接口不得返回任何凭据材料。
- WebUI 返回凭据时必须使用 `******` 掩码。
- 锁定和退出必须删除 `key.bin` 并轮换会话密钥。
- 忘记密码流程必须要求准确输入 `DELETE`。
- 配置写入必须使用文件锁、revision CAS 和原子替换。
- 默认主机密钥策略与旧版兼容；严格模式必须继续支持 `known_hosts`。
- 不得将 WebUI、`/api/*` 或 `/mcp/` 暴露到局域网或公网。

## 实现约定

- 优先使用 Go 标准库和当前已有依赖。
- 选择最小且直接的实现，不增加没有实际需求的抽象层。
- 不添加 Python 运行时依赖。
- 不在仓库中保存真实服务器地址、用户名、密码、私钥或环境配置。
- 所有外部输入都必须经过边界和类型校验。
- SSH 命令输出和远程文件读写必须保留大小限制。
- 新增后台 goroutine 时必须有明确退出路径，避免连接和文件句柄泄漏。
- Windows 专用实现使用 build tag，保证非 Windows 下 `--mcp` 仍可编译运行。
- 修改连接配置后必须关闭旧连接池，不能按服务器名称错误复用旧连接。
- Windows 托盘和 Streamable HTTP MCP 必须保持全系统单实例，使用 `Local\SSH-MCP-Singleton` 命名互斥量。
- stdio `--mcp` 不得使用全局单例；每个 MCP 客户端需要独立子进程和独立标准输入输出通道。

## 开发命令

格式化：

```powershell
go fmt ./...
```

整理依赖：

```powershell
go mod tidy
go mod verify
```

静态检查：

```powershell
go vet ./...
```

测试：

```powershell
go test -p 1 ./...
```

Windows 环境中使用 `-p 1`，避免安全软件并发扫描临时测试程序时产生文件占用错误。

构建：

```powershell
.\build.ps1
```

产物：

```text
dist\ssh-mcp.exe
dist\installer\SSH-MCP-Setup-<version>.exe
```

## 测试要求

涉及配置或加密时必须覆盖：

- 加密存储不出现明文凭据。
- Python Fernet token 可由 Go 解密。
- 掩码更新保留旧凭据。
- 错误密码不能导入配置。
- 过期 revision 不能覆盖新配置。
- 名称冲突、无效引用和跳板环路被拒绝。

涉及 MCP 时必须覆盖：

- 16 个工具清单完全一致。
- stdio MCP 可以初始化和列出工具。
- Streamable HTTP MCP 可以初始化和列出工具。
- MCP 列表输出不包含凭据。

涉及 SSH 时必须覆盖：

- 命令执行和退出状态。
- SFTP 读写、列目录和 stat。
- 跳板链。
- SOCKS5 代理。
- HTTP CONNECT 代理。
- 连接池复用、失活清理和关闭行为。
- 输出、超时和文件大小限制。

涉及 WebUI 时必须覆盖：

- 初始化、登录、锁定、改密和忘记密码。
- 未登录访问返回 401。
- revision 冲突返回 409。
- 服务器和跳板机 CRUD。
- 静态资源和 MCP 配置生成。

## 完成标准

修改只有在以下条件全部满足后才能视为完成：

```powershell
go fmt ./...
go test -p 1 ./...
go vet ./...
go mod verify
.\build.ps1
```

还必须确认：

- `dist\ssh-mcp.exe --version` 输出正确版本。
- 没有把测试配置写入真实 `%LOCALAPPDATA%\ssh_mcp`。
- 没有启动或覆盖参考 Python 项目的运行数据。
- README、USAGE 和行为保持一致。

## 安装器约定

- 开始菜单主快捷方式、桌面快捷方式和安装完成页必须使用 `ssh-mcp.exe --open`。
- 开机自启保持无参数启动，只启动托盘，避免每次登录 Windows 都弹出浏览器。
- 安装器使用当前用户权限安装到 `{localappdata}\Programs\SSH-MCP`。
- 卸载时不得默认删除 `%LOCALAPPDATA%\ssh_mcp`，避免用户配置丢失。
- 安装包版本必须从构建后的 `ssh-mcp.exe --version` 获取，不能与应用版本不一致。
