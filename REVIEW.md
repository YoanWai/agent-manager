# Review policy

What a review of this repository is for. It applies to every reviewer, human
or automated. The per-path rules live in `.coderabbit.yaml`, the invariants in
`AGENTS.md`, and the product intent in `PRODUCT.md`.

A review finds defects the diff introduces or exposes.

Scope belongs to the maintainer. Every pull request carries a Scope section:
required behavior, non-goals, and why this approach. Compare the diff against
it, and report behavior, public surface, or state that none of them asks for.

When the need for a change depends on product intent that nobody wrote down,
ask for a maintainer decision. An unanswered question stays a question.
