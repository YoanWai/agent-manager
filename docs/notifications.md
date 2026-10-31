# Release summaries and messages

The messages panel has two deliberately separate sources. Release changes come
from GitHub Releases. Maintainer messages come from `docs/messages.json`.

## The messages panel

The panel is an inbox. Messages sit in a list and the selected one opens beside
it.

The list groups messages under RELEASES, NEWS, HEADS UP and TIPS. A group with
nothing in it is left out. Each message takes two lines. The first holds a glyph
for its kind and the title. The second is dim and holds the release headline or
the feed `banner`. A message that has not been read yet is bold and ends in a
`●`.

A terminal 124 columns wide or more splits the panel, as long as it is tall
enough to show the whole list. The list sits on the left and the message on the
right. A release summary wraps at 76 columns and the rest of a message wraps at
the width of its pane. A narrower terminal, or a wide but short one, puts the
list above the message, one line per row, with the same tabs.
Compatible glyph mode draws an ASCII stand-in for each kind, unread, highlight
and call-to-action glyph. The `↑↓` key hint and the `·` separator stay as
they are, and the rules and the scrollbar follow the app's compatible chrome.

### A release

"Updated to X" and "X available" are release messages. Above its tabs a release
shows:

- The version, then its publish date (or `out now` for an available release)
  and the version you were on. Dates read in your local time zone.
- The headline.
- For an available release, a bar that reads `u  Update to X and restart`,
  with the notes link at its right.

Three tabs follow, with a count on Changes and Thanks.

- **Overview** holds the summary and the highlights under WHAT'S NEW (WHAT YOU
  GET for an available release). When every highlight fits on one row, the
  labels form a column with each detail beside it. When a detail would wrap,
  each highlight is a block: its label, its detail under it, and a blank row
  before the next. Counts of features, fixes, under-the-hood changes and people
  close the tab.
- **Changes** lists every change in the release as three blocks, features,
  fixes and under the hood, each in the order the notes give them. Each row has
  a glyph for its kind, the scope, the text, and the handle of an outside
  contributor at the right. Under-the-hood rows are dim. `f` steps the filter
  through All, features, fixes and under the hood.
- **Thanks** names each person once, under BUILT IT, FOUND IT or STARTED
  SOMETHING, with what they did and their pull request and issue numbers.

When an install skips releases, the header shows how many releases the message
covers. Overview shows the newest release in full and each older one as a
heading with its highlights and its counts. Changes lists every release under its version. Thanks
merges the range into one line per person, filed under the group of their first
appearance.

### A feed message

A feed message shows a label chip for its kind (NEWS, HEADS UP or TIP), its
`banner`, `until <date>` at the right when it expires, the title and the body.
When the entry has a `url`, a bar below the body in the kind's color reads `↵`
and the entry's `action` text, with the link's host at its right.

The config import warning that appears when `config.toml` could not be carried
into Settings is a HEADS UP message.

### Keys

| Key | Action |
|---|---|
| `↑` `↓` or `k` `j` | Previous or next message |
| `→` `←`, `l` `h` or `tab` `shift+tab` | Next or previous tab, on a release |
| `f` | Step the filter, on the Changes tab |
| `pgup` `pgdn` `home` `end` | Scroll the message |
| `↵` | Open the release notes or the feed link |
| `u` | Update, on an available release |
| `x` | Dismiss the message for good |
| `r` | Refresh the release catalog and the feed |
| `esc` or `q` | Close |

The bottom edge lists the keys that apply to the selected message.

### Unread

A message is unread until it has been the selected message while the panel is
open. The set of read messages is stored beside the dismissed set. The top
edge of the panel reads `N unread` while any message is unread. The footer
chip reads `unread` while any message is unread and `messages` once all have
been read. It disappears when no message is left. Dismissing a message removes
it from the list.

## Shipping a feature or fix

Write a clear Conventional Commit title for the pull request:

```text
feat(worktree): renaming a session moves its worktree and branch
fix(groups): make group creation immediate and reliable
```

GitHub adds merged pull request titles to the release's generated **What's
Changed** section. Every merged pull request stays in that list.

The messages panel reads four authored parts of the same notes, all optional:

```markdown
## v0.46.0
**Model + reasoning pickers are here!**

Two or three sentences of summary. `Backticks` put a word in the accent color.

## Highlights
- `Model picker` choose the model and reasoning level when you start a session.

## Thank you
### Built it
- @handle added X (#123)
### Found it
- @handle reported Y (#124, #125)
### Started something
- @handle opened a draft for Z (#126)
```

