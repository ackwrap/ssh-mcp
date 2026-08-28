# SSH-MCP Go 使用说明

本文档说明 SSH-MCP Go 版的构建、运行、服务器配置、MCP 接入、数据迁移和常见问题处理。

## 1. 系统要求

直接使用已构建程序时：

- Windows 10 或 Windows 11。
- 可以访问需要管理的 SSH 服务器。
- 不需要安装 Python、uv 或 Node.js。

从源码构建时：

- Go 1.25 或更高版本。
- PowerShell 7 或 Windows PowerShell。

## 2. 获取程序

推荐使用安装包：

```text
C:\Users\xiaobai\Desktop\ssh-mcp2\dist\installer\SSH-MCP-Setup-0.1.0.exe
```

运行安装包后，可以选择创建桌面快捷方式和登录 Windows 时自动启动托盘。开始菜单、桌面快捷方式及安装完成页都会使用 `ssh-mcp.exe --open`，因此启动应用时会同时打开 WebUI。

当前安装包未配置商业代码签名证书，Windows SmartScreen 可能显示“未知发布者”。发布给其他用户前建议使用受信任证书对安装包和应用程序签名。

免安装单文件程序位于：

```text
C:\Users\xiaobai\Desktop\ssh-mcp2\dist\ssh-mcp.exe
```

需要重新构建时，在项目目录执行：

```powershell
.\build.ps1
```

脚本会先运行测试，再生成：

```text
dist\ssh-mcp.exe
dist\installer\SSH-MCP-Setup-0.1.0.exe
```

仅构建免安装程序：

```powershell
.\build.ps1 -SkipInstaller
```

查看版本：

```powershell
.\dist\ssh-mcp.exe --version
```

预期输出：

```text
ssh-mcp 0.1.0
```

## 3. 启动方式

### 启动托盘程序

从开始菜单或桌面快捷方式启动时，安装器会自动附加 `--open`。直接运行免安装程序也可以执行：

```powershell
.\dist\ssh-mcp.exe
```

程序会驻留在 Windows 系统托盘，并在本机启动 WebUI 服务。

### 启动并打开 WebUI

```powershell
.\dist\ssh-mcp.exe --open
```

如果托盘实例已经运行，该命令不会启动第二个实例，只会打开已有实例的 WebUI。

Windows 托盘和 Streamable HTTP MCP 使用系统命名互斥量保证只有一个服务实例。重复双击快捷方式或再次执行 `--open` 时，会复用当前服务并打开 WebUI，不会重复监听端口。

默认地址：

```text
http://127.0.0.1:8877
```

### 运行 stdio MCP

```powershell
.\dist\ssh-mcp.exe --mcp
```

该模式通常由 Claude Desktop、Cursor、VS Code 等 MCP 客户端自动启动，不建议直接在终端中交互使用。

stdio MCP 不使用全局单例。每个 MCP 客户端会启动自己的 `--mcp` 子进程，因为每个进程都需要独立的 stdin/stdout 通道；多个 stdio 子进程会通过配置文件锁和 revision 机制安全共享同一份配置。这不代表启动了多个 HTTP MCP 服务。

### 参数限制

`--open` 和 `--mcp` 不能同时使用。未知参数会被拒绝。

## 4. 首次初始化

1. 启动程序并打开 `http://127.0.0.1:8877`。
2. 输入至少 6 位的主密码。
3. 再次输入主密码并确认。
4. 设置成功后进入服务器管理页面。

主密码用于派生配置加密密钥，程序不会保存主密码明文。主密码无法找回，请妥善保管。

首次初始化后会在以下目录生成数据：

```text
%LOCALAPPDATA%\ssh_mcp\
```

主要文件：

| 文件 | 用途 |
| --- | --- |
| `config.json` | Fernet 加密后的服务器和跳板机配置 |
| `key.bin` | 当前用户登录解锁后生成的派生密钥 |
| `session.secret` | WebUI 会话签名密钥 |

## 5. 添加服务器

在 WebUI 的“服务器”页面点击“新增服务器”。

需要填写：

