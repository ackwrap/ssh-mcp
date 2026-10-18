# SSH-MCP Go 使用说明

本文档说明 SSH-MCP Go 版的构建、运行、服务器配置、MCP 接入、数据迁移和常见问题处理。

## 1. 系统要求

直接使用已构建程序时：

- Windows 10/11 x64、macOS（Intel 或 Apple Silicon）或 Debian/Ubuntu Linux x64。
- 可以访问需要管理的 SSH 服务器。
- 不需要安装 Python、uv 或 Node.js。

从源码构建时：

- Go 1.25 或更高版本。
- Windows 使用 PowerShell 和 Inno Setup 6；macOS 使用 Bash 与 Xcode Command Line Tools；Debian/Ubuntu 使用 Bash 与 `dpkg-deb`。

## 2. 获取程序

从 [GitHub Releases](https://github.com/ackwrap/ssh-mcp/releases) 下载与系统对应的安装包。Windows 提供 `.exe`，macOS 提供 Intel/Apple Silicon 通用 `.pkg`，Linux 提供 x64 `.deb`。以下 Windows 路径示例仅用于本地构建；自动构建的文件名还包含平台和架构。

### macOS / Linux 安装与启动

macOS：双击 `SSH-MCP-Setup-<版本>-macos-universal.pkg` 安装，程序位于 `/usr/local/bin/ssh-mcp`。安装包暂未进行 Developer ID 签名和公证，系统可能要求手动允许。

Debian/Ubuntu：

```bash
sudo dpkg -i ./SSH-MCP-Setup-0.1.5-linux-amd64.deb
```

程序位于 `/usr/bin/ssh-mcp`。两平台安装后都在终端运行：

```bash
ssh-mcp --version
ssh-mcp --open
```

`--open` 启动前台 WebUI/HTTP MCP 并尝试打开浏览器；`--tray` 只启动前台服务，不提供原生托盘。关闭终端或按 Ctrl+C 会停止该管理服务，stdio MCP 子进程不受影响。无图形界面的 Linux 使用 `ssh-mcp --tray`，手动在本机访问 `http://127.0.0.1:8877`。服务仍只监听本机地址，不能暴露到公网。

初次使用先在 WebUI 设置主密码和服务器，再在 MCP 客户端中将 `command` 指向 `/usr/local/bin/ssh-mcp`（macOS）或 `/usr/bin/ssh-mcp`（Linux），无需传入参数。

源码构建在相应系统执行 `bash ./build.sh`，产物在 `dist/` 与 `dist/installer/`。Linux 脚本也支持在 arm64 主机本地打包。自动构建、标签发布、校验和及依赖要求见 [README 的构建说明](README.md#github-actions)。macOS/Linux 默认数据目录为 `~/ssh_mcp/`，包含 `config.json`、`key.bin`、`session.secret`；卸载程序时保留该目录。

### Windows 安装与本地构建

推荐使用安装包：

```text
dist\installer\SSH-MCP-Setup-0.1.5.exe
```

运行安装包后，可以选择创建桌面快捷方式和登录 Windows 时自动启动托盘。开始菜单、桌面快捷方式及安装完成页都会使用 `ssh-mcp.exe --open`，因此启动应用时会同时打开 WebUI。

当前安装包未配置商业代码签名证书，Windows SmartScreen 可能显示“未知发布者”。发布给其他用户前建议使用受信任证书对安装包和应用程序签名。

免安装单文件程序位于：

```text
dist\ssh-mcp.exe
```

需要重新构建时，在项目目录执行：

```powershell
.\build.ps1
```

脚本会先运行测试，再生成：

```text
dist\ssh-mcp.exe
dist\installer\SSH-MCP-Setup-0.1.5.exe
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
ssh-mcp 0.1.5
```

## 3. 启动方式

### 启动托盘程序

本节托盘、快捷方式和开机自启说明适用于 Windows；macOS/Linux 请按上一节使用前台管理服务。

从开始菜单或桌面快捷方式启动时，安装器会自动附加 `--open`。直接运行免安装程序并仅启动托盘服务时，请执行：

```powershell
.\dist\ssh-mcp.exe --tray
```

程序会驻留在 Windows 系统托盘，并在本机启动 WebUI 和 HTTP MCP 服务。

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

### 运行 stdio MCP（默认）

```powershell
.\dist\ssh-mcp.exe
```

旧的 `--mcp` 参数仍然兼容：

```powershell
.\dist\ssh-mcp.exe --mcp
```

该模式通常由 Claude Desktop、Cursor、VS Code 等 MCP 客户端自动启动，不建议直接在终端中交互使用。

stdio MCP 不使用全局单例。每个 MCP 客户端会启动独立子进程，因为每个进程都需要独立的 stdin/stdout 通道；多个 stdio 子进程会通过配置文件锁和 revision 机制安全共享同一份配置。这不代表启动了多个 HTTP MCP 服务。

### 参数限制

`--mcp`、`--tray` 和 `--open` 不能同时使用。未知参数会被拒绝。

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
| 分组 | 可选，如“生产环境”“测试环境”，留空为“未分组” |
| 状态 | 启用或停用；默认启用，停用后 AI 无法查看或操作 |
| 认证方式 | 密码、粘贴私钥或私钥文件路径 |
| 连接方式 | 直连或选择已有跳板机 |
| 代理 | 可选 SOCKS5 或 HTTP CONNECT |

保存后可以点击“测试连接”。测试成功会显示连接延迟。

服务器按分组显示，可点击组头折叠/展开，也可在顶部筛选分组。组头显示服务器总数和启用/停用数量，卡片上的“停用”/“启用”按钮可以直接切换状态；也可在编辑弹窗中设置。复制、导入和导出会保留分组与停用状态，旧配置默认启用且未分组。

停用的服务器仍可在 WebUI 中管理和测试连接，但不会返回给 AI。即使 AI 知道名称，MCP 的命令执行、连接测试、文件操作、修改及删除也会拒绝访问；AI 不能通过 MCP 重新启用服务器。状态变更对后续 MCP 请求生效。启用/停用仅控制本程序的 MCP 访问，不会关闭远程 SSH 服务。

新增、编辑和复制弹窗使用全屏遮罩，背景页面不可操作或滚动；移动鼠标、点击遮罩不会关闭弹窗，可使用“取消”或保存成功后关闭。

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

## 8. AI 工具一键接入

WebUI 不再提供需要手动复制的“对接文档”页面。打开“AI 工具接入”后，程序会检测当前 Windows 用户已安装的 AI 工具和已有 `ssh-mcp` 配置。

支持的工具：

| 工具 | 检测方式 | 配置方式 | 传输方式 |
| --- | --- | --- | --- |
| OpenCode | `opencode` 命令 | 官方 `opencode mcp add ... --global --` | stdio |
| Claude Code | `claude` 命令 | 官方 `claude mcp add --scope user ... --` | stdio |
| OpenAI Codex | `codex` 命令 | 官方 `codex mcp add ... --` | stdio |
| Kimi Code CLI | `kimi` 命令 | 用户级 `~/.kimi/mcp.json` | stdio |
| ZCode | `zcode` 命令或配置文件 | `~/.zcode/cli/config.json` 的 `mcp.servers` | stdio |
| Cursor | `cursor` 命令或应用路径 | `~/.cursor/mcp.json` | stdio |
| Visual Studio Code | `code` 命令或应用路径 | 用户级 `mcp.json` | stdio |
| Claude Desktop | 应用路径或现有配置 | `claude_desktop_config.json` | stdio |

接入步骤：

1. 启动 SSH-MCP 并登录 WebUI。
2. 打开“AI 工具接入”。
3. 点击“刷新检测”确认安装状态和配置路径。
4. 主动勾选需要接入的工具；初次打开、刷新检测和操作后重新加载列表时默认全部不勾选，也可点击“选择待接入”或“选择可取消”批量选择。
5. 点击“一键接入”并确认。
6. 查看每个工具的成功或失败结果。
7. 按页面提示完全重启对应 AI 工具。

一键接入只会执行内置的固定适配器，不接受浏览器传入的命令、可执行路径或任意参数。所有工具均写入无参数 stdio 命令，每个客户端启动独立 SSH-MCP 子进程。检测到相同命令时会直接成功跳过；旧 HTTP 或其他命令会显示“待迁移”，重新接入后替换为当前 stdio 配置。使用配置文件适配器时，程序会保留其他配置，并在原文件旁创建 `.ssh-mcp.bak` 备份；官方 CLI 配置失败时会恢复原文件。

勾选已存在配置的工具并点击“取消接入”，程序只删除对应配置中的 `ssh-mcp` 条目，不删除 AI 工具本体，也不删除其他 MCP 服务器。工具本体未安装但仍有残留配置时不会自动勾选，仍可手动选择并清理配置。Claude Code 和 Codex 使用官方 CLI 移除；OpenCode 及配置文件适配器执行安全 JSON 合并。包含注释或尾逗号的 OpenCode JSONC 会拒绝自动删除，避免破坏原格式。

官方 CLI 执行失败时，WebUI 会显示经过安全处理的原始错误详情：进程输出最多保留最新 64 KiB，ANSI 控制符和不可见字符会被移除，页面详情限制为 2000 个字符，API Key、token、密码、Authorization 和私钥内容会被替换为掩码。若 CLI 没有输出，页面会显示进程自身返回的错误。

Windows 上如果 `PATH` 命中 Scoop 生成的 `.exe` shim，程序会读取同目录标准 `.shim` 元数据，并通过 Windows 文件句柄确认最终目标仍位于同一个 Scoop 同名应用的 `apps` 目录后直接启动，避免无控制台的托盘进程触发 Scoop shim 创建进程失败。相对路径、网络路径、跨应用目标、缺失目标或异常大的元数据不会被采用。

OpenCode、Claude Code、Codex 使用各自官方 CLI 写入全局或用户级 stdio 配置。Kimi Code、ZCode、Cursor、VS Code 和 Claude Desktop 使用其公开配置格式进行原子合并。

OpenCode 使用 `--global`，默认写入 `~/.config/opencode/opencode.json(c)`（设置 `XDG_CONFIG_HOME` 时使用对应目录），不修改启动目录中的项目配置。检测和取消接入支持 V2 的 `mcp.servers.ssh-mcp`，也兼容旧版 `mcp.ssh-mcp`。如果旧版本接入后只在某个项目可见，升级后重新点击“一键接入”即可补上全局配置；已有项目级条目不会自动删除，仍可能覆盖全局同名条目，需按需检查。

工具检测使用当前 Windows 用户的 `PATH`、`CODEX_HOME`、`KIMI_SHARE_DIR` 等环境设置。只应接入你确认可信的本机工具安装；如果同名命令来自未知目录，应先修正 `PATH`。一键接入会在写入前再次比较文件内容，发现外部更新时拒绝覆盖并要求刷新重试；不要在接入过程中同时编辑同一个配置文件。

OpenCode 通过官方 CLI 无损修改 JSONC。其他配置文件如果包含 JSONC 注释或尾逗号，一键接入会拒绝重写并保留原文件，避免丢失注释；此时可先将对应文件转换为标准 JSON 后重试。

托盘运行时仍提供 Streamable HTTP MCP：

```text
http://127.0.0.1:8877/mcp/
```

一键接入不使用上面的 HTTP 地址，而是写入无参数 stdio `ssh-mcp.exe`。Streamable HTTP 端点继续保留给需要手动配置共享托盘服务的客户端；旧 stdio 配置中的 `--mcp` 参数仍然兼容。

## 9. MCP 工具

### 配置管理

| 工具 | 用途 |
| --- | --- |
| `ssh_list_servers` | 列出已启用服务器（含分组）和跳板机，默认隐藏主机/代理地址，不返回凭据 |
| `ssh_add_server` | 新增服务器 |
| `ssh_update_server` | 部分更新服务器 |
| `ssh_delete_server` | 删除服务器 |
| `ssh_add_jumphost` | 新增跳板机 |
| `ssh_update_jumphost` | 部分更新跳板机 |
| `ssh_delete_jumphost` | 删除跳板机 |

`ssh_list_servers` 的 `servers[].group` 为空字符串时表示未分组；服务器、跳板机及代理的 `host` 默认均为 `******`。通过服务器名称调用其他工具，真实地址仍保留在本地加密配置中。该掩码仅针对列表中的地址字段，WebUI 仍显示真实地址；自定义名称、分组以及远程命令输出不会被改写。

在 WebUI“设置 → MCP 地址隐私”中使用“隐藏 IP”开关：默认开启，只有用户手动关闭后，MCP 列表才能读取真实 IP 或域名。重新开启后，后续列表请求立即恢复隐藏；已返回给客户端的历史内容不会被撤回。开关状态会加密保存并在重启后保留，已有配置缺少该设置时默认隐藏，导入配置也不会改变本机隐私设置。无论开关如何设置，停用服务器仍不可见，密码和私钥仍不会返回。

`ssh_add_server` 和 `ssh_update_server` 支持设置 `group`。`disabled` 为 WebUI/API 管理字段（布尔值），MCP 不允许设置该字段，也不允许操作已停用的服务器。

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

`ssh_upload` 使用每文件 1 MiB（1,048,576 字节）的本地读缓存流式读取文件，减少磁盘读取次数；SFTP 协议仍使用兼容性较好的 32 KiB 数据包和 64 路并发请求流水线，避免逐包串行等待网络往返。读缓存大小不等于 SFTP 单包大小，不会把整个大文件加载到内存。批量上传时，每次调用都会复用可用的 SSH 连接。上传失败仍会返回错误，并尽力截断到首个失败数据包之前，可能留下不完整文件，需要重新上传。

命令单条输出上限为 4 MB。`ssh_read_file` 和 `ssh_write_file` 的文本内容硬上限为 4 MB，`ssh_read_file` 默认读取上限为 1 MB；该限制不适用于流式 `ssh_upload` / `ssh_download`。

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

确认命令路径指向实际存在的 `ssh-mcp.exe`。stdio 已是默认模式，无需配置 `args`；旧配置中的 `"args": ["--mcp"]` 仍然可用。

在终端验证版本：

```powershell
.\dist\ssh-mcp.exe --version
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

Linux `.deb` 可通过 `sudo dpkg -r ssh-mcp` 卸载。macOS 可移除 `/usr/local/bin/ssh-mcp` 和 `/usr/local/share/doc/ssh-mcp/` 中的程序与文档。两平台的 `~/ssh_mcp/` 为用户配置目录，不应随程序卸载删除。以下 Windows 卸载说明保持不变。

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
