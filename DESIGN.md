---
name: agent-manager
description: A precise terminal workspace for many AI coding agents.
colors:
  background: "#0f1115"
  surface: "#232830"
  overlay: "#2d333d"
  bright: "#eceff4"
  text: "#c6ccd6"
  dim: "#98a0ac"
  subtle: "#646c78"
  accent: "#6f9fd0"
  accent-secondary: "#6cb6a4"
  working: "#d08442"
  waiting: "#a78bd0"
  finished: "#85b26f"
  error: "#cc6a6a"
typography:
  title:
    fontFamily: "terminal monospace"
    fontWeight: 700
    lineHeight: 1
  body:
    fontFamily: "terminal monospace"
    fontWeight: 400
    lineHeight: 1
  label:
    fontFamily: "terminal monospace"
    fontWeight: 700
    lineHeight: 1
spacing:
  cell: "1ch"
  compact: "2ch"
  section: "3ch"
components:
  selected-row:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.bright}"
    typography: "{typography.body}"
  modal:
    backgroundColor: "{colors.background}"
    textColor: "{colors.text}"
    typography: "{typography.body}"
---

# Design System: agent-manager

## Overview

**Creative North Star: "The Operator's Field Desk"**

agent-manager is a dense working surface used for hours, not a presentation layer. Structure comes from alignment, tonal surfaces, concise labels, and persistent spatial relationships. The interface stays quiet until state requires attention.

Key characteristics: terminal-native, keyboard-first, information-dense, restrained, and deterministic. Avoid decorative SaaS patterns, novelty controls, modal dumps, and visual treatments that disguise stale or ambiguous state.

## Colors

Every theme implements the same semantic roles. The classic palette above is the reference, while code must resolve colors through `Theme` tokens rather than hard-coded values.

- **Accent:** focus, key hints, and the highest-priority action.
- **Secondary accent:** groups, scopes, and supporting structure.
- **Bright/Text/Dim/Subtle:** a four-step reading hierarchy.
- **Working/Waiting/Finished/Error/Idle:** agent state, always paired with a distinct glyph or label.

The warm messages surface is reserved for notices. Accent color is state, never decoration.

## Typography

The user's terminal monospace is the only typeface. Hierarchy comes from weight, color role, case, and whitespace.

- **Title:** bold, short, and unique within a surface.
- **Body:** regular weight, wrapped to roughly 65–75 columns when prose is the focus.
- **Label:** bold or dim according to importance; uppercase is reserved for the product wordmark and compact badges.

Compact and expanded representations use the same canonical title. Do not substitute unrelated teaser copy when a surface expands.

## Elevation

There are no shadows. Depth is structural: backdrop, surface, overlay, border, and selection tones. A modal earns its raised role through a complete frame and centered placement; nested cards are not used.

## Components

### Session rail

Rows retain a stable identity while asynchronous state changes. Selection uses a full-width surface fill. Status combines glyph, color, and text.

### Messages panel

The rail shows canonical notice titles in a bounded fieldset. Overflow is summarized numerically; opening the panel preserves the selected notice by ID.

### Notices modal

The modal presents a short list first, then a readable summary of the selected notice. Release notices group changes by version, show the direct upgrade path, and cap visible detail with an explicit remainder rather than clipping silently.

### Inputs and actions

Focus is explicit. Key hints name the action next to the key. Loading and failure states replace action copy in place without moving the surrounding layout.

## Do's and Don'ts

### Do:

- **Do** resolve every color through the active semantic theme.
- **Do** keep input and paint paths local, deterministic, and bounded.
- **Do** use one canonical title from the compact panel through the modal.
- **Do** summarize remote information before rendering it.
- **Do** pair status color with a glyph or label.

### Don't:

- **Don't** patch one screen while leaving competing sources of truth.
- **Don't** block the event loop on network or filesystem work.
- **Don't** expose raw Markdown or API boilerplate in the modal.
- **Don't** use decorative SaaS patterns, novelty controls, or animation without state meaning.
- **Don't** require contributors to coordinate unrelated UI internals to add a release-visible feature.
