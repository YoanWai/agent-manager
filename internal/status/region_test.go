package status

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/YoanWai/agent-manager/internal/config"
)

func TestGrokActivityRegionBoxedAndMinimal(t *testing.T) {
	engine := defaultEngine(t)
	boxed := "     ❯ count from 1 to 5\n  ╭────╮\n  │ ❯                        │\n"
	region, ok := engine.ActivityRegion("grok", boxed)
	if !ok || !strings.Contains(region, "count from 1 to 5") {
		t.Fatalf("boxed region = %q ok=%v", region, ok)
	}
	minimal := "◆ session_start\nminimal · /help\n❯\nGrok 4.6 (medium)\n"
	region, ok = engine.ActivityRegion("grok", minimal)
	if !ok || !strings.Contains(region, "session_start") {
		t.Fatalf("minimal region = %q ok=%v", region, ok)
	}
	if _, ok := engine.ActivityRegion("grok", "     ❯ count from 1 to 5\n     done\n"); ok {
		t.Fatal("an indented grok user turn was treated as the composer")
	}
}

// LastMessage quotes the agent's last message from its beginning, not its
// frame or its tail: the message_start marker finds where the reply began,
// its lines flatten into one, and the input box, shortcut hints, spinner
// rows and turn summaries are all stepped over. A pane that is nothing but
// frame yields an empty quote, and a tool without box rules reports it
// cannot tell at all.
func TestLastMessage(t *testing.T) {
	engine := defaultEngine(t)
	pane := "● Ran the suite.\n" +
		"\n" +
		"● Done. The fix is in auth.go.\n" +
		"  Two tests were touched.\n" +
		"\n" +
		"✻ Cerebrating… (4s · esc to interrupt)\n" +
		"\n" +
		"❯ \n" +
		"  ? for shortcuts"
	line, anchored, ok := engine.LastMessage("claude", pane)
	if !ok || !anchored {
		t.Fatal("claude has an activity cutoff, ok should be true")
	}
	if line != "Done. The fix is in auth.go. Two tests were touched." {
		t.Fatalf("LastMessage = %q, want the last message from its start", line)
	}

	// Current Claude Code bullets replies with ⏺, and prints notices (a
	// plugin banner) after the turn summary; the quote starts at the
	// bullet and stops at the summary, from a real v2.1.240 pane shape.
	realPane := "❯ Reply with exactly this sentence and nothing else: The quick banana ate seventeen kayaks today.\n" +
		"\n" +
		"⏺ The quick banana ate seventeen kayaks today.\n" +
		"\n" +
		"✻ Crunched for 3s\n" +
		"──────────────────────────────\n" +
		"Plugins updated: 7 plugins · Run /reload-plugins to apply\n" +
		"❯ \n" +
		"──────────────────────────────"
	line, anchored, ok = engine.LastMessage("claude", realPane)
	if !ok || !anchored || line != "The quick banana ate seventeen kayaks today." {
		t.Fatalf("real pane quote = %q ok=%v, want the reply alone", line, ok)
	}

	if line, _, ok = engine.LastMessage("claude", "✻ Musing… (2s · esc to interrupt)\n\n❯ "); !ok || line != "" {
		t.Fatalf("frame-only pane: line=%q ok=%v, want empty and true", line, ok)
	}

	// opencode has no message_start, so its newest content line is the quote.
	line, anchored, ok = engine.LastMessage("opencode",
		"     hey. what need?\n     ▣  Build · GLM-5.2 · 22.0s\n  ┃\n  ╹▀▀▀▀")
	if !ok || anchored || line != "hey. what need?" {
		t.Fatalf("opencode fallback quote = %q ok=%v", line, ok)
	}

	if _, _, ok = engine.LastMessage("no-such-tool", pane); ok {
		t.Fatal("unknown tool should report it cannot tell")
	}
	if _, _, ok = engine.LastMessage("claude", "just text, no input box"); ok {
		t.Fatal("pane without the cutoff should report it cannot tell")
	}
}

