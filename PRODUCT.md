# Product

## Register

product

## Users

Developers who run several AI coding agents at once, often inside a dense terminal workflow where attention and screen space are scarce. Contributors need to extend the product without learning hidden coupling between UI state, persistence, tmux, and remote services.

## Product Purpose

agent-manager makes many independent agent sessions feel like one dependable workspace. It should reveal which sessions need attention, preserve each agent's context, and make common actions immediate without obscuring the underlying CLIs.

## Thin Wrapper Principle

agent-manager manages the workspace around agent TUIs. Each TUI owns its models, configuration, and interaction. Keep the wrapper generic and give upstream tools as little custom configuration as possible, so their upgrades do not require matching agent-manager releases.

This is a project sustainability constraint. Provider-specific features multiply the maintenance burden across features, providers, and upstream releases. A growing collection of individually useful integrations can become impossible to keep working. Supporting a TUI means making it usable in the workspace; it does not commit agent-manager to exposing every feature that provider offers.

- Prefer workspace features that work across tools with little or no provider-specific code. Evaluate the ongoing cost across all supported providers before adding an integration; ease of implementing the first provider is not enough.
- Preserve each tool's defaults and the user's configuration. Add launch flags or configuration only when required for session management or explicitly requested by the user; do not choose a model on their behalf.
- Do not hardcode model names, model catalogs, or other values that change with upstream releases. Leave those choices in the underlying TUI unless a documented, stable interface can discover them dynamically.
- Design shared controls for all supported TUIs. Dynamic discovery for one tool alone does not justify a shared model selector: establish reliable discovery across the supported tools and how unavailable capabilities are handled before adding the control.
- Prefer stable, documented interfaces. Avoid version checks, private configuration formats, and parsing human-facing output to recreate upstream settings. If a feature needs those mechanisms, keep it in the underlying TUI.
- Status detection already carries an ongoing maintenance cost. Keep necessary tool-specific detection isolated; it is not a precedent for adding more coupling. Missing discovery or an unrecognized status must leave ordinary launch and interaction usable.
- Judge every integration by what happens when an upstream tool updates, across all supported TUIs. Verify the interface and failure behavior; prefer a smaller dependable feature over one that needs recurring fixes for individual tools or versions.

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
