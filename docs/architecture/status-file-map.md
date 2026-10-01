# Status classification file map

The status package keeps one compiled Engine and its existing public contract.
The split groups code by the decisions it owns; it does not add forwarding
interfaces or alter provider configuration.

| File | Responsibility |
| --- | --- |
| `status.go` | Engine construction, compiled rules and core matching |
| `classification.go` | State classification, typing holds and turn-state policy |
| `region.go` | Activity-region boundaries and extracted last-message values |
| `transcript.go` | Turn transcript selection and user-echo boundaries |
| `composer.go` | Input draft and composer prefix interpretation |

Behavioral tests stay beside each concern. This was a mechanical split from
`7c7c589`: the source comparator verified all 123 declarations, 409 comment
tokens and 90 exported names, with no init functions. Reproduce against this
worktree before committing:

```sh
go run ./tools/architecture/check-ui-moves -root internal/status 7c7c589 WORKTREE
go test ./internal/status ./tools/architecture/check-ui-moves
```

After committing, replace WORKTREE with the commit being reviewed. The original
UI comparison remains the default; `-root` selects another Go package.