// Claude blinks the bullet of a step that is still running: its off frame
// paints the bullet cell blank, which reads as a previous turn's message
// being the newest one. Rows as captured with capture-pane -e from Claude
// Code v2.1.282 on 2026-09-25.
func TestPlainRestoresClaudesBlinkedBullet(t *testing.T) {
	engine := defaultEngine(t)
	pane := func(bullet string) string {
		return "\x1b[38;5;231m\x1b[49m⏺\x1b[39m Tea, good choice.\n" +
			"\n" +
			"\x1b[38;5;246m✻\x1b[39m \x1b[38;5;246mWorked for 3s · done 1:41 AM\x1b[39m\n" +
			"\n" +
			"\x1b[38;5;239m\x1b[48;5;237m❯ \x1b[38;5;231mUse the Bash tool to run python3 -c \"import time; time.sleep(20)\" in the foreground, then reply with one short sentence.\x1b[39m\n" +
			"\n" +
			"\x1b[38;5;246m\x1b[49m" + bullet + "\x1b[39m Sleeping 20 seconds via python\n" +
			"\x1b[38;5;246m  ⎿  $ python3 -c \"import time; time.sleep(20)\"\x1b[39m\n" +
			"\n" +
			"\x1b[38;5;174m✶\x1b[39m \x1b[38;5;216mFrosting…\x1b[38;5;174m \x1b[38;5;246m(2s · ↓\x1b[39m \x1b[38;5;246m23 tokens)\x1b[39m\n" +
			"\x1b[38;5;244m────────────\n" +
			"\x1b[38;5;246m❯\u00a0\x1b[39m"
	}
	lit, blinked := engine.Plain("claude", pane("⏺")), engine.Plain("claude", pane(" "))
	if blinked != lit {
		t.Fatalf("blinked frame reads\n%s\nwant the lit frame\n%s", blinked, lit)
	}
	quote, anchored, _ := engine.LastMessage("claude", blinked)
	if want := `Sleeping 20 seconds via python ⎿  $ python3 -c "import time; time.sleep(20)"`; !anchored || quote != want {
		t.Fatalf("quote = %q anchored=%v, want %q", quote, anchored, want)
	}
	if text, _, _ := engine.FullTurnText("claude", blinked); text != "⏺ Sleeping 20 seconds via python" {
		t.Fatalf("copied text = %q, want the running step", text)
	}
	if got, want := engine.Plain("codex", pane(" ")), ansi.Strip(pane(" ")); got != want {
		t.Fatalf("codex declares no blinking marker, Plain = %q want %q", got, want)
	}
}

// An open question dialog draws its question where the reply would be,
// with no message of its own, so the newest message above it belongs to an
// earlier turn. Frame from a live Claude Code v2.1.281 session.
func TestLastMessageQuotesAnOpenDialogsQuestion(t *testing.T) {
	engine := defaultEngine(t)
	above := "⏺ Tea, good choice.\n" +
		"\n" +
		"✻ Brewed for 1s · done 12:59 AM\n" +
		"\n" +
		"❯ Use the AskUserQuestion tool right away to ask me whether I prefer cats or dogs. Nothing else.\n" +
		"  ⎿  8 skills available\n" +
		"────────────\n" +
		" ☐ Pet pref\n" +
		"\n" +
		"Do you prefer cats or dogs?\n" +
		"\n"
	below := "  3. Type something.\n" +
		"────────────\n" +
		"  4. Chat about this\n" +
		"\n" +
		"Enter to select · ↑/↓ to navigate · Esc to cancel"
	for name, pane := range map[string]string{
		"first option selected":  above + "❯ 1. Cats\n     You prefer cats\n  2. Dogs\n     You prefer dogs\n" + below,
		"second option selected": above + "  1. Cats\n     You prefer cats\n❯ 2. Dogs\n     You prefer dogs\n" + below,
		"option under the rule":  above + "  1. Cats\n     You prefer cats\n  2. Dogs\n     You prefer dogs\n  3. Type something.\n────────────\n❯ 4. Chat about this\n\nEnter to select · ↑/↓ to navigate · Esc to cancel",
	} {
		quote, anchored, _ := engine.LastMessage("claude", pane)
		if !anchored || quote != "Do you prefer cats or dogs?" {
			t.Fatalf("%s: quote = %q anchored=%v, want the dialog's question", name, quote, anchored)
		}
	}
}

