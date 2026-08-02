# Product

## Register

product

## Users

Developers who run several AI coding agents at once, often inside a dense terminal workflow where attention and screen space are scarce. Contributors need to extend the product without learning hidden coupling between UI state, persistence, tmux, and remote services.

## Product Purpose

agent-manager makes many independent agent sessions feel like one dependable workspace. It should reveal which sessions need attention, preserve each agent's context, and make common actions immediate without obscuring the underlying CLIs.

## Brand Personality

Precise, quiet, and capable. The interface should feel like a well-made terminal instrument: dense without being cramped, expressive without decoration, and trustworthy under sustained use.

## Anti-references

- Workarounds that patch one screen while leaving competing sources of truth.
- Hidden asynchronous behavior that makes fresh state look stale.
- Modal dumps that expose implementation details instead of a readable summary.
- Decorative SaaS patterns, novelty controls, and animation without state meaning.
- Contributor APIs that require knowledge of unrelated UI internals.

## Design Principles

- Build systems with one authoritative model and explicit boundaries between remote data, cached data, and rendered state.
- Keep the hot path local and bounded; network work is asynchronous, cached, and never blocks input or paint.
- Preserve information across skipped versions, then summarize it progressively instead of forcing sequential actions.
- Make compact and expanded views share one identity and vocabulary.
- Prefer small, testable types and pure transformations that contributors can understand in isolation.

## Accessibility & Inclusion

The product is keyboard-first and must remain usable in narrow terminals and limited color environments. Shape and text carry meaning alongside color. Focus, selection, loading, errors, and available actions stay explicit, and prose is wrapped to a readable terminal measure.
