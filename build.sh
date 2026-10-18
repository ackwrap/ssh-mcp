#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$root"
system=$(uname -s)
case "$system" in
  Linux|Darwin) ;;
  *) echo "Use build.ps1 on Windows; build.sh supports macOS and Linux." >&2; exit 1 ;;
esac

echo '== [1/4] Test and verify =='
go test -p 1 ./...
go vet ./...
go mod verify

dist="$root/dist"
mkdir -p "$dist/installer"
work=$(mktemp -d "$dist/.package.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

echo '== [2/4] Build application =='
arch=$(go env GOHOSTARCH)
if [[ "$system" == Darwin ]]; then
  for target in amd64 arm64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH="$target" go build -trimpath -ldflags '-s -w' -o "$work/ssh-mcp-$target" .
  done
  lipo -create "$work/ssh-mcp-amd64" "$work/ssh-mcp-arm64" -output "$dist/ssh-mcp"
  # Go signs its thin binaries ad hoc; sign the final universal executable too.
  codesign --force --sign - "$dist/ssh-mcp"
else
  case "$arch" in
    amd64|arm64) ;;
    *) echo "Unsupported Linux architecture: $arch" >&2; exit 1 ;;
  esac
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags '-s -w' -o "$dist/ssh-mcp" .
fi

echo '== [3/4] Verify application version =='
output=$("$dist/ssh-mcp" --version)
version=${output#ssh-mcp }
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Unable to read application version: $output" >&2
  exit 1
fi
echo "Application version: $version"

echo '== [4/4] Build installer =='
package_root="$work/root"
if [[ "$system" == Darwin ]]; then
  bin_dir="$package_root/usr/local/bin"
  doc_dir="$package_root/usr/local/share/doc/ssh-mcp"
  installer="$dist/installer/SSH-MCP-Setup-$version-macos-universal.pkg"
else
  bin_dir="$package_root/usr/bin"
  doc_dir="$package_root/usr/share/doc/ssh-mcp"
  installer="$dist/installer/SSH-MCP-Setup-$version-linux-$arch.deb"
fi
install -d -m 0755 "$bin_dir" "$doc_dir"
install -m 0755 "$dist/ssh-mcp" "$bin_dir/ssh-mcp"
install -m 0644 README.md USAGE.md "$doc_dir/"
install -m 0644 LICENSE "$doc_dir/copyright"

if [[ "$system" == Darwin ]]; then
  pkgbuild --root "$package_root" \
    --identifier io.github.ackwrap.ssh-mcp \
    --version "$version" \
    --install-location / \
    --ownership recommended \
    "$installer"
else
  install -d -m 0755 "$package_root/DEBIAN"
  installed_size=$(du -sk "$package_root/usr" | awk '{print $1}')
  cat > "$package_root/DEBIAN/control" <<EOF
Package: ssh-mcp
Version: $version
Section: net
Priority: optional
Architecture: $arch
Maintainer: SSH-MCP contributors
Installed-Size: $installed_size
Homepage: https://github.com/ackwrap/ssh-mcp
Description: SSH and SFTP tools for Model Context Protocol clients
 Includes a local WebUI and stdio/Streamable HTTP MCP servers.
EOF
  chmod 0644 "$package_root/DEBIAN/control"
  dpkg-deb --build --root-owner-group "$package_root" "$installer"
fi

echo "Build complete: $installer"
