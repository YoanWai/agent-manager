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

## How it was verified

<!-- Commands you ran, sessions you drove, and what you saw. -->

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes
- [ ] `mkdir -p /tmp/amtest && env -u TMUX TMUX_TMPDIR=/tmp/amtest go test -race ./...` passes
- [ ] `go build ./...` passes
- [ ] Ran it against real sessions
- [ ] Holds for every supported agent CLI, platform (macOS, Linux, WSL2), and terminal including over SSH, or the description names what it leaves out
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
