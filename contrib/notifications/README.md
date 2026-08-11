# Agent Manager → cmux notifications

Notifications from [Agent Manager](https://github.com/YoanWai/agent-manager)
agent sessions, delivered to the **correct cmux workspace** — whether Agent
Manager runs on the local Mac inside cmux, or on a remote Linux box reached
through a cmux ssh workspace.

Built and verified 2026-08-10/11. Two moving parts per platform, one
integration point (Agent Manager's session status files):

```
Agent Manager                       this host                         target
─────────────                       ──────────                        ──────
writes per-session            am-status-notify (watcher, 2s poll;
status files:                 systemd user service on Linux,
  waiting / working /         launchd agent on macOS)
  finished / idle                    │  on flip to waiting/finished:
<hooks>/<session-id>.status          ▼  cmux-notify "Agent Manager" "<id> …"
  Linux: ~/.config/agent-       cmux-notify (delivery module, per platform)
  manager/hooks/                     │
  macOS: ~/Library/Application       │  TARGETING (per event, fresh):
  Support/agent-manager/hooks/       │  live probe of the agent-manager
                                     │  process (its identity is the route)
                                     ▼
                          Linux:                          macOS:
                          ┌───────────────────────┐       ┌────────────────────────┐
                          │ cmux relay shim:      │       │ OSC 777 escape written │
                          │ ~/.cmux/bin/cmux      │       │ to the agent-manager   │
                          │ notify --workspace ID │       │ TTY; cmux raises it    │
                          │ (ID probed from       │       │ natively, attributed   │
                          │  /proc/<am>/environ   │       │ to the AM workspace    │
                          │  CMUX_WORKSPACE_ID)   │       │ (CMUX_WORKSPACE_ID and │
                          ├───────────────────────┤       │  TTY probed via ps)    │
                          │ notify-send (desktop  │       └────────────────────────┘
                          │ daemon, GNOME/KDE/….) │       single channel, no
                          └───────────────────────┘       osascript side channel
                                     │ both channels (Linux) / tty write (macOS) fail?
                                     ▼
                          spool FIFO (TSV, cap 200), replayed in order
                          by later calls / --flush; stale targets retried
                          once against the live-probed target
```

## Scope and compatibility — exactly this, nothing else

| Path | Mechanism | Status |
|---|---|---|
| macOS, Agent Manager in a local **cmux** terminal | OSC 777 into the AM pane's pty → cmux pane ring, sidebar badge, notification panel, macOS banner, correct workspace attribution | ✅ verified live |
| Linux desktop (Ubuntu/GNOME shown; KDE/Arch equivalent) | `notify-send` to the freedesktop notification daemon — **terminal-agnostic**, works no matter which terminal emulator Agent Manager runs in | ✅ verified on Ubuntu/GNOME |
| Agent Manager on remote Linux via **cmux ssh workspace** | cmux relay shim with explicit `--workspace` targeting, discovered live per event | ✅ verified live (notification lands on the remote's workspace even while another workspace is focused) |
| Codex per-turn pings (Linux) | `notify = ["<path>/cmux-notify", "Codex"]` in `~/.codex/config.toml`; codex's payload JSON is normalized to its `last-assistant-message`, one line, capped at 160 chars | ✅ deployed |

Explicitly **out of scope** (by design, not oversight):

- Terminal emulators other than cmux on macOS (iTerm2, Terminal.app, Kitty…).
  Terminal.app has no escape-sequence notification API at all; iTerm2/Kitty
  speak different dialects (OSC 9 / OSC 99). The delivery target is cmux.
- `osascript`/Notification-Center banners on macOS. They arrive attributed to
  "Script Editor", cannot focus the pane, and duplicate cmux's own banner.
  Deliberately removed.
- Per-agent wrapper scripts. The watcher covers every agent type Agent
  Manager manages; Codex's native `notify` hook points directly at the
  delivery module. Claude Code needs no configuration.
- Headless Linux with no desktop session and no cmux workspace connected:
  nothing can display there; events spool (cap 200) until a channel returns.

## Files

| Package file | Deployed at | Role |
|---|---|---|
| `linux/cmux-notify` | `~/.local/bin/cmux-notify` | **Delivery (Linux).** `cmux-notify [--workspace <id>] "title" "body"` · `--flush`. Targeting precedence: `--workspace` flag → `$CMUX_NOTIFY_WORKSPACE` env pin (empty = explicitly untargeted) → live probe of agent-manager's `CMUX_WORKSPACE_ID` in `/proc` → untargeted. Channels: cmux relay shim, then `notify-send`. 4-field TSV spool (title, body, relay route, workspace); flush replays per-event with stale-workspace fallback to untargeted. |
| `linux/am-status-notify` | `~/.local/bin/am-status-notify` | **Watcher (Linux).** Polls `~/.config/agent-manager/hooks/*.status` every 2s; fires on flips to `waiting`/`finished`; seeds silently on first pass; flushes the spool each cycle; drops state for removed sessions. |
| `linux/am-status-notify.service` | `~/.config/systemd/user/` | **Lifecycle (Linux).** `Restart=always`; linger enabled (`loginctl enable-linger <user>`) so it starts at boot. |
| `macos/cmux-notify` | `~/.local/bin/cmux-notify` | **Delivery (macOS).** `cmux-notify "title" "body"` · `--flush`. Probes the agent-manager process for its TTY (`ps`) and `CMUX_WORKSPACE_ID`, writes OSC 777 (`ESC ] 777 ; notify ; title ; body BEL`) to that TTY. Spools only when the TTY write fails; flush retries a dead stored TTY once against the live-probed TTY. |
| `macos/am-status-notify` | `~/.local/bin/am-status-notify` | **Watcher (macOS).** Identical logic, status dir is `~/Library/Application Support/agent-manager/hooks`. |
| `macos/com.agentmanager.am-status-notify.plist` | `~/Library/LaunchAgents/` | **Lifecycle (macOS).** Template — `__HOME__` is substituted with the installing user's home at install time. `RunAtLoad` + `KeepAlive`, loaded with `launchctl bootstrap gui/<uid>`. |
| `linux/install.sh` / `linux/uninstall.sh` | — | Install/remove the Linux pipeline (scripts, systemd unit, optional linger + Codex hints). |
| `macos/install.sh` / `macos/uninstall.sh` | — | Install/remove the macOS pipeline (scripts, rendered plist, launchd load/bootout). |
| `tests/test-interface-linux.sh` | — | **Test suite for the Linux delivery module.** 18 checks, fully sandboxed (temp `HOME`, fake relay that logs only on success, desktop channel off, workspace probe pinned empty). Covers targeting precedence, 4-field spool format, FIFO flush order, stale-workspace retry, mid-flush failure re-spool, spool cap, body normalization (JSON, 160-char cap, multiline collapse). |

State (both platforms): `~/.local/state/cmux-notify/{spool,log}` —
one decision line per event in `log`; `spool` exists only while events are
queued. Watcher dedup state: `~/.local/state/am-status-notify/`.

## Install

Linux: `bash linux/install.sh` (scripts into `~/.local/bin`, systemd user
unit enabled and started; prints the optional linger and Codex `notify`
hints at the end).

macOS (requires cmux): `bash macos/install.sh` (scripts into `~/.local/bin`,
plist rendered with your `$HOME`, launchd agent loaded).

Both watchers invoke the delivery module as `$HOME/.local/bin/cmux-notify` —
if your distribution packages this elsewhere, adjust the path in the watcher
scripts accordingly.

## Uninstall

`bash linux/uninstall.sh` or `bash macos/uninstall.sh` — removes scripts,
service/plist, and state directories. On Linux, remove the Codex `notify =`
line from `~/.codex/config.toml` if you added it.

## Why it is built this way (design notes)

- **Routing bug that started this:** cmux ssh workspaces each register a relay
  on the remote host, but after reconnects the binding can go stale, and an
  untargeted `cmux notify` then lands on whichever workspace is *focused*.
  Proven with controlled focus experiments. Fix: explicit `--workspace`
  targeting, with the id read live from the agent-manager process environment
  per event — reconnects and id churn cannot strand it.
- **macOS uses escape sequences, not the cmux CLI:** the local cmux socket
  refuses connections from processes not started inside cmux
  ("Access denied"), so a launchd watcher cannot use `cmux notify`. cmux is
  libghostty-based and natively turns OSC 777 from the terminal stream into a
  notification attributed to the emitting surface — writing to the AM pane's
  TTY is both the native path and the correctly-attributed one.
- **Linux uses `notify-send`, not escapes:** the freedesktop notification
  daemon is the platform's terminal-agnostic mechanism; OSC 777 support
  across Linux terminals is fragmented (urxvt/foot/contour/WezTerm yes,
  stock VTE/GNOME Terminal, Kitty, xterm, Alacritty no).
- **Spool discipline:** an event is only queued when every channel failed;
  replay preserves FIFO order; a stored target that died while queued gets
  one retry against the live-probed target before the event is re-queued.
- **Known upstream cmux quirks** (observed during verification, candidates
  for issues against manaflow-ai/cmux): orphaned remote-relay workspace
  binding after reconnect → focus-follows notification attribution; local
  `cmux notify --workspace` invoked from a non-cmux process appears to ignore
  the workspace argument.
