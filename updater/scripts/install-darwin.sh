#!/bin/sh
set -eu

pid="$1"
archive="$2"
destination="$3"
log_directory="${HOME}/Library/Logs/ATNetool"
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
new_bundle="$temporary/ATNetool.app"
if [ ! -d "$new_bundle" ]; then
  echo "archive does not contain ATNetool.app" >&2
  exit 1
fi

backup="$destination.old"
rm -rf "$backup"
mv "$destination" "$backup"
if ! mv "$new_bundle" "$destination"; then
  mv "$backup" "$destination"
  exit 1
fi
rm -rf "$backup"
rm -f "$archive"
open "$destination"
