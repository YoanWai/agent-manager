# Paste images in the quick prompt

## Goal

Let a user attach a clipboard image while the quick-prompt bar is open, and have the image reach whatever agent runs in the target session, regardless of provider (claude, codex, grok, opencode, or any future tool).

## Approach

Agent-manager always spawns agents in local tmux on the same host, so any file the TUI writes is readable by the agent. On a `Ctrl+V` keypress in the quick bar, read the OS clipboard for image data, write it to a temp file, and insert that file's absolute path into the prompt text. On submit the existing `SendText` delivers the whole prompt (path included) via `send-keys`; the agent opens the local path.

Nothing is tool-specific: every agent reads a local image path from prompt text. This is the only provider-agnostic mechanism and needs no per-tool config.

## Components

### `internal/clipboard`

- `ReadImage() ([]byte, string, error)` returns raw image bytes and an extension (`png`).
  - macOS: an `osascript` snippet coerces the clipboard to `«class PNGf»` and writes it to a temp file, which is then read back. A clipboard with no image errors.
  - Linux: `wl-paste --type image/png` (Wayland) or `xclip -selection clipboard -t image/png -o` (X11), whichever binary is present. Missing both returns a "install wl-clipboard or xclip" error.
  - Other OS: an "unsupported" error.
  - `goos`, `lookPath`, and `runCmd` are package vars so tests inject fakes without touching a real clipboard.
- `SaveToTemp(data []byte, ext string) (string, error)` writes bytes to `os.TempDir()/agent-manager-pastes/paste-*.<ext>` and returns the absolute path.

### `internal/ui`

- `captureClipboardImage` is a package var defaulting to `clipboard.ReadImage`, overridable in tests.
- `handleQuickKey` intercepts `ctrl+v` before the textarea sees it and calls `attachQuickImage`.
- `attachQuickImage` reads the clipboard image, saves it to a temp file, and inserts the padded absolute path at the cursor. On any error it sets `m.err` to the error text and leaves the input unchanged.

## Error handling

No silent fallbacks. An empty clipboard, a missing Linux clipboard tool, and an unsupported OS each produce a distinct error surfaced through `m.err`.

## Testing

- `clipboard`: `SaveToTemp` writes the bytes with the right extension; the darwin and linux branches invoke the expected command and return injected bytes; the linux branch errors when no tool is present.
- `ui`: `ctrl+v` with a fake image inserts an existing temp-file path into the quick input; `ctrl+v` with no image sets `m.err` and leaves the input empty.

## Scope

One image per paste. macOS and Linux. No drag-drop, no Windows, no inline thumbnail.
