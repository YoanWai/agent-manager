<!-- The PR title becomes a changelog line. Conventional Commits, e.g. feat(ui): add mouse support -->

## What changed

## Why

Closes #

## Scope

<!--
What the change has to do and why this shape. A typo or a one-line fix
answers both in a sentence.
-->

### Required behavior

### Why this approach

### Tool coverage

<!--
For a shared feature, account for every tool in builtinTools with its new
behavior and verification, or evidence that the feature does not apply.
A table is useful. Keeping old behavior is partial coverage.
For a tool-specific fix or new CLI, name the tools and why the scope is narrow.
If you cannot or do not plan to cover other applicable tools, open an issue
first. An optional reference implementation stays in a draft PR linked to
that issue. Say it is for reference only and name the missing coverage.
Mark it ready only after completing coverage or obtaining an accepted exception.
Before requesting merge of a partial feature, give the upstream evidence, the
shared alternative considered, maintenance cost, and unsupported behavior. Link the
maintainer decision accepting the exception, or state it explicitly if you
are the maintainer. See REVIEW.md#tool-coverage.
For changes with no tool-dependent behavior, say so in one sentence.
-->

## How it was verified

<!-- Commands you ran, sessions you drove, and what you saw. -->

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes
- [ ] `mkdir -p /tmp/amtest && env -u TMUX TMUX_TMPDIR=/tmp/amtest go test -race ./...` passes
- [ ] `go build ./...` passes
- [ ] Ran it against real sessions
- [ ] New behavior covers every applicable CLI, or Scope establishes a tool-specific fix, new CLI, or explicitly accepted feature exception
- [ ] Holds for every supported platform (macOS, Linux, WSL2) and terminal including over SSH, or the description names what it leaves out
- [ ] Every new action works by keyboard and by mouse, and every new setting lives on a Settings row

## Visual evidence

<!--
Add before and after screenshots whenever the change can be shown visually.
A short recording can replace them when interaction or motion matters more.
If useful visual evidence is not possible, explain why.
-->

**Before:**

**After:**

**Not applicable because:**