| 字段 | 说明 |
| --- | --- |
| 名称 | 服务器唯一名称，不能包含 `/` 或 `\` |
| 主机 | IP 地址或域名 |
| 端口 | SSH 端口，默认 `22` |
| 用户名 | SSH 登录用户名 |
| 认证方式 | 密码、粘贴私钥或私钥文件路径 |
| 连接方式 | 直连或选择已有跳板机 |
| 代理 | 可选 SOCKS5 或 HTTP CONNECT |

保存后可以点击“测试连接”。测试成功会显示连接延迟。

### 密码认证

选择“密码”，填写 SSH 用户密码。

### 粘贴私钥

选择“私钥（粘贴内容）”，粘贴完整私钥，例如：

```text
-----BEGIN OPENSSH PRIVATE KEY-----
...
-----END OPENSSH PRIVATE KEY-----
```

如果私钥已加密，还需要填写私钥口令。

### 私钥文件

选择“私钥（文件路径）”，填写本机绝对路径，例如：

```text
C:\Users\your-name\.ssh\id_ed25519
```

使用 `key_path` 时，托盘程序和 stdio MCP 进程都必须能读取该文件。把配置复制到其他电脑后，需要保证对应路径存在。

## 6. 配置跳板机

在“跳板机”页面点击“新增跳板机”。跳板机字段与普通服务器类似。

链式多跳示例：

```text
本机 -> jump-a -> jump-b -> target
```

配置顺序：

1. 新增 `jump-a`，上一级跳板选择“无”。
2. 新增 `jump-b`，上一级跳板选择 `jump-a`。
3. 新增目标服务器 `target`，跳板选择 `jump-b`。

程序会自动从根跳板开始建立 SSH 链。跳板链存在环路或引用不存在的跳板机时，配置会被拒绝。

仍被服务器或其他跳板机引用的跳板机不能直接删除，需要先解除引用。

## 7. 配置代理

服务器可以配置 SOCKS5 或 HTTP CONNECT 代理。代理和跳板机同时存在时，代理用于连接第一台跳板机；没有跳板机时，代理直接连接目标服务器。

### SOCKS5

填写代理主机和端口。代理需要认证时，再填写用户名和密码。

示例：

```text
类型：SOCKS5
主机：127.0.0.1
端口：1080
```

### HTTP CONNECT

填写 HTTP 代理主机和端口。需要 Basic 代理认证时填写用户名和密码。

示例：

```text
类型：HTTP CONNECT
主机：127.0.0.1
端口：8080
```

## 8. MCP 客户端接入

打开 WebUI 的“对接文档”页面，可以直接复制当前程序对应的配置。

### Claude Desktop

配置示例：

```json
{
  "mcpServers": {
    "ssh-mcp": {
      "command": "C:\\Users\\xiaobai\\Desktop\\ssh-mcp2\\dist\\ssh-mcp.exe",
      "args": ["--mcp"]
    }
  }
}
```

保存后完全退出并重新启动 Claude Desktop。

### Cursor

在 Cursor MCP 设置或项目 `.cursor/mcp.json` 中加入：

```json
{
  "mcpServers": {
    "ssh-mcp": {
      "command": "C:\\Users\\xiaobai\\Desktop\\ssh-mcp2\\dist\\ssh-mcp.exe",
      "args": ["--mcp"]
    }
  }
}
```

### VS Code

在项目 `.vscode/mcp.json` 或用户 MCP 配置中加入：

```json
{
  "servers": {
    "ssh-mcp": {
      "type": "stdio",
      "command": "C:\\Users\\xiaobai\\Desktop\\ssh-mcp2\\dist\\ssh-mcp.exe",
      "args": ["--mcp"]
    }
  }
}
```

### Streamable HTTP

托盘程序运行时，可以使用：

```text
http://127.0.0.1:8877/mcp/
```

配置示例：

```json
{
  "mcpServers": {
    "ssh-mcp": {
      "type": "streamable-http",
      "url": "http://127.0.0.1:8877/mcp/"
    }
  }
}
```

HTTP MCP 依赖托盘程序持续运行。URL 末尾应保留 `/`。

## 9. MCP 工具

### 配置管理

| 工具 | 用途 |
| --- | --- |
| `ssh_list_servers` | 列出服务器和跳板机，不返回凭据 |
| `ssh_add_server` | 新增服务器 |
| `ssh_update_server` | 部分更新服务器 |
| `ssh_delete_server` | 删除服务器 |
| `ssh_add_jumphost` | 新增跳板机 |
| `ssh_update_jumphost` | 部分更新跳板机 |
| `ssh_delete_jumphost` | 删除跳板机 |

### 命令执行

| 工具 | 用途 |
| --- | --- |
| `ssh_exec` | 在一台服务器执行命令 |
| `ssh_exec_multi` | 在多台服务器执行同一命令 |
| `ssh_test_connection` | 测试 SSH 连接 |

`ssh_exec` 支持：

- `timeout`：执行超时，范围 1 到 600 秒，默认 30 秒。
- `cwd`：执行命令前切换远程目录。
- `env`：执行命令前设置环境变量。

命令退出码非零时会返回错误，并包含已捕获的 stdout 和 stderr。

### 文件操作

| 工具 | 用途 |
| --- | --- |
| `ssh_upload` | 从本机上传文件 |
| `ssh_download` | 下载远程文件到本机 |
| `ssh_read_file` | 读取远程文本文件 |
| `ssh_write_file` | 覆盖写入远程文本文件 |
| `ssh_list_dir` | 列出远程目录 |
| `ssh_stat` | 获取远程文件属性 |

命令单条输出上限为 4 MB。远程文件读取和写入硬上限为 4 MB，`ssh_read_file` 默认读取上限为 1 MB。

## 10. 锁定和退出

在 WebUI“设置”页面点击“锁定并退出登录”后：

- 删除本机 `key.bin`。
- 撤销已有 WebUI 会话。
- 关闭已缓存的 SSH 连接。
- stdio MCP 无法继续解密服务器凭据。

重新登录 WebUI 后会再次生成 `key.bin`，MCP 工具即可恢复使用。

系统托盘“退出”只退出当前程序，不会删除服务器配置。

## 11. 修改主密码

在 WebUI“设置”页面填写：

1. 当前主密码。
2. 新主密码。
3. 再次确认新主密码。

修改成功后，完整配置会使用新的 salt 和派生密钥重新加密，其他已有会话会失效，当前页面保持登录。

## 12. 忘记主密码

主密码无法恢复。如需重置：

1. 打开 WebUI“设置”。
2. 在忘记主密码区域准确输入 `DELETE`。
3. 确认清空。

该操作会永久删除服务器、跳板机及凭据配置。执行前应确认已有可用备份。

## 13. 导入和导出

### 完整导出

完整导出包含加密后的配置和凭据。导入时需要输入导出时使用的主密码。

完整导出中不会出现明文密码或私钥，但仍应作为敏感文件保管。

### 无凭据模板

模板导出会把密码、私钥和口令替换为 `******`，适合共享配置结构，但不能直接用于完整恢复。

### 导入

导入完整配置会整体替换当前服务器和跳板机配置，并使用当前主密码对应的密钥重新加密。

错误密码、损坏数据、重复名称、无效跳板引用或跳板环路都会导致导入失败，原配置不会被覆盖。

## 14. 从 Python 版迁移

Go 版与 Python 版使用相同数据目录和加密格式，一般不需要转换配置。

推荐迁移流程：

1. 在 Python 版 WebUI 中导出完整配置。
2. 完全退出 Python 托盘程序。
3. 备份 `%LOCALAPPDATA%\ssh_mcp`。
4. 启动 Go 版 `ssh-mcp.exe --open`。
5. 使用原主密码登录。
6. 测试服务器连接和 MCP 客户端。

不要同时运行 Python 版和 Go 版。两者默认使用相同端口、配置文件和锁文件。

使用 Go 版安装包覆盖安装时，相同 AppId 会沿用原安装位置。安装器会删除 Python 版的 `ssh_mcp.exe` 和 `_internal` 运行时目录，但不会删除 `%LOCALAPPDATA%\ssh_mcp` 中的配置和凭据。

## 15. 备份和恢复

推荐同时保留：

- WebUI 生成的完整配置导出。
- `%LOCALAPPDATA%\ssh_mcp\config.json` 的离线备份。

不要把 `key.bin`、主密码或完整导出提交到 Git 仓库。

恢复时优先使用 WebUI“导入配置”功能。直接覆盖 `config.json` 前必须退出所有 SSH-MCP 进程。

## 16. 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SSH_MCP_PORT` | `8877` | WebUI 和 HTTP MCP 端口 |
| `SSH_MCP_PASSWORD` | 无 | stdio MCP 无法读取 `key.bin` 时使用的主密码 |
| `SSH_MCP_STRICT_HOST_KEY` | `0` | 设为 `1` 启用严格主机密钥验证 |
| `SSH_MCP_KNOWN_HOSTS` | 无 | 附加的 `known_hosts` 文件 |

