#!/bin/bash
# Install the Linux notification pipeline: delivery module + watcher +
# systemd user service.
set -e
cd "$(dirname "$0")"

install -m755 cmux-notify am-status-notify "$HOME/.local/bin/"
install -m644 am-status-notify.service "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable --now am-status-notify

echo "installed and started am-status-notify.service"
echo "optional: loginctl enable-linger \$USER   # start at boot without login"
echo "optional: per-turn Codex pings — add to ~/.codex/config.toml:"
echo "  notify = [\"$HOME/.local/bin/cmux-notify\", \"Codex\"]"
