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

Follow the [Thin Wrapper Principle](../PRODUCT.md#thin-wrapper-principle): preserve
each agent TUI's defaults and user configuration, keep shared features generic,
and avoid integrations that need updates whenever an upstream tool changes.
For example, a model picker needs reliable dynamic discovery across supported
tools; a maintained list of model names does not belong in agent-manager.

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
env -u TMUX TMUX_TMPDIR=/tmp/amtest go test -race ./...
go build ./...
```

The test suite includes end-to-end tests against a real tmux server on its own socket. Install tmux to run those tests locally; tmux-dependent tests are skipped when tmux is unavailable. `env -u TMUX` is required when your shell is already inside tmux, or the tests land on that server.

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

The TUI reads **Highlights** and **Thank you** from the GitHub release notes
into the messages panel. Do not duplicate those notes in the maintainer message
feed; see [Release summaries and messages](../docs/notifications.md) for the two
publishing paths.

In the pull request description, say what changed and why, and how you verified
it. Fill the Scope section: what the change has to do and why this shape. A typo
or a one-line fix answers both in a sentence, and a review reads them to tell
the change you meant from the change you made. Say there too when the change
reaches only some of the agent CLIs or platforms we support, and what covering
the rest would take. If the change grew out of a Discussion, or you opened one
about it, link it there.
Complete the Visual evidence section for every pull request. Include before
and after screenshots whenever the change can be shown visually; use a short
recording when interaction or motion is clearer that way. If useful visual
evidence is not possible, say why. For example, a new UI may have no
reproducible before state, while an internal refactor may have no meaningful
visual state at all.

## Licensing

Contributions come in under the project's [Apache-2.0 license](../LICENSE), same as everything else here. There is nothing extra to sign: section 5 places any contribution you intentionally submit for inclusion under those terms unless you state otherwise.

Two clauses are worth knowing as a contributor. Section 3 licenses the patent claims you can license that your own contribution necessarily infringes, on its own or combined with the project, and it withdraws that grant from anyone who sues over the work. Section 9 lets a redistributor sell support or a warranty only on its own behalf, and requires it to indemnify, defend and hold contributors harmless for what it took on.

## Review

[REVIEW.md](../REVIEW.md) states what a review here covers, so you can see what a finding will be about before you open the PR. CodeRabbit reviews every pull request automatically, usually within minutes. Work through its findings first: fix what it got right, and reply on the comment when you disagree, so the thread records why. Maintainer review starts once the CodeRabbit round is handled; a PR with open, unanswered findings waits.

@YoanWai reviews and merges everything (see [CODEOWNERS](CODEOWNERS)). Expect a first response within a few days.

## Code of Conduct

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md).