修改端口示例：

```powershell
$env:SSH_MCP_PORT = "9000"
.\dist\ssh-mcp.exe --open
```

对应 HTTP MCP 地址变为：

```text
http://127.0.0.1:9000/mcp/
```

`SSH_MCP_PASSWORD` 是敏感信息，不要写入仓库、日志或可被其他用户读取的全局环境配置。优先通过 WebUI 登录生成 `key.bin`。

## 17. SSH 主机密钥验证

默认模式自动接受未知 SSH 主机密钥，与参考 Python 项目保持兼容。该模式不能防御中间人攻击。

启用严格模式：

```powershell
$env:SSH_MCP_STRICT_HOST_KEY = "1"
.\dist\ssh-mcp.exe --open
```

程序默认尝试加载：

```text
%USERPROFILE%\.ssh\known_hosts
```

指定附加文件：

```powershell
$env:SSH_MCP_KNOWN_HOSTS = "C:\Users\your-name\.ssh\known_hosts"
```

严格模式下，未知或已变化的主机密钥会导致连接失败。

## 18. 托盘菜单

Windows 托盘菜单提供：

| 菜单 | 功能 |
| --- | --- |
| 打开 WebUI | 打开本地管理页面 |
| 重启服务 | 重启 WebUI 和 HTTP MCP 服务 |
| 重置密码 | 打开 WebUI 设置页面 |
| 开机自启 | 设置当前用户登录时自动启动 |
| 退出 | 停止 HTTP 服务并退出程序 |

