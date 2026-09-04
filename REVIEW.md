# Review policy

This file states what a review of this repository is for. It applies to every
reviewer, human or automated.

## What a review covers

Review defects the diff introduces or exposes: a wrong result, a race, a leak,
a lost session, a silent failure, a break in a documented contract. Read
`PRODUCT.md` for what the product is for and `AGENTS.md` for the invariants the
code already relies on.

## Scope belongs to the maintainer

A pull request carries a Scope section: required behavior, non-goals, and why
this approach. Compare the diff against it. Behavior, public surface, or state
that no one asked for is a finding, whatever its quality.

When the need for a change depends on product intent that nobody wrote down,
ask for a maintainer decision. An unanswered question stays a question.

## Prefer less code

A guard, fallback, retry, helper, abstraction, or new field earns its place by
a reachable caller or an external input that requires it. Name that caller or
input in the review. Deletion and reuse beat new machinery.

Treat the invariants in `AGENTS.md` and the package rules in `.coderabbit.yaml`
as contracts: code that upholds them is finished, not thin.

## Out of scope for review

Formatting `gofmt` owns, import order, doc comments on exported identifiers,
and renames of names whose meaning a reader can already recover.
