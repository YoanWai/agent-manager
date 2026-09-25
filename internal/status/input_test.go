package status

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

// InputDraft reads what the user has typed after the composer marker, and
// refuses the placeholder wording a composer paints on its empty row.
func TestInputDraft(t *testing.T) {
	engine := defaultEngine(t)
	if draft, ok := engine.InputDraft("claude", "● Done.\n\n❯ fix the flaky test"); !ok || draft != "fix the flaky test" {
		t.Fatalf("claude draft = %q ok=%v", draft, ok)
	}
	if _, ok := engine.InputDraft("claude", "● Done.\n\n❯ "); ok {
		t.Fatal("empty composer should carry no draft")
	}
	for _, placeholder := range []string{
		"Press up to edit queued messages",
		"Press up to edit queued messages, Enter to send them immediately",
		"Press up to select a queued message to edit, or Enter to send them now",
		"Press up to select a queued message, then Enter to edit it",
	} {
		if _, ok := engine.InputDraft("claude", "● Done.\n\n❯ "+placeholder); ok {
			t.Fatalf("the queued composer's placeholder %q should not read as a draft", placeholder)
		}
	}
	if _, ok := engine.InputDraft("codex", "› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev"); ok {
		t.Fatal("codex placeholder should not read as a draft")
	}
	if draft, ok := engine.InputDraft("codex", "› rename the flag\n  gpt-5.6-terra medium · /home/dev"); !ok || draft != "rename the flag" {
		t.Fatalf("codex draft = %q ok=%v", draft, ok)
	}
	// A gutter composer sits above its cutoff, so the text after the
	// cutoff match is the box's border fill, not what was typed — even
	// when a typed line is sitting right there in the gutter.
	opencode := "┃\n" +
		"┃ fix the flaky test\n" +
		"╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		" ■⬝⬝⬝⬝⬝⬝  esc interrupt"
	if _, ok := engine.InputDraft("opencode", opencode); ok {
		t.Fatal("a gutter composer's border fill must not read as a draft")
	}
}