## 19. 常见问题

### WebUI 无法打开

先检查健康端点：

```powershell
Invoke-RestMethod http://127.0.0.1:8877/api/health
```

检查端口占用：

```powershell
Get-NetTCPConnection -LocalPort 8877 -ErrorAction SilentlyContinue
```

如果端口被占用，可以设置 `SSH_MCP_PORT` 后重新启动。

### MCP 提示无法解密凭据

错误通常表示 `key.bin` 不存在。

处理方法：

1. 启动托盘程序。
2. 打开 WebUI。
3. 使用主密码登录一次。
4. 重启 MCP 客户端。

也可以为 stdio MCP 进程设置 `SSH_MCP_PASSWORD`，但不建议长期保存该变量。

### MCP 客户端找不到工具

确认命令路径指向实际存在的 `ssh-mcp.exe`，并包含：

```json
"args": ["--mcp"]
```

在终端验证版本：

```powershell
& "C:\Users\xiaobai\Desktop\ssh-mcp2\dist\ssh-mcp.exe" --version
```

修改 MCP 配置后应完全重启客户端。

### SSH 密码或私钥认证失败

检查：

- 用户名是否正确。
- SSH 端口是否正确。
- 密码是否过期。
- 私钥是否完整。
- 私钥口令是否正确。
- `key_path` 是否能被当前 Windows 用户读取。
- 服务器是否允许对应认证方式。

### 跳板机连接失败

按连接顺序分别测试每一跳。确认目标服务器的地址是从最后一台跳板机视角可访问的地址，而不是仅本机可访问的地址。

### 代理连接失败

确认代理支持 TCP CONNECT，目标主机和端口未被代理规则阻止。SOCKS5 用户名或密码错误时会直接返回认证失败。

### 严格模式提示 known_hosts 不存在

创建 `%USERPROFILE%\.ssh\known_hosts`，或者通过 `SSH_MCP_KNOWN_HOSTS` 指向已有文件。关闭严格模式只能恢复兼容行为，不提供主机指纹保护。

### 配置已被其他进程更新

这是 revision CAS 冲突保护。刷新 WebUI 后重新提交。还应确认没有同时运行 Python 版和 Go 版。

## 20. 安全注意事项

- WebUI 和 MCP HTTP 只应绑定 `127.0.0.1`。
- 不要通过反向代理、端口转发或防火墙规则暴露到局域网或公网。
- 不要在聊天、日志或代码仓库中粘贴主密码和私钥。
- 完整配置导出虽然已加密，仍应按敏感文件管理。
- 默认自动接受未知主机密钥，生产环境建议启用严格模式。
- 同一 Windows 用户下运行的其他进程通常可以访问 loopback 服务和用户数据目录。

## 21. 卸载

通过安装包安装时，可以在 Windows“设置 -> 应用 -> 已安装的应用”中卸载 SSH-MCP，也可以使用开始菜单中的卸载入口。

卸载程序会删除应用文件、快捷方式和由安装器创建的开机自启项，但默认保留用户配置：

```text
%LOCALAPPDATA%\ssh_mcp
```

免安装模式的清理步骤：

1. 从托盘菜单退出 SSH-MCP。
2. 关闭使用该 MCP 服务的客户端。
3. 如果启用了开机自启，先在托盘菜单中取消。
4. 删除 `ssh-mcp.exe`。

用户数据默认保留在：

```text
%LOCALAPPDATA%\ssh_mcp
```

只有在确认不再需要配置或已经完成备份后，才手动删除该目录。