func TestGrokLastMessageSkipsChrome(t *testing.T) {
	engine := defaultEngine(t)

	fullscreen := "    created pr?                                          2:14 AM\n" +
		"    ◆ user_prompt_submit  [hooks: 1]\n" +
		"    No PR yet. I'll commit the worktree branch and open one.  2:14 AM\n" +
		"    COPIED (53 bytes, 1 line)\n" +
		"    Worked for 1m27s\n" +
		" Help improve Grok                               [Opt out] [Opt in]\n" +
		" Off by default. Opt-in to allow SpaceXAI to retain coding data, e.g., prompts,\n" +
		" Read Terms and Privacy Policy.\n" +
		" ╭────────────────────────────╮\n" +
		" │ ❯                        │\n" +
		" ╰──────────── Grok 4.6 (high) ─╯\n"
	line, anchored, ok := engine.LastMessage("grok", fullscreen)
	if !ok || anchored {
		t.Fatalf("fullscreen grok: anchored=%v ok=%v, want unanchored ok", anchored, ok)
	}
	if line != "COPIED (53 bytes, 1 line)" {
		t.Fatalf("fullscreen grok quote = %q, want the last reply line", line)
	}

	minimal := "◆ session_start\n" +
		"      ✓ global/settings:session_start[0].hooks[0] (70ms)\n" +
		"reply with the single word pong and nothing else\n" +
		"◆ user_prompt_submit\n" +
		"      ✓ global/computer-use:user_prompt_submit[0].hooks[0] (83ms)\n" +
		"pong\n" +
		"Worked for 3.7s\n" +
		"minimal · /help\n" +
		"❯\n" +
		"Grok 4.6 (xhigh) · always-approve · 33K / 500K (7%) · ctrl+o transcript\n"
	line, anchored, ok = engine.LastMessage("grok", minimal)
	if !ok || anchored {
		t.Fatalf("minimal grok: anchored=%v ok=%v, want unanchored ok", anchored, ok)
	}
	if line != "pong" {
		t.Fatalf("minimal grok quote = %q, want the reply", line)
	}

	if echoed, ok := engine.LastUserEcho("grok", fullscreen); ok {
		t.Fatalf("grok has no user_echo, LastUserEcho ok=%v echo=%q", ok, echoed)
	}
}

// A degenerate cutoff like ^ matches every row at zero width. InputPrefix
// refuses it for tools that did not declare a prefix, and the row-matcher
// behind MatchesActivityCutoff refuses it just the same, so neither door
// can stamp arbitrary rows as composer rows.
func TestDegenerateCutoffStampsNothing(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"degenerate": {Command: "x", ActivityCutoff: "^"},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, ok := engine.InputPrefix("degenerate", "any row at all"); ok {
		t.Fatal("a zero-width cutoff read as an input prefix")
	}
	if engine.MatchesActivityCutoff("degenerate", "any row at all") {
		t.Fatal("a zero-width cutoff read as a composer boundary")
	}
}

// FullTurnText keeps every marker-led paragraph of the turn, where
// LastMessage anchors to only the newest one.
func TestFullTurnText(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ Reply with exactly this sentence and nothing else: some prompt text\n" +
		"⏺ First paragraph about the topic.\n" +
		"  continued content of first paragraph.\n" +
		"\n" +
		"\n" +
		"⏺ Second paragraph, a different topic.\n" +
		"\n" +
		"⏺ Third and final paragraph.\n" +
		"\n" +
		"✻ Crunched for 3s\n" +
		"────────────────────\n" +
		"❯ "
	want := "⏺ First paragraph about the topic.\n" +
		"  continued content of first paragraph.\n" +
		"\n" +
		"⏺ Second paragraph, a different topic.\n" +
		"\n" +
		"⏺ Third and final paragraph."
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("claude has an activity cutoff, ok should be true")
	}
	if text != want {
		t.Fatalf("FullTurnText =\n%q\nwant\n%q", text, want)
	}
	if line, _, _ := engine.LastMessage("claude", pane); line != "Third and final paragraph." {
		t.Fatalf("LastMessage = %q, want only the newest paragraph", line)
	}

	if text, _, _ := engine.FullTurnText("claude", "❯ the only question\n❯ "); text != "" {
		t.Fatalf("a prompt with no answer yet = %q, want empty", text)
	}
	if _, _, ok := engine.FullTurnText("no-such-tool", pane); ok {
		t.Fatal("unknown tool should report it cannot tell")
	}
	if _, _, ok := engine.FullTurnText("claude", "just text, no input box"); ok {
		t.Fatal("pane without the cutoff should report it cannot tell")
	}
}

