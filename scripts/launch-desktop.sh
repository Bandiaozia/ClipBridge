#!/usr/bin/env bash
set -u

export DISPLAY="${DISPLAY:-:1}"
export XAUTHORITY="${XAUTHORITY:-/run/user/1000/gdm/Xauthority}"
export QT_QPA_PLATFORM="${QT_QPA_PLATFORM:-xcb}"

script_dir="$(cd "$(dirname "$0")" && pwd)"
project_dir="$(cd "$script_dir/.." && pwd)"

log_file="/tmp/clipbridge-desktop-launch.log"
printf '%s start pid=%s display=%s\n' "$(date -Is)" "$$" "$DISPLAY" >> "$log_file"
cd "$project_dir" || exit 70
"$project_dir/build/desktop-linux/clipbridge-desktop" >> "$log_file" 2>&1
exit_code=$?
printf '%s exit pid=%s code=%s\n' "$(date -Is)" "$$" "$exit_code" >> "$log_file"
exit "$exit_code"
