# Review policy

What a review of this repository is for. It applies to every reviewer, human
or automated. The per-path rules live in `.coderabbit.yaml`, the invariants in
`AGENTS.md`, and the product intent in `PRODUCT.md`.

A review finds defects the diff introduces or exposes.

Scope belongs to the maintainer. Every pull request carries a Scope section:
required behavior, and why this approach. Compare the diff against it, and
report behavior, public surface, or state that it does not ask for.

A change holds on every agent CLI and every platform this project supports:
the tools in `defaultConfig` in `internal/config/config.go`, and the platforms
`.goreleaser.yaml` builds. Verify it on all of them, or name the values you
left out and what supporting them would take, and let the maintainer decide.
Opening the pull request first and settling that in review is the right order.

For integrations, apply the [Thin Wrapper Principle](PRODUCT.md#thin-wrapper-principle).
Check for overridden upstream defaults, hardcoded model catalogs, and dependencies
on particular tool versions. Verify that unavailable capabilities leave ordinary
launch and interaction usable.
Assess ongoing maintenance across providers and releases, including whether the
feature requires a separate implementation for each provider.

When the need for a change depends on product intent that nobody wrote down,
ask for a maintainer decision. An unanswered question stays a question.