// A pane holding two exchanges yields only the newest one's prose.
func TestFullTurnTextStopsAtPreviousTurn(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ first prompt\n" +
		"⏺ first answer\n" +
		"✻ Crunched for 1s\n" +
		"❯ second prompt\n" +
		"⏺ second answer\n" +
		"✻ Crunched for 2s\n" +
		"❯ "
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("ok should be true")
	}
	if text != "⏺ second answer" {
		t.Fatalf("FullTurnText = %q, want only the newest turn", text)
	}
}

// A prompt long enough to wrap echoes over several rows, but user_echo
// only matches the first: the reply's own marker is what opens the turn.
// Tool result rows are not prose, and a notice printed under the turn-end
// summary is past the reply entirely.
func TestFullTurnTextDropsPromptTailAndToolRows(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ a prompt long enough that the composer wrapped it onto\n" +
		"  a second row and then a third row as well\n" +
		"⏺ Read(internal/status/status.go)\n" +
		"  ⎿  Read 120 lines\n" +
		"⏺ The answer itself.\n" +
		"✻ Crunched for 3s\n" +
		"✔ Update installed · Restart to update\n" +
		"❯ "
	want := "⏺ Read(internal/status/status.go)\n" +
		"⏺ The answer itself."
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("ok should be true")
	}
	if text != want {
		t.Fatalf("FullTurnText =\n%q\nwant\n%q", text, want)
	}
}

// Claude prints a "new task?" nudge above the composer once context use
// runs high. It is chrome, and belongs to no turn.
func TestFullTurnTextDropsComposerHint(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ a prompt\n" +
		"⏺ The answer.\n" +
		"                          new task? /clear to save 421.3k tokens\n" +
		"❯ "
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("ok should be true")
	}
	if text != "⏺ The answer." {
		t.Fatalf("FullTurnText = %q, want the nudge dropped", text)
	}
}

// Not every reply opens on a message_start marker: a turn can render as
// plain unmarked prose, and dropping it as prompt tail would copy nothing.
func TestFullTurnTextKeepsUnmarkedReply(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ a prompt\n" +
		"Regression test. Some unmarked prose.\n" +
		"  its own wrapped continuation row.\n" +
		"\n" +
		"Docs. A second unmarked paragraph.\n" +
		"✻ Baked for 1m 33s\n" +
		"❯ "
	want := "Regression test. Some unmarked prose.\n" +
		"  its own wrapped continuation row.\n" +
		"\n" +
		"Docs. A second unmarked paragraph."
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("ok should be true")
	}
	if text != want {
		t.Fatalf("FullTurnText =\n%q\nwant\n%q", text, want)
	}
}

// A reply taller than the capture has no prompt above it, so the region
// opens mid-reply and every row of it is content.
func TestFullTurnTextKeepsReplyThatOutrunsTheCapture(t *testing.T) {
	engine := defaultEngine(t)
	pane := "  a wrapped row of the reply, its marker scrolled away.\n" +
		"⏺ A later paragraph of the same reply.\n" +
		"✻ Crunched for 9s\n" +
		"❯ "
	want := "  a wrapped row of the reply, its marker scrolled away.\n" +
		"⏺ A later paragraph of the same reply."
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("ok should be true")
	}
	if text != want {
		t.Fatalf("FullTurnText =\n%q\nwant\n%q", text, want)
	}
}

// A table's rows open on the same box-drawing characters a tool result is
// drawn under, and they are content: only claude's own ⎿ marks a result.
func TestFullTurnTextKeepsTableRows(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ a prompt\n" +
		"⏺ Here is the table.\n" +
		"  ┌───────┬───────┐\n" +
		"  │ Raw   │ Under │\n" +
		"  ├───────┼───────┤\n" +
		"  │ 0.50  │ 23.00 │\n" +
		"  └───────┴───────┘\n" +
		"  ⎿  Read 120 lines\n" +
		"❯ "
	want := "⏺ Here is the table.\n" +
		"  ┌───────┬───────┐\n" +
		"  │ Raw   │ Under │\n" +
		"  ├───────┼───────┤\n" +
		"  │ 0.50  │ 23.00 │\n" +
		"  └───────┴───────┘"
	text, _, ok := engine.FullTurnText("claude", pane)
	if !ok {
		t.Fatal("ok should be true")
	}
	if text != want {
		t.Fatalf("FullTurnText =\n%q\nwant\n%q", text, want)
	}
}

