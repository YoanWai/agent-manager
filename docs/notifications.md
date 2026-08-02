# Release summaries and messages

The messages panel has two deliberately separate sources. Release changes come
from GitHub Releases; maintainer messages come from `docs/messages.json`.

## Shipping a feature or fix

Write a clear Conventional Commit title for the pull request:

```text
feat(worktree): renaming a session moves its worktree and branch
fix(groups): make group creation immediate and reliable
```

GitHub adds merged pull request titles to the release's generated **What's
Changed** section. agent-manager reads that section, removes the commit prefix,
author, and pull request URL, then shows the result in the messages modal. There
is no second changelog or message entry to maintain.

When an install skips releases, the modal groups the intervening releases in the
retained catalog into one summary. If the installed version is older than that
window, the modal marks the summary as partial and links to the complete notes.
Updating remains one operation straight to the latest version. After restart,
the same cached catalog explains what was installed.

The client bounds remote data to keep rendering predictable: 100 stable
releases, 12 summarized changes per release, and 120 characters per change.
The full release page remains one keypress away when a release exceeds those
limits.

## Publishing an editorial message

Use `docs/messages.json` only for information that is not an ordinary release
change: a known issue, a time-sensitive migration warning, or a project
announcement. The feed is polled independently of the installed version and can
target a version range.

```json
[
  {
    "id": "known-issue-0180",
    "banner": "Known issue in v0.18.0",
    "title": "Known issue in v0.18.0",
    "body": [
      "What users will observe.",
      "The safe action to take while a fix is prepared."
    ],
    "url": "https://github.com/YoanWai/agent-manager/issues/234",
    "min_version": "v0.18.0",
    "max_version": "v0.18.0",
    "expires_at": "2026-08-09T12:00:00Z"
  }
]
```

Rules:

- `id` is permanent, lowercase kebab-case, and unique. Changing it resurfaces a
  dismissed message.
- `title` is the canonical copy shown in both the compact card and modal.
- `banner` must mirror `title` while older clients still read that legacy field.
- `body` explains impact and action in short plain-text lines.
- `url` is optional and must be HTTPS.
- `min_version` and `max_version` are inclusive and optional. Time-sensitive or
  version-specific messages should have narrow version bounds.
- `expires_at` is an optional RFC 3339 timestamp. Expired or invalid timestamps
  are rejected by the client; every time-sensitive message must set one.

Pressing `r` in the messages modal bypasses both local cache ages and refreshes
the release catalog and editorial feed from GitHub. Normal background checks do
no network or parsing work on the render path. The release catalog checks every
10 minutes with an HTTP ETag, while the editorial feed's cache lasts 6 hours;
both schedules are independent of the installed version.
