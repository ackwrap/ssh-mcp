; SSH-MCP Go 安装程序（Inno Setup 6）
; 由 build.ps1 调用 ISCC.exe 编译。

#define MyAppName "SSH-MCP"
#ifndef MyAppVersion
  #define MyAppVersion "0.1.0"
#endif
#define MyAppPublisher "SSH-MCP contributors"
#define MyAppExeName "ssh-mcp.exe"
#define MyAppURL "https://github.com/ssh-mcp/ssh-mcp-go"

[Setup]
AppId={{8E2F3C1A-5D4B-4E6F-9A8B-1C2D3E4F5A6B}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/issues
AppUpdatesURL={#MyAppURL}/releases
DefaultDirName={localappdata}\Programs\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#MyAppExeName}
OutputDir=dist\installer
OutputBaseFilename=SSH-MCP-Setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
PrivilegesRequired=lowest
WizardStyle=modern
LicenseFile=LICENSE
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
AppMutex=Local\SSH-MCP-Singleton
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加选项："; Flags: unchecked
Name: "autostart"; Description: "登录 Windows 时自动启动托盘"; GroupDescription: "附加选项："; Flags: unchecked

[Files]
Source: "dist\ssh-mcp.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "USAGE.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[InstallDelete]
; 使用相同 AppId 从 Python 版原地升级时，清理旧运行时但保留用户数据目录。
Type: files; Name: "{app}\ssh_mcp.exe"
Type: filesandordirs; Name: "{app}\_internal"
Type: files; Name: "{group}\打开 WebUI.lnk"

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--open"; WorkingDir: "{app}"
Name: "{group}\使用说明"; Filename: "{app}\USAGE.md"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--open"; WorkingDir: "{app}"; Tasks: desktopicon

[Registry]
; 开机自启只启动托盘，不弹出浏览器；用户手动启动快捷方式统一使用 --open。
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "SSH-MCP"; ValueData: """{app}\{#MyAppExeName}"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--open"; WorkingDir: "{app}"; Description: "启动 {#MyAppName} 并打开 WebUI"; Flags: nowait postinstall skipifsilent

; 用户配置保留在 {localappdata}\ssh_mcp，卸载程序不会无提示删除。