// The turn bound reads a prompt the way LastUserEcho does, so the rows a
// tool draws behind its own composer marker cannot pass for one: opencode
// spells its model footer in the ┃ gutter its echoes use, and codex draws
// an open dialog's options behind ›. Row shapes as verified live in
// TestLastUserEchoPerTool. Tools that echo nothing (grok, hermes) take the
// composer row itself as the bound, and a reply body that renders indented
// (opencode) is content, not a wrapped prompt's tail.
func TestFullTurnTextPerTool(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name, tool, pane, want string
	}{
		{
			name: "opencode keeps an indented reply under its gutter footer",
			tool: "opencode",
			pane: "  ┃\n" +
				"  ┃  Reply with exactly: OPENCODE ECHO TEST DONE.\n" +
				"  ┃\n" +
				"     OPENCODE ECHO TEST DONE.\n" +
				"     ▣  Build · Gemini 3.6 Flash · 2.6s\n" +
				"  ┃\n" +
				"  ┃  Build · Gemini 3.6 Flash Google\n" +
				"  ╹▀▀▀▀▀▀▀▀▀▀▀▀",
			want: "     OPENCODE ECHO TEST DONE.",
		},
		{
			name: "codex keeps the reply while a permission dialog is open",
			tool: "codex",
			pane: "› Reply with exactly: CODEX ECHO TEST DONE.\n" +
				"• CODEX ECHO TEST DONE.\n" +
				"─── Worked for 2s ───\n" +
				"  Allow codex to run this command?\n" +
				"› 1. Yes, allow\n" +
				"  2. No\n" +
				"› Ask Codex to do anything",
			want: "• CODEX ECHO TEST DONE.",
		},
		{
			name: "gemini opens on its indented marker",
			tool: "gemini",
			pane: " > Reply with exactly: GEMINI ECHO TEST DONE.\n" +
				"▀▀▀▀▀▀▀▀▀▀▀▀\n" +
				"✦ GEMINI ECHO TEST DONE.\n" +
				"  a second row of the same reply.\n" +
				"                  ? for shortcuts\n" +
				" >   Type your message or @path/to/file",
			want: "✦ GEMINI ECHO TEST DONE.\n  a second row of the same reply.",
		},
		{
			name: "gemini drops its approval banner beside a skills count",
			tool: "gemini",
			pane: " > Reply with exactly: GEMINI ECHO TEST DONE.\n" +
				"✦ GEMINI ECHO TEST DONE.\n" +
				"                  ? for shortcuts\n" +
				" Shift+Tab to accept edits                          2 skills\n" +
				" >   Type your message or @path/to/file",
			want: "✦ GEMINI ECHO TEST DONE.",
		},
		{
			name: "command-code keeps both rows of the reply",
			tool: "command-code",
			pane: "❯ Reply with exactly: CMD ECHO TEST DONE.\n" +
				"⠶ CMD ECHO TEST DONE.\n" +
				"  And a second line of the reply.\n" +
				"❯ Ask your question...",
			want: "⠶ CMD ECHO TEST DONE.\n  And a second line of the reply.",
		},
		{
			name: "grok bounds on its composer row, not its turn summary",
			tool: "grok",
			pane: "│ ❯ first prompt\n" +
				"Grok answer paragraph one.\n" +
				"Grok answer paragraph two.\n" +
				"  Worked for 3s. Press ctrl+c to stop\n" +
				"│ ❯ ",
			want: "Grok answer paragraph one.\nGrok answer paragraph two.",
		},
		{
			name: "grok keeps no prompt, so its previous turn summary bounds",
			tool: "grok",
			pane: "Grok answer to the first question.\n" +
				"  Worked for 3s. Press ctrl+c to stop\n" +
				"Grok answer to the second question.\n" +
				"  Worked for 5s. Press ctrl+c to stop\n" +
				"│ ❯ ",
			want: "Grok answer to the second question.",
		},
		{
			name: "grok mid-turn bounds on the summary it already drew",
			tool: "grok",
			pane: "Grok answer to the first question.\n" +
				"  Worked for 3s. Press ctrl+c to stop\n" +
				"Grok is answering the second question.\n" +
				"│ ❯ ",
			want: "Grok is answering the second question.",
		},
		{
			name: "claude falls back to the previous summary with no prompt in frame",
			tool: "claude",
			pane: "⏺ The answer to a question that scrolled away.\n" +
				"✻ Crunched for 1s\n" +
				"⏺ The answer we want.\n" +
				"✻ Crunched for 2s\n" +
				"❯ ",
			want: "⏺ The answer we want.",
		},
		{
			name: "claude drops a tool result with the rows it wrapped onto",
			tool: "claude",
			pane: "❯ a prompt\n" +
				"⏺ Read(file.go)\n" +
				"  ⎿  Read 120 lines\n" +
				"     line two of the result\n" +
				"     line three of the result\n" +
				"⏺ The answer.\n" +
				"✻ Crunched for 1s\n" +
				"❯ ",
			want: "⏺ Read(file.go)\n⏺ The answer.",
		},
		{
			name: "codex drops a command's output under its own glyph",
			tool: "codex",
			pane: "› a prompt\n" +
				"• Ran command\n" +
				"  └ ok\n" +
				"    more output\n" +
				"• The answer.\n" +
				"─── Worked for 2s ───\n" +
				"› ",
			want: "• Ran command\n• The answer.",
		},
		{
			name: "a result row under the last summary is not content",
			tool: "claude",
			pane: "⏺ answer one\n" +
				"✻ Crunched for 1s\n" +
				"⏺ the answer we want\n" +
				"✻ Crunched for 2s\n" +
				"  ⎿  a result row drawn under the summary\n" +
				"❯ ",
			want: "⏺ the answer we want",
		},
		{
			name: "an unmarked indented reply beats a notice below the summary",
			tool: "claude",
			pane: "❯ a prompt\n" +
				"  an indented unmarked reply row\n" +
				"✻ Crunched for 1s\n" +
				"A left-aligned notice printed after the turn\n" +
				"❯ ",
			want: "  an indented unmarked reply row",
		},
		{
			name: "a follow-up sent mid-read falls back to the answer above",
			tool: "claude",
			pane: "❯ the real question\n" +
				"⏺ The answer the user is reading.\n" +
				"  a second row of it.\n" +
				"✻ Crunched for 12s\n" +
				"❯ a follow-up typed while it was working\n" +
				"❯ ",
			want: "⏺ The answer the user is reading.\n  a second row of it.",
		},
		{
			name: "hermes stops at the newest prompt it drew",
			tool: "hermes",
			pane: "────────────────\n" +
				"● first prompt\n" +
				"────────────────\n" +
				"╭─ ⚕ Hermes ─────╮\n" +
				"first answer\n" +
				"╰────────────────╯\n" +
				"────────────────\n" +
				"● second prompt\n" +
				"Initializing agent...\n" +
				"────────────────\n" +
				"┌─ Reasoning ────┐\n" +
				"thinking about it\n" +
				"└────────────────┘\n" +
				"╭─ ⚕ Hermes ─────╮\n" +
				"second answer\n" +
				"╰────────────────╯\n" +
				" ⚕ grok-4.6 │ 22.2K/500K │ 23s\n" +
				"────────────────\n" +
				"❯ ",
			want: "thinking about it\nsecond answer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, _, ok := engine.FullTurnText(tc.tool, tc.pane)
			if !ok {
				t.Fatalf("%s: ok should be true", tc.tool)
			}
			if text != tc.want {
				t.Fatalf("FullTurnText(%s) =\n%q\nwant\n%q", tc.tool, text, tc.want)
			}
		})
	}
}

