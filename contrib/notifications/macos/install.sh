#!/bin/bash
# Install the macOS notification pipeline: delivery module + watcher +
# launchd agent (plist template is rendered with the installing user's $HOME).
# Requires cmux (https://github.com/manaflow-ai/cmux).
set -e
cd "$(dirname "$0")"

LABEL=com.agentmanager.am-status-notify
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

install -m755 cmux-notify am-status-notify "$HOME/.local/bin/"
sed "s|__HOME__|$HOME|g" "$LABEL.plist" > "$PLIST"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "installed and loaded $LABEL"
