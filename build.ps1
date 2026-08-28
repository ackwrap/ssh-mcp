param(
  [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$dist = Join-Path $root "dist"
$output = Join-Path $dist "ssh-mcp.exe"

Push-Location $root
try {
  Write-Host "== [1/5] Run tests ==" -ForegroundColor Cyan
  go test -p 1 ./...
  if ($LASTEXITCODE -ne 0) { throw "Tests failed" }

  New-Item -ItemType Directory -Path $dist -Force | Out-Null
  Write-Host "== [2/5] Build Windows application ==" -ForegroundColor Cyan
  go build -trimpath -ldflags "-s -w -H=windowsgui" -o $output .
  if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
  if (-not (Test-Path -LiteralPath $output)) { throw "Application not found: $output" }

  Write-Host "== [3/5] Verify application version ==" -ForegroundColor Cyan
  $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
  $startInfo.FileName = $output
  $startInfo.Arguments = "--version"
  $startInfo.UseShellExecute = $false
  $startInfo.CreateNoWindow = $true
  $startInfo.RedirectStandardOutput = $true
  $startInfo.RedirectStandardError = $true
  $process = [System.Diagnostics.Process]::Start($startInfo)
  $versionOutput = $process.StandardOutput.ReadToEnd().Trim()
  $versionError = $process.StandardError.ReadToEnd().Trim()
  $process.WaitForExit()
  if ($process.ExitCode -ne 0 -or $versionOutput -notmatch '^ssh-mcp\s+(.+)$') {
    throw "Unable to read application version: stdout='$versionOutput', stderr='$versionError'"
  }
  $version = $Matches[1]
  Write-Host "Application version: $version"

  if ($SkipInstaller) {
    Write-Host "Installer build skipped." -ForegroundColor Yellow
    Write-Host "Application: $output" -ForegroundColor Green
    return
  }

  Write-Host "== [4/5] Find Inno Setup ==" -ForegroundColor Cyan
  $candidates = @(
    $env:ISCC_EXE,
    "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
    "$root\tools\innosetup\ISCC.exe"
  ) | Where-Object { $_ }
  $iscc = $candidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
  if (-not $iscc) {
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) {
      throw "Inno Setup 6 and winget were not found. Install Inno Setup or use .\build.ps1 -SkipInstaller."
    }
    Write-Host "Inno Setup was not found; installing it with winget..." -ForegroundColor Yellow
    winget install --id JRSoftware.InnoSetup -e --source winget --silent --accept-source-agreements --accept-package-agreements
    if ($LASTEXITCODE -ne 0) { throw "Failed to install Inno Setup with winget" }
    $iscc = $candidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if (-not $iscc) { throw "ISCC.exe was not found after installing Inno Setup" }
  }

  Write-Host "== [5/5] Build installer ==" -ForegroundColor Cyan
  & $iscc "/DMyAppVersion=$version" "installer.iss"
  if ($LASTEXITCODE -ne 0) { throw "Inno Setup build failed" }

  $installer = Join-Path $dist "installer\SSH-MCP-Setup-$version.exe"
  if (-not (Test-Path -LiteralPath $installer)) { throw "Installer not found: $installer" }
  $installerInfo = Get-Item -LiteralPath $installer

  Write-Host ""
  Write-Host "Build complete:" -ForegroundColor Green
  Write-Host "  Application: $output"
  Write-Host "  Installer: $installer ($([math]::Round($installerInfo.Length / 1MB, 1)) MB)"
}
finally {
  Pop-Location
}