- **Headline**: a bold first line under the version heading. It shows under the
  version in the release header.
- **Summary**: the paragraph under it. It opens the Overview tab of the newest
  release and wraps at 76 columns.
- **Highlights**: short bullets naming what the release gives someone. From
  v0.46.0 each opens with its label in backticks. The label fills a column and
  the rest of the bullet is its detail. A bullet without a leading label wraps
  under its bullet. Highlights carry no pull request numbers.
- **Thank you**: one bullet per person, `- @handle what they did (#1, #2)`. The
  `###` headings group the bullets. A bullet above the first heading sits under
  Built it. The trailing `(#...)` becomes the numbers column on the Thanks tab.

The Changes tab lists What's Changed by kind, in the order the notes give. A change's
Conventional Commit type sets its glyph and color and what `f` filters. `feat`
is a feature, `fix` and `perf` are fixes, and every other type (`docs`,
`chore`, `test`, `ci`, `build`, `refactor`, `style`, or no type at all) is under
the hood. Outside authors are credited on their row.

Two notices read these notes. "vX available" is drawn by the version a person
is still on, and "Updated to X" by the new one, so notes are written for both
panels. Readers before v0.46.0 show each highlight as one line with no label
column, show the thank-you bullets without their groups, and leave the `docs`,
`chore`, `test`, `ci`, `build`, `refactor` and `style` lines of What's Changed
on the web page. v0.35.0 through v0.39.0 show the first 6 Highlights and 8
thank-you lines, cut each line at 120 visible characters, and show neither the
headline nor the summary. v0.18.0 through v0.34.0 draw the first 12 rows of
What's Changed, of every type, each cut at 120. Put what matters first and keep
lines within 120 while those versions are in use.

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
change, such as a known issue, a time-sensitive migration warning, a tip or a
project announcement. The feed is polled independently of the installed version
and can target a version range.

```json
[
  {
    "id": "known-issue-0460",
    "kind": "heads-up",
    "banner": "Known issue in v0.46.0",
    "title": "Known issue in v0.46.0",
    "accent": ["safe action"],
    "body": [
      "What users will observe.",
      "The safe action to take while a fix is prepared."
    ],
    "url": "https://github.com/YoanWai/agent-manager/issues/234",
    "action": "Read the issue",
    "min_version": "v0.46.0",
    "max_version": "v0.46.0",
    "expires_at": "2026-12-09T12:00:00Z"
  }
]
```

Rules:

- `id` is permanent, lowercase kebab-case, and unique. Changing it resurfaces a
  dismissed message.
- `kind` is optional and is `news`, `heads-up` or `tip`. It sets the glyph, the
  color and the list group (NEWS, HEADS UP, TIPS). An entry without it, or with
  any other value, is news. Builds before v0.46.0 ignore it and draw every entry
  as news.
- `title` is the canonical copy shown in the messages modal, and in the compact
  card that older clients still draw.
- `banner` is required and up to 60 characters. It is the dim line under the
  title in the list and the line beside the kind label over the message.
- `headline` is optional, up to 60 characters. The panel shows the title in
  its place. Only builds from v0.40.0 through v0.45.0 draw it, as the bold line
  that opens the message.
- `accent` is optional: up to 8 phrases of at most 40 characters. Every
  occurrence in `body` is drawn in the accent color in v0.40.0 and later.
  Matching is case-sensitive and also finds a phrase inside a longer word, so
  `tab` also colors "table".
- `body` explains impact and action in short plain-text lines, up to 16 lines
  of 160 characters. Versions before v0.40.0 show the first 8 lines and cut
  each at 120, so an entry they can see stays within that, and one that needs
  more sets `min_version` to `v0.40.0`.
- `url` is optional and must be HTTPS.
- `action` is optional, up to 40 characters. It is the text of the button the
  `url` opens, such as `Join on Discord`. Without it the button reads
  `Open link`. New entries put the button text here and leave a "Press Enter to
  ..." line out of `body`. Entries shipped before v0.46.0 keep that line because
  older builds still show it.
- `min_version` and `max_version` are inclusive and optional. Time-sensitive or
  version-specific messages should have narrow version bounds.
- `expires_at` is an optional RFC 3339 timestamp. The message shows
  `until <date>` in the viewer's local time zone. Expired or invalid timestamps
  are rejected by the client. Every time-sensitive message must set one.

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
