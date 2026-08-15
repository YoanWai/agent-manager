---
name: land-pr
description: Use when landing this session's agent-manager branch end to end, when the user says land the PR, create the PR and merge, merge after review, poll CodeRabbit, full review, handle CodeRabbit findings, iterate until CodeRabbit approves, or /land-pr.
---

# Land PR

Land **this checkout's branch only**. Wait out the OSS hourly limit, run a full review, handle every finding, then run another full review. Repeat until CodeRabbit approves. Then squash-merge. Repo is `YoanWai/agent-manager`.

macOS `sleep` takes seconds only.

## Pin

```bash
BRANCH=$(git branch --show-current)
```

Stop if `BRANCH` is empty or `main`. Stop if the working tree is dirty.

A `$PR` argument is allowed only as a check: it must already be an open PR whose `headRefName` equals `BRANCH`. If it does not match, stop. Do not land it.

If `$PR` is omitted:

```bash
PR=$(gh pr view --json number,headRefName,state -q "select(.headRefName==\"$BRANCH\" and .state==\"OPEN\") | .number" || true)
```

If `PR` is empty: this session's work. Re-check `git branch --show-current` equals `BRANCH`. Push this branch and open one. Do not search the repo for another PR.

Write a filled body (What / Why / Verified from this branch's commits) to a file outside the checkout, then:

```bash
git fetch origin main
git rev-list --count origin/main..HEAD   # must be > 0
git push -u origin "$BRANCH"
gh pr create --base main --head "$BRANCH" --title "$(git log -1 --format=%s)" --body-file "$NOTES"
PR=$(gh pr view --json number,headRefName,state -q "select(.headRefName==\"$BRANCH\" and .state==\"OPEN\") | .number")
```

Title stays Conventional Commits (it is the changelog line). Stop if `PR` is empty.

Never run `gh pr list` and pick one. Never land a PR whose head is not `BRANCH`.

## Wait

Read the newest issue comment by `coderabbitai[bot]` whose body contains `rate limited` or `next included review will be available in`.

Parse `N` from `Next review available in:** **N minutes**` or `available in N minutes`. `N` must be digits only. Cap at 120. Ignore the comment if the author is not the bot, `N` is invalid, or `created_at + N` minutes is already past.

If remaining minutes `R` > 0:

```bash
sleep $(( (R + 2) * 60 ))
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

15 minutes with no qualifying review: stop. Do not merge.

## Findings

Load every unresolved review thread from `coderabbitai[bot]`. For each one, read the cited code and prove the claim.

- Real, in scope, and correct for this repo: fix it, run `gofmt -l .` (must print nothing) and `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test` on the packages you touched, commit, push, reply in the thread with the SHA.
- Wrong, style-only, or against AGENTS.md / the surrounding code: reply why. Do not change the code.
- Unclear: say what you cannot verify. Do not guess.

Never apply a finding because the bot said so. Never reply "ack" or "will fix".

Then go to **Again**. Do not merge from this section.

## Again

A findings push is not approval. Thread replies are not approval. CodeRabbit resolving a thread is not approval. CI green is not approval.

After Findings, go to **Wait**, then **Trigger**. Loop Wait → Trigger → Findings until **Approved**.

## Approved

Only the latest `coderabbitai[bot]` review with `submitted_at` after the last `@coderabbitai full review` counts.

Approved when that review's `state` is `APPROVED`.

`COMMENTED` also counts when its body has `Actionable comments posted: 0` and there are zero unresolved `coderabbitai[bot]` threads.

Not approved:

- `CHANGES_REQUESTED`
- any unresolved `coderabbitai[bot]` thread
- a review submitted before the last findings push
- check title `Review rate limited`

If not approved, go to **Wait** (if limited) or **Trigger**.

## Merge

Merge only when all of these hold:

- `gh pr view "$PR" --json headRefName` is still `BRANCH`
- `headRefOid` equals the `commit_id` of the **Approved** review
- the latest CodeRabbit review is **Approved**
- CI is green
- the PR is not a draft (run `gh pr ready` first if it is)

```bash
gh pr merge "$PR" --squash --delete-branch
```

If merge is blocked, say the exact check or rule. Do not force.

## Red flags

These thoughts mean stop and run another full review:

- "Threads are answered and CI is green, merge"
- "The follow-up review is rate limited, the first review is enough"
- "CodeRabbit resolved the threads, that is approval"
- Merging after a findings push without a new `@coderabbitai full review`
