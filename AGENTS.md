# Agent notes

Project context for coding agents. Human setup and PR conventions live in
[.github/CONTRIBUTING.md](.github/CONTRIBUTING.md), and what a review looks
for in [REVIEW.md](REVIEW.md). Fill the Scope section of the pull request
template: a review reads it to tell the change you meant from the change you
made.

## Product boundary

Keep agent-manager a thin wrapper around the supported TUIs. Preserve upstream
defaults and user configuration; avoid hardcoded models, version-specific
behavior, and recreating upstream settings. Apply the
[Thin Wrapper Principle](PRODUCT.md#thin-wrapper-principle) to every integration,
including shared controls, capability discovery, and status detection.
Provider-specific features multiply ongoing maintenance. Supporting a TUI does
not mean mirroring its feature set; prefer workspace features shared across tools.

## What every change covers

A feature, a tool, or a fix is done when it holds on the whole matrix. The
matrix is every CLI in `builtinTools` (`internal/config/config.go`), every
platform `.goreleaser.yaml` builds, and both input methods. A change that
covers a subset names what it leaves out in the PR description and what
covering the rest would take; [REVIEW.md](REVIEW.md) says how a review treats
that.

- **Keyboard and mouse.** Every action a key performs is reachable with the
  mouse, and every clickable surface has a key. A new row, panel, button,
  or handle ships with both, and the footer and the `?` key map name the key.
- **Dynamic over hardcoded.** Anything that varies by tool, version, or user
  is discovered at runtime or read from the tool itself: models, thinking
  modes, capabilities, prompts, screens. A maintained list of values that an
  upstream release can change is a defect. Where discovery has no stable,
  documented interface, the feature stays in the underlying TUI.
- **Configurable in the UI, or not configurable.** A setting a user can
  change lives in Settings (`s`), is stored by the manager, and takes effect
  from the picker, the way the keybindings row does. The manager owns
  config.toml: the picker writes it, and a user never has to open it for a
  feature to work. A new per-user file, environment variable, or hand-edited
  block is the wrong shape; put the choice on a Settings row or pick a
  default in code.
- **Every tool.** A field, status rule, or command added to one `[tools.*]`
  block is added to every tool it applies to, in the same PR.
- **Every platform.** A `runtime.GOOS` branch, a platform-only command, or
  a path built from one platform's layout needs its counterpart, or the
  description names the platform it skips.

The reason is maintenance. One binary serves every user, and a value frozen
in code or in a user's file stops receiving fixes the day it is written.

## Build and test

```bash
go run .                                        # run the TUI
go build ./...
mkdir -p /tmp/amtest && env -u TMUX TMUX_TMPDIR=/tmp/amtest go test -race ./...
```

The suite drives a real tmux server. When your own shell already runs inside
tmux, `$TMUX` overrides `TMUX_TMPDIR` and the tests land on the live socket,
so `env -u TMUX` is mandatory, never a bare `go test`. Keep the socket dir
short: `TMUX_TMPDIR/tmux-<uid>/default` must stay under 104 characters or
tmux silently falls back to the default socket. The directory must exist
before tmux starts, for the same reason. Never run `tmux kill-server` or
`kill-session` against the default socket; kill stray processes by PID.

Before finishing: `gofmt -l .` prints nothing, `go vet ./...` is clean.

A change to the UI, the poller, or a tool's status rules is verified against
real sessions before the PR says so: build the binary, run it under a
throwaway `HOME` on its own socket (see Capturing a TUI frame in
[CONTRIBUTING.md](.github/CONTRIBUTING.md)), drive a real CLI in it, and read
the frames back. A green suite is not that.

## Invariants

Rules the code depends on that no compiler checks. Each one has broken a
release or a user's sessions before.

- **Every tmux call names its socket.** Go through `Driver` in
  `internal/tmux`, whose `args` pins `-L agentmgr`. A bare
  `exec.Command("tmux", ...)` resolves through `$TMUX` to the developer's own
  server. A global option, `kill-session`, or `set-hook` on a socket this
  process does not own is data loss.
- **`pane-base-index` stays 0.** `EnsureBindings` pins it so `.0` is the
  agent's pane. A CLI that splits its window (Claude Code teammates run as
  splits of the managed window) leaves the agent in pane 0, and a target
  that names the session instead of the pane lands on the wrong one.
- **Read panes through `Engine.Plain`, not `capture-pane -e`.** A running
  step's marker blinks; its off frame is a styled blank cell only the
  escape-preserving capture shows, and a rule matched on it flaps.
- **`Update` never blocks.** Bubble Tea runs one `Model`; synchronous I/O,
  exec, or a sleep on the update path goes into a `tea.Cmd`.
