# SSH-MCP

[简体中文](README.md) | **English**

[![Build installers](https://github.com/ackwrap/ssh-mcp/actions/workflows/build.yml/badge.svg)](https://github.com/ackwrap/ssh-mcp/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/ackwrap/ssh-mcp)](https://github.com/ackwrap/ssh-mcp/releases)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An SSH and SFTP tool server for AI clients, built with Go and the Model Context Protocol (MCP).

Manage connections and credentials through a local WebUI, then use your MCP client to execute remote commands, transfer files, and manage servers. SSH-MCP runs on Windows, macOS, and Linux. The WebUI is embedded in a single executable; no Python, Node.js, or separate frontend service is required.

[Download installers](https://github.com/ackwrap/ssh-mcp/releases) · [Detailed usage (Chinese)](USAGE.md) · [Report an issue](https://github.com/ackwrap/ssh-mcp/issues)

## Features

- **Remote operations**: 16 MCP tools for command execution, batch execution, connection testing, file transfers, and directory operations.
- **Flexible connectivity**: Direct connections, multi-hop SSH chains, SOCKS5 and HTTP CONNECT proxies; password authentication, private key files, pasted keys, and key passphrases.
- **Local management**: A WebUI for master-password authentication, locking, password changes, imports and exports, server groups, and enable/disable controls.
- **Credential protection**: Fernet-encrypted configuration with PBKDF2-HMAC-SHA256 key derivation. File locks, revision checks, and atomic writes protect configuration updates.
- **Client integrations**: Built-in detection and configuration adapters for OpenCode, Claude Code, Codex, Kimi Code, ZCode, Cursor, VS Code, and Claude Desktop.
- **Transfers and transports**: stdio and Streamable HTTP MCP, SSH connection reuse, and a 1 MiB local read buffer per upload with bounded pipelined writes, retaining compatible 32 KiB SFTP packets.

Automatic integration availability depends on the client installation and platform. Review the detected tools and configuration paths in the WebUI before making changes. Configuration files are backed up before integration setup or removal modifies them.

## Platforms and installation

Download the appropriate installer from [GitHub Releases](https://github.com/ackwrap/ssh-mcp/releases).

| Platform | Installer | Executable / launch method |
| --- | --- | --- |
| Windows x64 | `SSH-MCP-Setup-<version>-windows-amd64.exe` | **SSH-MCP** in the Start menu, or `ssh-mcp.exe` in the installation directory |
| macOS Intel / Apple Silicon | `SSH-MCP-Setup-<version>-macos-universal.pkg` | `/usr/local/bin/ssh-mcp` |
| Debian / Ubuntu Linux x64 | `SSH-MCP-Setup-<version>-linux-amd64.deb` | `/usr/bin/ssh-mcp` |

Linux installation example; replace the filename with the version you downloaded:

```bash
sudo dpkg -i ./SSH-MCP-Setup-0.1.4-linux-amd64.deb
```

**Platform differences:** Windows provides a native system tray, single-instance management, and optional startup at login. macOS/Linux do not provide a native tray or install an automatic startup service; the management service runs in the foreground until you press Ctrl+C. All three platforms support stdio, the WebUI, and Streamable HTTP MCP.

**Signing status:** Windows installers are not currently commercially code-signed. macOS installers are not Developer ID-signed or notarized. Your operating system may display a security warning.

## Quick start

### 1. Initialize local configuration

On Windows, launch **SSH-MCP** from the Start menu. On macOS/Linux, run:

```bash
ssh-mcp --open
```

Open `http://127.0.0.1:8877`, set a master password, and add a server. Without a desktop environment, run `ssh-mcp --tray` and access the WebUI from a browser on the same machine.

### 2. Connect an MCP client

Use the WebUI's **AI tool integrations** page to detect and configure clients, or configure stdio MCP manually. This example is for clients that use the `mcpServers` configuration format:

```json
{
  "mcpServers": {
    "ssh-mcp": {
      "command": "/usr/local/bin/ssh-mcp"
    }
  }
}
```

- On macOS, use `/usr/local/bin/ssh-mcp`.
- On Linux, use `/usr/bin/ssh-mcp`.
- On Windows, use the full path to the installed `ssh-mcp.exe`. Escape backslashes in JSON, for example `C:\\path\\to\\ssh-mcp.exe`.
- Top-level configuration formats vary by client. See [USAGE.md (Chinese)](USAGE.md) for detailed instructions.

**Running without arguments starts stdio MCP**; `--mcp` is optional. Each client launches its own subprocess. Do not use `--tray` or `--open` in a stdio configuration. Use the WebUI for initial setup or to unlock credentials again.

### 3. Optional: use HTTP MCP

Keep the local management service running and connect a Streamable HTTP-compatible client to:

```text
http://127.0.0.1:8877/mcp/
```

The health endpoint is `http://127.0.0.1:8877/api/health`.

## Command-line options

| Option | Behavior |
| --- | --- |
| No arguments | Run the stdio MCP server |
| `--mcp` | Legacy-compatible stdio mode |
| `--tray` | Start WebUI / HTTP MCP; use the tray on Windows or run in the foreground on macOS/Linux |
| `--open` | Start the management service and attempt to open a browser |
| `--version` / `-v` | Print the version and exit |

`--mcp`, `--tray`, and `--open` are mutually exclusive. Windows reuses an existing tray instance; stdio subprocesses remain independent.

## MCP tools

| Category | Tools |
| --- | --- |
| Connection management | `ssh_list_servers`, `ssh_add_server`, `ssh_update_server`, `ssh_delete_server`, `ssh_add_jumphost`, `ssh_update_jumphost`, `ssh_delete_jumphost` |
| Remote execution | `ssh_exec`, `ssh_exec_multi`, `ssh_test_connection` |
| File operations | `ssh_upload`, `ssh_download`, `ssh_read_file`, `ssh_write_file`, `ssh_list_dir`, `ssh_stat` |

Disabled servers are excluded from MCP listings and cannot be operated on through MCP. Enable/disable controls are managed in the WebUI. Server listings include group information but never return passwords or private keys. Server, jump-host, and proxy addresses are hidden by default; listing real addresses requires explicitly turning off **Hide IP** in the WebUI.

Command stdout and stderr are each limited to 4 MiB. Text content for `ssh_read_file` / `ssh_write_file` is limited to 4 MiB. That text limit does not apply to streaming `ssh_upload` / `ssh_download`. See the [usage guide (Chinese)](USAGE.md) for full parameters and limits.

## Security and data

> The WebUI, `/api/*`, and `/mcp/` are for local use only and bind exclusively to `127.0.0.1`. Do not expose them to a LAN or the internet through port forwarding or a reverse proxy.

- **Host keys**: SSH host-key verification is disabled by default. For production use, set `SSH_MCP_STRICT_HOST_KEY=1` and maintain a trusted `known_hosts` file.
- **Encryption**: PBKDF2-HMAC-SHA256 with 300,000 iterations derives a 32-byte key used to encrypt configuration with Fernet. The data format remains compatible with the previous implementation.
- **Unlocked state**: After unlocking, the derived key is cached locally in `key.bin`. Protect your local account and data directory. Locking through the WebUI removes this file, revokes sessions, and closes cached connections.
- **Backups and upgrades**: Export a complete encrypted backup before upgrading. Do not run old and new implementations concurrently against the same data directory, or commit keys, master passwords, or full configuration exports to a repository.

Default data directories:

| Platform | Directory |
| --- | --- |
| Windows | `%LOCALAPPDATA%\ssh_mcp\` |
| macOS / Linux | `~/ssh_mcp/` |

The main files are `config.json` (encrypted configuration), `key.bin` (unlock key), and `session.secret` (WebUI session secret). Preserve user data when uninstalling unless you explicitly intend to delete your configuration.

### Environment variables

| Variable | Purpose |
| --- | --- |
| `SSH_MCP_PORT` | WebUI / HTTP MCP port; defaults to `8877` |
| `SSH_MCP_PASSWORD` | Master-password fallback when stdio mode cannot read the cached key; avoid persisting it in the environment |
| `SSH_MCP_STRICT_HOST_KEY` | Set to `1` to enable strict host-key verification |
| `SSH_MCP_KNOWN_HOSTS` | Additional `known_hosts` file used in strict mode |

## Build from source

Requires **Go 1.25+**. WebUI assets are embedded into the executable; npm and frontend build tools are not required.

### Windows

Requires Inno Setup 6. If it is missing, the script attempts to install it through `winget`.

```powershell
.\build.ps1
```

Outputs: `dist\ssh-mcp.exe` and `dist\installer\SSH-MCP-Setup-<version>.exe`. Use `-SkipInstaller` to build only the application. Windows installer filenames published by CI also include the `-windows-amd64` suffix.

### macOS / Linux

macOS requires Xcode Command Line Tools; Debian/Ubuntu requires `dpkg-deb`. Run on the corresponding operating system:

```bash
bash ./build.sh
```

Outputs: `dist/ssh-mcp` and an installer under `dist/installer/`. macOS combines amd64/arm64 into a universal executable. Linux generates a `.deb` for the host's amd64/arm64 architecture.

### Development checks

```bash
go fmt ./...
go test -p 1 ./...
go vet ./...
go mod verify
```

## GitHub Actions

The [build workflow](.github/workflows/build.yml) tests and packages the application on native runners for all three operating systems:

- **Branch pushes / pull requests / manual runs** build three installers, available under **Artifacts** on the workflow run page for 14 days.
- **Pushing a `v<version>` tag** publishes a GitHub Release after all platform builds succeed, including three installers and `SHA256SUMS.txt`.
- **Version validation** requires the tag to match `internal/mcpserver/server.go`. Installer versions are read from the built application.
- **Permissions** are read-only for regular builds. Only the release job receives `contents: write`.

For example, after committing and pushing the source for version `0.1.4`:

```bash
git tag -a v0.1.4 -m "Release v0.1.4"
git push origin v0.1.4
```

## Documentation and support

- [Usage guide (Chinese)](USAGE.md): Setup, client integrations, migration, backups, and troubleshooting.
- [Maintenance guide (Chinese)](AGENTS.md): Project structure, compatibility boundaries, and validation requirements.
- [GitHub Issues](https://github.com/ackwrap/ssh-mcp/issues): Bug reports and feature requests. Never include real credentials.

## License

Licensed under the [MIT License](LICENSE).
