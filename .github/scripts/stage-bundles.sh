#!/usr/bin/env bash
set -euo pipefail

label="$1"
version="$2"
stage="$3"
output="build/bin"
stem="ATNetool_${version}"

mkdir -p "$stage"

case "$label" in
  macos-arm64|macos-x64)
    if [[ "$label" == "macos-arm64" ]]; then arch="arm64"; else arch="amd64"; fi
    app="$output/ATNetool.app"
    test -d "$app"
    tar -C "$output" -czf "$stage/${stem}_darwin_${arch}.app.tar.gz" ATNetool.app
    dmg_root=$(mktemp -d)
    trap 'rm -rf "$dmg_root"' EXIT
    cp -R "$app" "$dmg_root/ATNetool.app"
    ln -s /Applications "$dmg_root/Applications"
    hdiutil create \
      -volname "ATNetool" \
      -srcfolder "$dmg_root" \
      -ov \
      -format UDZO \
      "$stage/${stem}_darwin_${arch}.dmg" \
      >/dev/null
    ;;
  linux-x64|linux-arm64)
    if [[ "$label" == "linux-arm64" ]]; then arch="arm64"; else arch="amd64"; fi
    binary="$output/ATNetool"
    test -f "$binary"
    package_dir=$(mktemp -d)
    trap 'rm -rf "$package_dir"' EXIT
    cp "$binary" "$package_dir/ATNetool"
    chmod +x "$package_dir/ATNetool"
    tar -C "$package_dir" -czf "$stage/${stem}_linux_${arch}.tar.gz" ATNetool

    deb_root=$(mktemp -d)
    trap 'rm -rf "$package_dir" "$deb_root"' EXIT
    install -D -m 0755 "$binary" "$deb_root/usr/bin/ATNetool"
    install -D -m 0644 build/linux/icon.png "$deb_root/usr/share/icons/hicolor/256x256/apps/atnetool.png"
    install -D -m 0644 /dev/stdin "$deb_root/usr/share/applications/atnetool.desktop" <<'DESKTOP'
[Desktop Entry]
Name=ATNetool
Comment=AT 网络助手
Exec=ATNetool
Icon=atnetool
Terminal=false
Type=Application
Categories=Network;Utility;
DESKTOP
    install -D -m 0644 /dev/stdin "$deb_root/DEBIAN/control" <<CONTROL
Package: atnetool
Version: ${version}
Section: net
Priority: optional
Architecture: ${arch}
Maintainer: Attson
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Description: AT 网络助手
 网络诊断、治理与加速工具。
CONTROL
    dpkg-deb --build --root-owner-group "$deb_root" "$stage/${stem}_linux_${arch}.deb" >/dev/null
    ;;
  windows-x64)
    executable="$output/ATNetool.exe"
    test -f "$executable"
    package_dir=$(mktemp -d)
    trap 'rm -rf "$package_dir"' EXIT
    cp "$executable" "$package_dir/ATNetool.exe"
    7z a "$stage/${stem}_windows_amd64.zip" "$package_dir/ATNetool.exe" >/dev/null
    installer=$(find "$output" -maxdepth 1 -type f -iname '*installer.exe' | head -1)
    test -n "$installer"
    cp "$installer" "$stage/${stem}_windows_amd64-installer.exe"
    ;;
  *)
    echo "unknown build label: $label" >&2
    exit 1
    ;;
esac

ls -lah "$stage"
