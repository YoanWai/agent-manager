#!/bin/bash
# Uninstall the Linux notification pipeline installed by install.sh.
set -e

systemctl --user disable --now am-status-notify 2>/dev/null || true
rm -f "$HOME/.config/systemd/user/am-status-notify.service"
rm -f "$HOME/.local/bin/am-status-notify" "$HOME/.local/bin/cmux-notify"
rm -rf "$HOME/.local/state/am-status-notify" "$HOME/.local/state/cmux-notify"
systemctl --user daemon-reload
echo "uninstalled am-status-notify"
echo "note: remove the notify = [...] line from ~/.codex/config.toml if added"
