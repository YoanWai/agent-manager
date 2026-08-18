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

## Releases

Cut from a clean worktree at the tag, with goreleaser run locally; there is
no release workflow in CI.

```bash
tag=v0.30.0                       # the release being cut
git tag "$tag" && git push origin "$tag"
git worktree add /tmp/amrel "$tag"
cd /tmp/amrel && GITHUB_TOKEN="$(gh auth token)" AUR_KEY="$HOME/.ssh/aur_agent_manager" \
  goreleaser release --clean
```

`AUR_KEY` is what publishes the Arch package: without it `git_url` templates
to empty, goreleaser skips that step, and the release still reports success
while the AUR package goes stale.

The notes carry more than the generated list of pull requests:

- **A summary in your own words**, at the top, saying what the release gives
  someone who installs it. Two or three sentences, the change first and the
  mechanism second. The generated list says which pull requests landed; it
  does not say what is different now.
- **Thanks to every contributor in the range, by handle.** Read the merged
  pull requests, not only the generated list, and name what each one did.
- **Thanks to whoever reported what got fixed, by handle.** A bug someone
  took the time to write up is why the fix exists, and the reporter is
  usually not the author. Credit the release a feature builds on, too, when
  it extends someone else's work.

A range with nobody outside the maintainer in it carries no thanks line;
write the summary and leave it at that rather than manufacturing one.

`--release-notes=notes.md` replaces the generated list of pull requests
rather than sitting above it, so a file passed that way has to carry the
list itself. Publishing first and then editing is the simpler order: take
what goreleaser published with `gh release view <tag> --json body -q .body`,
put the summary and thanks above its `## What's Changed`, and send it back
with `gh release edit <tag> --notes-file`. The header and footer from
`.goreleaser.yaml` come along either way.

## Layout

- `main.go` dispatches subcommands (`rename`, `review-repo`, `review-base`,
  `mcp`) and boots the TUI.
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
- No speculative branches for inputs that cannot occur; verify the real
  shape first.
- Tests live next to the file they cover: a test for `listview.go` belongs
  in `listview_test.go`.