// Where nothing in the pane says the turn began - grok keeps no prompt in
// its transcript, and its summary can sit above the capture - the text is
// the whole region, and the caller is told so. pi draws no readable region
// at all, so no reply can be read from it.
func TestFullTurnTextReportsAnUnboundedCopy(t *testing.T) {
	engine := defaultEngine(t)
	bounded := "│ ❯ a prompt\nGrok answered it.\n│ ❯ "
	if text, isBounded, ok := engine.FullTurnText("grok", bounded); !ok || !isBounded || text != "Grok answered it." {
		t.Fatalf("bounded grok turn = %q bounded=%v ok=%v", text, isBounded, ok)
	}
	unbounded := "Grok answered something older.\nGrok answered this too.\n│ ❯ "
	text, isBounded, ok := engine.FullTurnText("grok", unbounded)
	if !ok {
		t.Fatal("a grok pane with a composer has a region")
	}
	if isBounded {
		t.Fatal("no prompt and no summary in frame is not a bounded turn")
	}
	if text != "Grok answered something older.\nGrok answered this too." {
		t.Fatalf("unbounded grok copy = %q, want the whole region", text)
	}

	// pi opens its region at the pane origin, so the copy falls back to
	// the pane above the composer rather than reporting nothing.
	pi := "  a pi reply row\n────────────\n\n────────────\n/tmp (main)\n"
	text, isBounded, ok = engine.FullTurnText("pi", pi)
	if !ok {
		t.Fatal("pi should copy the pane its region leaves empty")
	}
	if isBounded {
		t.Fatal("a pane read without a turn boundary is not bounded")
	}
	if text != "  a pi reply row" {
		t.Fatalf("pi copy = %q, want the reply row above the composer", text)
	}
}

