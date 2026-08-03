# Contributing to agent-manager

Thanks for taking an interest. Bug reports, feature ideas, and pull requests are all welcome.

## Ways to help

- **Report a bug** with the [bug report form](https://github.com/YoanWai/agent-manager/issues/new?template=bug_report.yml).
- **Suggest a feature** with the [feature request form](https://github.com/YoanWai/agent-manager/issues/new?template=feature_request.yml).
- **Ask a question or show what you built** in [Discussions](https://github.com/YoanWai/agent-manager/discussions).
- **Add status rules for a CLI tool** you use. Tool support is config-driven, so a `[tools.<name>]` block with good detection rules helps everyone running that agent.

## Before you open a pull request

Send it. Typos, broken links, a one-line fix, a status rule for a tool you use: straight to a pull request is fine, and a rough patch that works is worth more than a perfect one you never open.

For a large change (new UI, new keybinding, reworked status detection), an issue first gets you a read on the approach so the work lands the first time. Optional, and worth it.

## Setup

You need Go 1.26+ and tmux.

```bash
git clone https://github.com/YoanWai/agent-manager.git
cd agent-manager
go run .
```

The manager runs its sessions on a dedicated tmux socket (`-L`), so a development build stays clear of the tmux server your own shell is attached to.

## Checks

CI runs these four on every pull request. Run them locally first:

```bash
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
go build ./...
```

The test suite includes end-to-end tests against a real tmux server on its own socket, so tmux must be installed for `go test` to pass.

## Code style

- Match the surrounding code. Names describe intent; comments explain a non-obvious *why* and stay rare.
- Keep changes focused on one thing. A PR that fixes a bug and refactors three packages is two PRs.
- Add tests for behavior you change, next to the package you touched.

## Commits and pull requests

Commit subjects follow Conventional Commits, matching the existing history:

```
feat(ui): add mouse support to the sessions rail
fix(status): treat an Esc interrupt as waiting
docs: document the review-base subcommand
```

Release notes are generated from merged pull request titles, so give the PR the title you want readers to see in the changelog.

The TUI turns those generated notes into cumulative update summaries
automatically. Do not duplicate feature and fix notes in the maintainer message
feed; see [Release summaries and messages](../docs/notifications.md) for the two
publishing paths.

In the pull request description, say what changed and why, and how you verified it. Screenshots or a short recording make UI changes much easier to review.

## Licensing

Contributions come in under the project's [Apache-2.0 license](../LICENSE), same as everything else here. There is nothing extra to sign: section 5 of the license already places every contribution under those terms.

Apache-2.0 carries a patent grant and an indemnity clause, so contributing to this project also licenses your patent claims over your own contribution and keeps anyone who sells support around your work from putting that liability on you.

## Review

@YoanWai reviews and merges everything (see [CODEOWNERS](CODEOWNERS)). Expect a first response within a few days.

## Code of Conduct

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md).
