# Local Go test gates

Use the fast gate while changing policy, storage, extracted UI features, or their dependency boundaries:

```sh
./tools/testing/fast.sh
```

Use the process/race matrix when a change can cross a goroutine, SQLite connection, application runner, or tmux boundary:

```sh
./tools/testing/process-race.sh
```

Both commands require Go and tmux. They run uncached tests with the race detector, unset inherited tmux identity, use `/bin/sh` with a clean `ZDOTDIR`, and allocate short private `TMUX_TMPDIR` paths. The process matrix runs each package sequentially under a different socket directory. It never issues a command against the default tmux server. A failed process phase retains its printed fixture directory for diagnosis.

## What the fast gate proves

The first phase runs package-level tests for agent-session parsing, configuration, pure diff values, status policy, the SQLite store, four extracted UI features plus presentation, and both production dependency guards. The second phase runs twelve session command task, reservation, group, and transactional claim tests. Those command tests use a real config, SQLite store, command binding, and independent command connections for racing claims. They construct a tmux driver but do not start a pane or server.

On the macOS development host on 2026-10-01, the final uncached `-race -count=1` selection passed in 26.40 seconds wall clock. The parallel package phase's slowest reported package was `internal/store` at 19.750 seconds; the second phase ran its 12 selected session command tests in 4.634 seconds. This run immediately preceded final corrections in the root `internal/ui` package, which the fast gate does not select; the gate and every package it selects were unchanged, so it was not repeated. An earlier broad alternative that ran every package except `internal/ui`, `internal/sessioncmd`, `internal/execution`, and `internal/tmux` took 20.1 seconds before the final store and architecture tests landed. That historical comparison selected the narrower boundary-focused gate; it is not a current speed comparison. These are local measurements, not hosted CI timings.

## What the process/race matrix proves

| Phase | Top-level tests | Selected boundary | Observed uncached race time |
| --- | ---: | --- | ---: |
| Session commands | 5 | Real spawn, kill/revive, queued message/read, terminal I/O, and deletion of a group with a live member | 8.284 s |
| Execution | 6 | Blocked-subscriber cancellation, two-manager takeover, guarded delivery admission, and refused, uncertain, or confirmed outcomes | 3.624 s |
| UI | 37 | Accepted job draining; blocked adapters; acknowledged ordered input; installer uncertainty; quick send; Review preferences/base; notice, split, and editor fences; prepared attach and typed liveness failures | 12.218 s |
| tmux | 11 | Concurrent control-client paste, cross-process attach and command serialization, bounded close/reap, typed liveness, sizing, text send, and lifecycle | 12.556 s |

The final 59-test sequential script took 39.35 seconds wall clock on the same host. Test names are explicit so changes to a boundary require a deliberate selection update. Each phase checks the number of selected top-level tests, so a rename or removal fails the gate instead of silently shrinking it. The matrix validates cooperating automatic-delivery owners after the documented offline cutover; it does not extend that authority to human pane input, legacy writers, or remote clients.

## Full and release gates remain mandatory

These commands shorten local feedback; they do not replace the repository test suite or change CI. Before integration, run the complete race suite with isolated tmux state:

```sh
fixture_root=$(mktemp -d /tmp/am-race.XXXXXXXX)
mkdir -p "$fixture_root/zsh"
env -u TMUX -u TMUX_PANE TMUX_TMPDIR="$fixture_root" ZDOTDIR="$fixture_root/zsh" SHELL=/bin/sh \
  go test -race ./...
```

Run the [real TUI harness](../e2e/README.md) for actual-binary UI and terminal wiring. Run the [compatibility gates](../compatibility/README.md) for released clients, mixed-version task mutations, and two real manager processes. Those suites retain their own isolated profiles and artifacts. Real provider installers and session stores, installed extensions, mutation families beyond the named contracts, human pane-input authority, remote or saved-connection authorization, crash recovery, and the supported platform matrix remain separate acceptance work.
