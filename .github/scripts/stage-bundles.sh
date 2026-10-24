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
