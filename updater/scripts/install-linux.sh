#!/bin/sh
set -eu

pid="$1"
archive="$2"
destination="$3"
data_home="${XDG_DATA_HOME:-${HOME}/.local/share}"
log_directory="$data_home/ATNetool"
mkdir -p "$log_directory"
log="$log_directory/update-$pid.log"
exec 2>>"$log"

count=0
while kill -0 "$pid" 2>/dev/null; do
  count=$((count + 1))
  if [ "$count" -ge 120 ]; then
    echo "application did not exit before timeout" >&2
    exit 1
  fi
  sleep 0.5
done

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"; rm -f "$0"' EXIT
tar -xzf "$archive" -C "$temporary"
new_binary="$temporary/ATNetool"
if [ ! -f "$new_binary" ]; then
  echo "archive does not contain ATNetool" >&2
  exit 1
fi
chmod +x "$new_binary"

install_directory=$(dirname "$destination")
if [ -w "$install_directory" ]; then
  backup="$destination.old"
  rm -f "$backup"
  mv "$destination" "$backup"
  if ! mv "$new_binary" "$destination"; then
    mv "$backup" "$destination"
    exit 1
  fi
  chmod +x "$destination"
  rm -f "$backup"
else
  if ! command -v pkexec >/dev/null 2>&1; then
    echo "destination is not writable and pkexec is unavailable" >&2
    exit 1
  fi
  pkexec install -m 755 "$new_binary" "$destination"
fi

rm -f "$archive"
setsid "$destination" >/dev/null 2>&1 < /dev/null &