// LastUserEcho reads the newest prompt the transcript echoes; a pane whose
// reply scrolled the bullet away reports an unanchored LastMessage, which
// is the caller's cue to capture deeper.
func TestLastUserEchoAndScrolledMarker(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ first prompt\n" +
		"⏺ First reply.\n" +
		"❯ second prompt goes here\n" +
		"⏺ Second reply.\n" +
		"❯ "
	echoed, ok := engine.LastUserEcho("claude", pane)
	if !ok || echoed != "second prompt goes here" {
		t.Fatalf("LastUserEcho = %q ok=%v, want the newest echoed prompt", echoed, ok)
	}

	scrolled := "tail of a reply that scrolled its bullet away.\n\n❯ "
	line, anchored, ok := engine.LastMessage("claude", scrolled)
	if !ok || anchored {
		t.Fatalf("scrolled pane: anchored=%v ok=%v, want unanchored", anchored, ok)
	}
	if line != "tail of a reply that scrolled its bullet away." {
		t.Fatalf("scrolled fallback = %q", line)
	}

	running := "  Running python3 -c \"import time; time.sleep(25); print(1)\"\n\n"
	composer := "\n\n────────────\n❯ "
	type frame struct {
		name, pane, want string
		anchored         bool
	}
	frames := []frame{
		{"queued dialog", "⏺ Red or blue?\n\n❯ Also tell me a fun fact about tmux after that.\n────────────\n ☐ Color\n\nRed or blue?\n\n❯ 1. Red\n     Red\n  2. Blue", "Red or blue?", true},
		{"bullet-less dialog", "  Cat or dog?\n────────────\n ☐ Pet pref\n\nCat or dog?\n\n❯ 1. Cat", "Cat or dog?", false},
		{"queued running turn", "  Running python3 -c 'time.sleep(30)' (ctrl+b ctrl+b (twice) to run in background)\n❯ Run this exact shell command once, then continue.\n  ctrl+x ctrl+s to send now\n✻ Razzmatazz… (1m 25s · ↓ 147 tokens)\n\n❯ Press up to edit queued messages", "Running python3 -c 'time.sleep(30)' (ctrl+b ctrl+b (twice) to run in background)", false},
		{"wrapped queued message", running + "❯ Also, after that finishes, tell me in one plain sentence what a terminal multiplexer is, keeping it short and simple, and please do not use any\n  tools at all for this follow-up question, thanks a lot.\n  ctrl+x ctrl+s to send now\n\n✳ Scurrying… (25s · ↓ 98 tokens)\n\n────────────\n❯ Press up to edit queued messages", "Running python3 -c \"import time; time.sleep(25); print(1)\"", false},
		{"nothing said yet", "\n ▐▛███▛█   Claude Code v2.1.281\n▝▜██████▀  Sonnet 5 with medium effort · Claude Max\n  ▝▝ ▝▝    /home/dev/project · /rc\n\n❯ Use the AskUserQuestion tool right away to ask me whether I prefer red or blue. Nothing else.\n\n❯ Also tell me a fun fact about tmux after that.\n\n✶ Galloping… (running UserPromptSubmit hooks… 0/3 · 1s)\n" + strings.Repeat(" ", 130) + "◐ medium · /effort\n────────────\n❯ Press up to edit queued messages", "", false},
		{"badge over a typed prompt", "\n ▐▛███▛█   Claude Code v2.1.281\n▝▜██████▀  Sonnet 5 with medium effort · Claude Max\n  ▝▝ ▝▝    /home/dev/project · /rc\n\n" + strings.Repeat(" ", 130) + "◐ medium · /effort\n────────────\n❯ Use the AskUserQuestion tool right away to ask me which pet I prefer, cat or dog. Nothing else.", "", false},
	}
	for _, notice := range []string{
		strings.Repeat(" ", 130) + "◐ medium · /effort",
		strings.Repeat(" ", 48) + "tmux focus-events off · add 'set -g focus-events on' to ~/.tmux.conf and reattach for focus tracking",
		strings.Repeat(" ", 72) + "You've used 78% of your weekly limit · resets Sep 26 at 1am (Asia/Jerusalem)",
		"  ⎿  Tip: Using a Slack MCP? With Claude Tag you can @Claude directly in Slack — run /install-slack-app or share claude.com/product/tag with your org\n     owner",
	} {
		frames = append(frames, frame{"notice under the spinner: " + strings.TrimSpace(notice), running + "✽ Scurrying… (17s · ↓ 98 tokens)\n" + notice + composer, "Running python3 -c \"import time; time.sleep(25); print(1)\"", false})
	}
	for _, f := range frames {
		line, anchored, ok := engine.LastMessage("claude", f.pane)
		if !ok || anchored != f.anchored || line != f.want {
			t.Fatalf("%s: line=%q anchored=%v ok=%v, want %q anchored=%v", f.name, line, anchored, ok, f.want, f.anchored)
		}
	}
	if !engine.HasMessageStart("claude") || engine.HasMessageStart("opencode") {
		t.Fatal("HasMessageStart should be true for claude, false for opencode")
	}
	if echoed, ok := engine.LastUserEcho("claude", "⏺ Only replies here.\n❯ "); !ok || echoed != "" {
		t.Fatalf("echoless pane: echo=%q ok=%v, want empty and true", echoed, ok)
	}
}

// Echo shapes verified live on 2026-08-23: codex v0.56 (trust dialog and
// composer share the › marker), gemini v0.53 (> echo, ✦ reply), opencode
// v1.18.21 (┃ gutter echo above the reply, composer block on the cutoff).
func TestLastUserEchoPerTool(t *testing.T) {
	engine := defaultEngine(t)

	codexPane := "> You are in /private/tmp/work\n" +
		"  Do you trust the contents of this directory?\n" +
		"› 1. Yes, continue\n" +
		"  2. No, quit\n" +
		"  Press enter to continue\n" +
		"› Reply with exactly: CODEX ECHO TEST DONE.\n" +
		"• CODEX ECHO TEST DONE.\n" +
		"› Ask Codex to do anything\n" +
		"  gpt-5.6-luna medium · /private/tmp/work"
	if echoed, ok := engine.LastUserEcho("codex", codexPane); !ok || echoed != "Reply with exactly: CODEX ECHO TEST DONE." {
		t.Fatalf("codex echo = %q ok=%v", echoed, ok)
	}
	if line, anchored, ok := engine.LastMessage("codex", codexPane); !ok || !anchored || line != "CODEX ECHO TEST DONE." {
		t.Fatalf("codex reply = %q anchored=%v ok=%v", line, anchored, ok)
	}

	geminiPane := " > Reply with exactly: GEMINI ECHO TEST DONE.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		"✦ GEMINI ECHO TEST DONE.\n" +
		"                  ? for shortcuts\n" +
		"────────────\n" +
		" Shift+Tab to accept edits\n" +
		"▄▄▄▄▄▄▄▄▄▄▄▄\n" +
		" >   Type your message or @path/to/file\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀"
	if echoed, ok := engine.LastUserEcho("gemini", geminiPane); !ok || echoed != "Reply with exactly: GEMINI ECHO TEST DONE." {
		t.Fatalf("gemini echo = %q ok=%v", echoed, ok)
	}
	if line, anchored, ok := engine.LastMessage("gemini", geminiPane); !ok || !anchored || line != "GEMINI ECHO TEST DONE." {
		t.Fatalf("gemini reply = %q anchored=%v ok=%v", line, anchored, ok)
	}

	opencodePane := "  ┃\n" +
		"  ┃  Reply with exactly: OPENCODE ECHO TEST DONE.\n" +
		"  ┃\n" +
		"     OPENCODE ECHO TEST DONE.\n" +
		"     ▣  Build · Gemini 3.6 Flash · 2.6s\n" +
		"  ┃\n" +
		"  ┃\n" +
		"  ┃  Build · Gemini 3.6 Flash Google\n" +
		"  ╹▀▀▀▀▀▀▀▀▀▀▀▀"
	if echoed, ok := engine.LastUserEcho("opencode", opencodePane); !ok || echoed != "Reply with exactly: OPENCODE ECHO TEST DONE." {
		t.Fatalf("opencode echo = %q ok=%v", echoed, ok)
	}
	if line, _, ok := engine.LastMessage("opencode", opencodePane); !ok || line != "OPENCODE ECHO TEST DONE." {
		t.Fatalf("opencode reply = %q ok=%v", line, ok)
	}
}

