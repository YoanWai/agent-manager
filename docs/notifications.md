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
Changed** section.

The messages panel reads four authored parts of the same notes, all optional:

```markdown
## v0.40.0
**Model + reasoning pickers are here!**

Two or three sentences of summary. `Backticks` put a word in the accent color.

## Highlights
- One sentence each. `Backticks` put a word in the accent color.

## Thank you
- @handle did X (#123).
```

- **Headline**: a bold first line under the version heading. It opens the
  notice in bold over an accent rule.
- **Summary**: the paragraph under it. It opens the newest release in the
  notice and wraps at 76 columns.
- **Highlights**: short bullets naming what the release gives someone. A
  sentence each, feature and fix language, no pull request numbers.
- **Thank you**: one bullet per contributor or reporter, with handle and the
  PR or issue.

The newest release lists its Features, Fixes and Other changes between its
Highlights and its Thank you lines. The panel sorts What's Changed into those
groups by Conventional Commit type, with `perf` under Features, and credits
outside authors on their row. An older release in a range shows its Highlights
and a count of the rest, or its whole list when it has no Highlights. `docs`,
`chore`, `test`, `ci`, `build`, `refactor` and `style` lines stay on the web
page. There is no second changelog in `docs/messages.json` to maintain.

Two notices read these notes. "vX available" is drawn by the version a person
is still on, and "Updated to X" by the new one, so notes are written for both
panels. v0.35.0 through v0.39.0 show the first 6 Highlights and 8 thank-you
lines, cut each line at 120 visible characters, and show neither the headline
nor the summary. v0.18.0 through v0.34.0 draw the first 12 rows of What's
Changed, of every type, each cut at 120. Put what matters first and keep lines
within 120 while those versions are in use.

When an install skips releases, the modal groups the intervening releases in the
retained catalog into one summary. If the installed version is older than that
window, the modal marks the summary as partial and links to the complete notes.
Updating remains one operation straight to the latest version. After restart,
the same cached catalog explains what was installed.

The client bounds remote data to keep rendering predictable: 100 stable
releases, 8 highlights, 24 thanks and 100 changes per release, 160 visible
characters per line, 80 for the headline and 600 for the summary. Backticks are
not counted. The full release page remains one keypress away when a release
exceeds those limits.

## Publishing an editorial message

Use `docs/messages.json` only for information that is not an ordinary release
change: a known issue, a time-sensitive migration warning, or a project
announcement. The feed is polled independently of the installed version and can
target a version range.

```json
[
  {
    "id": "known-issue-0400",
    "banner": "Known issue in v0.40.0",
    "title": "Known issue in v0.40.0",
    "headline": "Known issue in v0.40.0",
    "accent": ["safe action"],
    "body": [
      "What users will observe.",
      "The safe action to take while a fix is prepared."
    ],
    "url": "https://github.com/YoanWai/agent-manager/issues/234",
    "min_version": "v0.40.0",
    "max_version": "v0.40.0",
    "expires_at": "2026-08-09T12:00:00Z"
  }
]
```

Rules:

- `id` is permanent, lowercase kebab-case, and unique. Changing it resurfaces a
  dismissed message.
- `title` is the canonical copy shown in the messages modal, and in the compact
  card that older clients still draw.
- `banner` must mirror `title` while older clients still read that legacy field.
- `headline` is optional, up to 60 characters: the bold line that opens the
  message in v0.40.0 and later. Earlier versions ignore it.
- `accent` is optional: up to 8 phrases of at most 40 characters. Every
  occurrence in `body` is drawn in the accent color in v0.40.0 and later.
  Matching is case-sensitive and also finds a phrase inside a longer word, so
  `tab` also colors "table".
- `body` explains impact and action in short plain-text lines, up to 16 lines
  of 160 characters. Versions before v0.40.0 show the first 8 lines and cut
  each at 120, so an entry they can see stays within that, and one that needs
  more sets `min_version` to `v0.40.0`.
- `url` is optional and must be HTTPS.
- `min_version` and `max_version` are inclusive and optional. Time-sensitive or
  version-specific messages should have narrow version bounds.
- `expires_at` is an optional RFC 3339 timestamp. Expired or invalid timestamps
  are rejected by the client; every time-sensitive message must set one.

Pressing `r` in the messages modal bypasses both local cache ages and refreshes
the release catalog and editorial feed from GitHub. Normal background checks do
no network or parsing work on the render path. The release catalog and the
editorial feed both check every 10 minutes with an HTTP ETag, independent of
the installed version. Each is cached in a file of its own format,
`release-catalog.json` and `feed-messages.json`, stamped with the parser that
wrote it. A build that finds another parser's stamp downloads the whole list
once. `update-check.json` and `message-feed.json` belong to versions before
v0.40.0 and are read only to fill the panel on the first start after an
upgrade.