func TestFullTurnTextPendingWrappedPrompt(t *testing.T) {
	engine := defaultEngine(t)
	for _, tc := range []struct {
		tool, prompt, answer, end string
	}{
		{"claude", "❯ ", "⏺ Previous answer.", "✻ Crunched for 1s"},
		{"codex", "› ", "• Previous answer.", "─── Worked for 2s ───"},
		{"gemini", " > ", "✦ Previous answer.", ""},
		{"command-code", "❯ ", "⠶ Previous answer.", ""},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			pending := tc.prompt + "new question that wraps\n  continuation of the new question\n" + tc.prompt
			for _, answered := range []bool{false, true} {
				pane, want := pending, ""
				if answered {
					pane = tc.prompt + "original question\n" + tc.answer + "\n" + tc.end + "\n" + pending
					want = tc.answer
				}
				if got, _, ok := engine.FullTurnText(tc.tool, pane); !ok || got != want {
					t.Fatalf("answered=%v: copied %q, ok=%v; want %q", answered, got, ok, want)
				}
			}
		})
	}
}

func TestFullTurnTextResultWithBlankLine(t *testing.T) {
	engine := defaultEngine(t)
	for _, tc := range []struct {
		tool, prompt, call, result, answer string
	}{
		{"claude", "❯ ", "⏺ Read(file.go)", "  ⎿ first output line", "⏺ Answer."},
		{"codex", "› ", "• Ran command", "  └ first output line", "• Answer."},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			pane := tc.prompt + "question\n" + tc.call + "\n" + tc.result + "\n\n    second output line\n" + tc.answer + "\n  reply continuation\n" + tc.prompt
			want := tc.call + "\n\n" + tc.answer + "\n  reply continuation"
			if got, _, ok := engine.FullTurnText(tc.tool, pane); !ok || got != want {
				t.Fatalf("copied %q, ok=%v; want %q", got, ok, want)
			}
		})
	}
}

func TestMusePromptAndReply(t *testing.T) {
	engine := defaultEngine(t)
	for _, divider := range []string{"────────────────", "── Voice input (⌥ + v to start) ────────────"} {
		pane := "❯ earlier prompt\n◆ earlier reply\n❯ hello muse\n\n◆ echo: hello muse\n\n" + divider + "\n❯\n────────────────\n  echo · /work · Auto-review\n"
		if _, ok := engine.ActivityRegion("muse", pane); !ok {
			t.Fatal("composer did not bound the activity region")
		}
		if got := engine.TypingHold("muse", pane); got != "" {
			t.Fatalf("resting prompt held as %q", got)
		}
		if got, ok := engine.LastUserEcho("muse", pane); !ok || got != "hello muse" {
			t.Fatalf("LastUserEcho = %q, %v", got, ok)
		}
		if got, anchored, ok := engine.LastMessage("muse", pane); !ok || !anchored || got != "echo: hello muse" {
			t.Fatalf("LastMessage = %q, %v, %v", got, anchored, ok)
		}
		if got, bounded, ok := engine.FullTurnText("muse", pane); !ok || !bounded || got != "◆ echo: hello muse" {
			t.Fatalf("FullTurnText = %q, %v, %v", got, bounded, ok)
		}
	}
	picker := "  Resume a previous session\n❯ just now    blush-polaris · hello\n  1 / 3 · 34%  enter resume  esc exit"
	if got := engine.TypingHold("muse", picker); got != Waiting {
		t.Fatalf("picker TypingHold = %q", got)
	}
}
