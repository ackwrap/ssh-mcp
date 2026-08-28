$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
$dist = Join-Path $root "dist"
$output = Join-Path $dist "ssh-mcp.exe"

Write-Host "Running tests..."
go test -p 1 ./...
if ($LASTEXITCODE -ne 0) { throw "Tests failed" }

New-Item -ItemType Directory -Path $dist -Force | Out-Null
Write-Host "Building $output..."
go build -trimpath -ldflags "-s -w -H=windowsgui" -o $output .
if ($LASTEXITCODE -ne 0) { throw "Build failed" }

& $output --version
if ($LASTEXITCODE -ne 0) { throw "Version check failed" }
Write-Host "Built: $output"
