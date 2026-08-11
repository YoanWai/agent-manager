#!/bin/bash
# Uninstall the macOS notification pipeline installed by install.sh.
set -e

LABEL=com.agentmanager.am-status-notify

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
rm -f "$HOME/Library/LaunchAgents/$LABEL.plist"
rm -f "$HOME/.local/bin/am-status-notify" "$HOME/.local/bin/cmux-notify"
rm -rf "$HOME/.local/state/am-status-notify" "$HOME/.local/state/cmux-notify"
echo "uninstalled $LABEL"
