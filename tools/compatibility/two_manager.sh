#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d /tmp/am-two.XXXXXX)
mkdir -p "$work/tmxa" "$work/tmxb"
SLOW_SWITCH=$work/unused
REAL_TMUX=$(command -v tmux)
socketA=$work/tmxa/socket
socketB=$work/tmxb/socket
cleanup() {
  rm -f "$SLOW_SWITCH"
  for pid_file in "$work/manager-a.pid" "$work/manager-b.pid"; do
    if [ -f "$pid_file" ]; then
      read -r manager_pid < "$pid_file"
      rm -f "$pid_file"
      case "$manager_pid" in
        ''|*[!0-9]*|0|1) ;;
        *) kill "$manager_pid" >/dev/null 2>&1 || true ;;
      esac
    fi
  done
  for pid in "${observer_pid:-}" "${a_pid:-}" "${b_pid:-}"; do
    [ -n "$pid" ] && kill "$pid" >/dev/null 2>&1 || true
  done
  "$REAL_TMUX" -S "$socketA" kill-server >/dev/null 2>&1 || true
  "$REAL_TMUX" -S "$socketB" kill-server >/dev/null 2>&1 || true
}
trap cleanup EXIT
binary=${1:?pass the candidate binary path}
python3 "$here/two_manager.py" --binary "$binary" --work "$work"
