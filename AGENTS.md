# Agent notes

Project context for coding agents. Human setup and PR conventions live in
[.github/CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Build and test

```bash
go run .                                        # run the TUI
go build ./...
env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./...
```

The suite drives a real tmux server. When your own shell already runs inside
tmux, `$TMUX` overrides `TMUX_TMPDIR` and the tests land on the live socket,
so `env -u TMUX` is mandatory, never a bare `go test`. Keep the socket dir
short: `TMUX_TMPDIR/tmux-<uid>/default` must stay under 104 characters or
tmux silently falls back to the default socket. Never run `tmux kill-server`
or `kill-session` against the default socket; kill stray processes by PID.

Before finishing: `gofmt -l .` prints nothing, `go vet ./...` is clean.

## Concurrent sessions

Multiple agent sessions share this checkout. Do feature work in an isolated
git worktree (`git worktree add`), or your edits get swept into someone
else's commit. Branch from `origin/main` after a fetch, not from the local
`main` ref.

## Layout

- `main.go` dispatches subcommands (`rename`, `review-repo`, `review-base`,
  `mcp`) and boots the TUI.
- `internal/ui` is the bubbletea program: one `Model`, files grouped by
  feature (list, diff review, focus, quick prompt, settings).
- `internal/tmux` owns the dedicated tmux socket and control-mode client;
  `internal/store` is the SQLite state; `internal/status` classifies pane
  output into agent states; `internal/config` loads `config.toml` and the
  tool rules.
- `docs/badges/` is written by the badges workflow, not by hand.

## Style

- Comments are rare and explain a non-obvious why, never what the code does.
- No speculative branches for inputs that cannot occur; verify the real
  shape first.
- Tests live next to the file they cover: a test for `listview.go` belongs
  in `listview_test.go`.