- **The migrations slice in `internal/store` is append-only.** The loop
  swallows "duplicate column", so an edited or reordered entry reaches fresh
  installs only while upgraded databases keep the old shape. New schema is a
  new entry at the end, written so re-running it is harmless.
- **`builtinTools` is the only source of tool definitions.** It loads on
  every start, so an edited pattern reaches every install on that release.
  `starterConfig` is written once, when no config.toml exists, so text there
  reaches new installs only; a default belongs in code.
- **`internal/update` verifies the checksum before anything is renamed over
  the running binary.** No redirect to another host, no wider permissions on
  the staged file, no write outside the staging directory.
- **Workflow actions are pinned to a release tag** and bumped by dependabot.
  A branch ref or a missing ref is a defect.
- **No speculative branches.** A fallback or nil-guard for a shape that
  cannot occur here is a defect, not safety. Verify the real shape first.
  Guards on external input are correct.

## Pitfalls

- `git stash` is repo-wide across worktrees. With several sessions on one
  checkout, a pop lands on someone else's work. Commit to the branch instead.
- Every entry in `docs/messages.json` sets `max_version` to the release it
  announces; an unbounded entry shows to every user forever.
- Release notes: a `## Highlights` or `## Thank you` bullet past 120 raw
  characters, backticks included, is cut mid-word in the messages panel.
- A tmux `send-keys` stops around 1024 bytes; paste through `load-buffer`,
  which `Driver` already does, with a buffer name that carries the pid so
  the manager, the MCP server, and the CLI never swap text between panes.

## Concurrent sessions

Multiple agent sessions share this checkout. Do feature work in an isolated
git worktree (`git worktree add`), or your edits get swept into someone
else's commit. Branch from `origin/main` after a fetch, not from the local
`main` ref.

## Releases

A push of a `v*` tag runs `.github/workflows/release.yml`: goreleaser on
Actions with the `RELEASE_TOKEN` and `AUR_KEY` secrets, then build
provenance attested over `dist/checksums.txt`. Only the admin role can push
the tag.

```bash
tag=v0.30.0                       # the release being cut
git tag "$tag" origin/main && git push origin "$tag"
gh run watch "$(gh run list --workflow release.yml --branch "$tag" --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status
```

Without `AUR_KEY` the AUR step silently skips and the release still reports
success while the Arch package goes stale, so the run log must show the
release published, the Homebrew cask pushed, and the AUR push. A failed
run after the release was created is retried from a clean tag. Fix the cause
on `origin/main`, then `gh release delete "$tag" --cleanup-tag`, which
removes the remote tag and leaves the local one on the old commit, so
recreate it: `git tag -f "$tag" origin/main && git push origin "$tag"`.

The notes carry more than the generated list of pull requests:

- **A summary in your own words**, at the top, saying what the release gives
  someone who installs it. Two or three sentences, the change first and the
  mechanism second. The generated list says which pull requests landed; it
  does not say what is different now.
- **A `## Highlights` section above `## What's Changed`**, holding short
  bullets, one per thing the release gives someone. The manager reads
  exactly those bullets into its messages panel, so write them for a modal:
  a sentence each, feature and fix language, no pull request numbers. Prose
  in that section stays on the web page; only bullets travel. A release
  without the section falls back to the generated list, filtered to
  `feat`, `fix` and `perf`.
- **A `## Thank you` section**, one bullet per person: handle, what they
  did, PR or issue number. The manager reads those bullets into the
  messages panel under the highlights, labelled Thank you. Prose in that
  section stays on the web page; only bullets travel. Cover PR authors and
  issue reporters: the generated changelog names authors only, and a fix
  exists because someone wrote the bug up. Credit the release a feature
  builds on, too, when it extends someone else's work. A range with nobody
  outside the maintainer omits the section.

Publish first, then edit: take what the workflow published with
`gh release view <tag> --json body -q .body`, put the summary, the
highlights and the thanks above its `## What's Changed`, and send it back
with `gh release edit <tag> --notes-file notes.md`.

## Layout

- `main.go` dispatches subcommands (`rename`, `review-repo`, `sessions`,
  `spawn`, `mcp`, and the rest of the workspace CLI) and boots the TUI.
- `internal/ui` is the Bubble Tea program: one `Model`, files grouped by
  feature (list, diff review, focus, quick prompt, settings).
- `internal/tmux` owns the dedicated tmux socket and control-mode client;
  `internal/store` is the SQLite state; `internal/status` classifies pane
  output into agent states; `internal/config` loads `config.toml` and the
  tool rules.
- The badges workflow publishes the clone count and contributor image to the
  `badges` branch; neither generated asset is edited by hand.

## Style

- Comments are rare and explain a non-obvious why, never what the code does.
- Tests live next to the file they cover: a test for `listview.go` belongs
  in `listview_test.go`.
