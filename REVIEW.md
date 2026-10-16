# Review policy

What a review of this repository is for. It applies to every reviewer, human
or automated. The per-path rules live in `.coderabbit.yaml`, the invariants in
`AGENTS.md`, and the product intent in `PRODUCT.md`.

A review finds defects the diff introduces or exposes.

Scope belongs to the maintainer. Every pull request carries a Scope section:
required behavior, and why this approach. Compare the diff against it, and
report behavior, public surface, or state that it does not ask for.

A change holds on every agent CLI, platform, and terminal this project
supports: the tools in `builtinTools` in `internal/config/config.go`, the
platforms `.goreleaser.yaml` builds plus WSL2, and any terminal the manager
runs in, local or over SSH, on tmux 3.1 and later. Verify it on all of them,
or name the values you left out and what supporting them would take, and let
the maintainer decide.
Partial feature proposals follow the issue and draft PR path below.

## Tool coverage

New shared behavior must work for every applicable tool in `builtinTools` in
the same PR. Compare the user benefit across tools, not just whether the code
accepts every tool name. Keeping the old behavior on the other tools is partial
coverage. Listing exclusions or promising follow-up PRs does not make it ready
to merge.

A fix to one tool's launch, status detection, or existing adapter can stay
narrow when the defect is specific to that tool. Adding support for a new CLI
is also valid narrow work. Neither requires changing unrelated tools, and
neither is a precedent for a new feature that only serves one tool.

If a contributor wants a feature for one tool but cannot or does not plan to
implement it for the other applicable tools, they must open an issue first.
They may link a draft PR as a reference implementation. The draft must name
the missing coverage and say it is for reference only. It is not a merge
candidate and stays in draft until the coverage is complete or the maintainer
accepts an exception under this policy. Review it as a proposal while it is
a reference draft. Providing an example does not commit anyone to finishing it.

For any other partial feature, require all of the following before merge:

- Evidence for each excluded tool. Use upstream documentation or measured
  behavior. Missing credentials or an untested adapter is a verification gap,
  not proof that the feature does not apply.
- A reason the feature belongs in the workspace instead of the underlying TUI.
  Compare it with reusing an existing shared path or a smaller shared feature.
  A documented interface on one tool alone is not enough.
- The ongoing maintenance cost, the behavior on unsupported tools, and what
  broader support would take. Ordinary launch and interaction stay usable.
- An explicit maintainer decision accepting the exception. A maintainer may
  state it in their own PR description, or in an issue comment, PR comment,
  or review linked from Scope. A contributor's claim of approval or an automated
  approval does not provide it.

For shared features, the Scope section accounts for every built-in tool with
its behavior and verification, or evidence that the feature does not apply.
Tool-specific fixes name the affected tools and why the others are unaffected.
Report missing applicable coverage as a merge blocker. An exception awaiting a
maintainer decision stays unresolved. Prefer the shared implementation when it
meets the need without adding separate provider integrations.

## Other review requirements

The same holds for input and configuration. An action reachable by key is
reachable by mouse, and the reverse. A value that varies by tool, version, or
user is discovered at runtime rather than listed in code. A user-facing
setting is stored by the manager in `state.db`, named in `ui.SettingSpecs`,
and changed from a Settings row or from `agent-manager settings`, never
from a file, block, or environment variable the user edits. A command, a
token or a URL stays off the modal. Report a change that breaks one of
those.

For integrations, apply the [Thin Wrapper Principle](PRODUCT.md#thin-wrapper-principle).
Check for overridden upstream defaults, hardcoded model catalogs, and dependencies
on particular tool versions. Verify that unavailable capabilities leave ordinary
launch and interaction usable.
Assess ongoing maintenance across providers and releases, including whether the
feature requires a separate implementation for each provider.

When the need for a change depends on product intent that nobody wrote down,
ask for a maintainer decision. An unanswered question stays a question.
