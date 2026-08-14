---
name: coderabbit-merge
description: Use when finishing an agent-manager pull request that is waiting on CodeRabbit's OSS hourly review limit, or when the user says poll CodeRabbit, wait for the next review, full review, handle CodeRabbit findings, or merge after CodeRabbit. Also use for /coderabbit-merge.
---

# CodeRabbit merge

Wait out the OSS hourly limit, run a full review, verify every finding, then squash-merge. Repo is `YoanWai/agent-manager`. PR is the argument, or the open PR for the current branch.

macOS `sleep` takes seconds only.

## Wait

Read the newest issue comment whose body contains `rate limited by coderabbit.ai`.

Parse `Next review available in:** **N minutes**`.

If that comment exists and N > 0:

```bash
sleep $(( (N + 2) * 60 ))
```

One background sleep. Do not poll GitHub during the wait. After it ends, re-read. Still limited: parse again and sleep once more. Tell the user the wait, then keep going. Do not ask them to come back.

No rate-limit comment: continue.

## Trigger

```bash
gh pr comment "$PR" --body '@coderabbitai full review'
```

Then, at most every 60s for 15 minutes, wait until both are true:

- a `coderabbitai[bot]` review exists with `submitted_at` after that comment
- the CodeRabbit check title is not `Review rate limited`

Rate-limited again: go back to Wait.

## Findings

Load every unresolved review thread from `coderabbitai[bot]`. For each one, read the cited code and prove the claim.

- Real, in scope, and correct for this repo: fix it, run `gofmt` and `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test` on the packages you touched, commit, push, reply in the thread with the SHA.
- Wrong, style-only, or against AGENTS.md / the surrounding code: reply why. Do not change the code.
- Unclear: say what you cannot verify. Do not guess.

Never apply a finding because the bot said so. Never reply "ack" or "will fix".

## Merge

Merge only when all of these hold:

- every CodeRabbit thread is fixed or answered
- CI is green
- the PR is not a draft (run `gh pr ready` first if it is)

```bash
gh pr merge "$PR" --squash --delete-branch
```

If merge is blocked, say the exact check or rule. Do not force.