// Command Code shapes, verified accountless on v1.32.1 with the inject
// stream: replies open on a static ⠶ row, prompts echo on ❯ like claude,
// and the composer paints "Ask your question..." on its empty row.
func TestCommandCodeRowShapes(t *testing.T) {
	engine := defaultEngine(t)
	pane := "# Command Code v1.32.1\n" +
		"❯ Reply with exactly: CMD ECHO TEST DONE.\n" +
		"⠶ CMD ECHO TEST DONE.\n" +
		"  And a second line of the reply.\n" +
		"────────────────────────\n" +
		"❯ Ask your question...\n" +
		"────────────────────────\n" +
		"  ? for shortcuts · taste on"
	if echoed, ok := engine.LastUserEcho("command-code", pane); !ok || echoed != "Reply with exactly: CMD ECHO TEST DONE." {
		t.Fatalf("command-code echo = %q ok=%v", echoed, ok)
	}
	line, anchored, ok := engine.LastMessage("command-code", pane)
	if !ok || !anchored || line != "CMD ECHO TEST DONE. And a second line of the reply." {
		t.Fatalf("command-code reply = %q anchored=%v ok=%v", line, anchored, ok)
	}
	if _, ok := engine.InputDraft("command-code", "⠶ Done.\n❯ Ask your question..."); ok {
		t.Fatal("the composer placeholder should not read as a draft")
	}
}

// An empty composer is the pristine placeholder or a bare marker, and the
// placeholder closes the row, so a draft that merely quotes it mid-text
// stays a draft. Row shapes measured live on command-code v1.33.0: the
// placeholder shows until the first prompt is typed, and a composer cleared
// afterwards paints "❯" with nothing after it for the rest of the session.
func TestComposerIsEmpty(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"command-code": {
			ActivityCutoff:      `(?m)^❯`,
			ComposerPlaceholder: "Ask your question...",
		},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if !engine.ComposerIsEmpty("command-code", "❯ Ask your question...") {
		t.Fatal("the pristine composer's placeholder was not recognised")
	}
	for _, row := range []string{"❯", "❯ ", "❯   "} {
		if !engine.ComposerIsEmpty("command-code", row) {
			t.Fatalf("a cleared composer %q did not read as empty", row)
		}
	}
	if engine.ComposerIsEmpty("command-code", "❯ fix the Ask your question... bug") {
		t.Fatal("a draft quoting the placeholder read as empty")
	}
	if engine.ComposerIsEmpty("command-code", "❯ retry Ask your question...") {
		t.Fatal("a draft ending with the placeholder read as empty")
	}
	// A tool that declares no placeholder never takes the parked-caret
	// path, so a bare marker of its own is not empty for this purpose.
	plain, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"claude": {ActivityCutoff: `(?m)^❯`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if plain.ComposerIsEmpty("claude", "❯ ") {
		t.Fatal("a tool without a declared placeholder took the parked-caret path")
	}
}
