# Review data without the Git runtime

Review consumes diff data, not a Git process runner. The original `internal/diff` package combined those responsibilities. Its model now lives in a separate package:

| Package | Ownership |
| --- | --- |
| `internal/git/value` | Scope, repository, changed-file, stat, and worktree values |
| `internal/diff/model` | Diff sets, file and line values, pure content construction, pairing, changed spans, and side-by-side rows |
| `internal/git` | Concrete Git subprocesses and filesystem discovery |
| `internal/diff` | Loading changed-file metadata and contents through the concrete Git driver |

Existing adapter-facing types are aliases to the value/model packages. Scope names and numeric ordering retain their stored meaning. The loader preserves known-stat and loaded-state handling, binary detection, truncation, and untracked-file counting. This is a source dependency split; it introduces no remote protocol or storage migration.

Pure content tests live beside the line-model implementation. Runtime tests remain beside the Git/diff adapters. The compiled line-model race tests pass with `PATH=/nonexistent`, while adapter tests continue to exercise real repositories.

Run the enforced production dependency check:

```sh
go run ./tools/architecture/check-ui-boundaries
```

The pure model may depend on Git values; neither may import the concrete Git runtime, store, tmux, execution, application composition, or root UI. The check uses the transitive production graph. It does not claim to enforce test imports or historical-client compatibility.
