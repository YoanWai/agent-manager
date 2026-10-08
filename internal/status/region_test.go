package status

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

// codex draws a queued follow-up under the running step and a done time under
// a finished reply; neither is part of the reply the row quotes.
func TestLastMessageSkipsCodexQueuedFollowUpAndDoneTime(t *testing.T) {
	engine := defaultEngine(t)
	transcript := "› Run the shell command `sleep 25; echo first-done` and reply with one short sentence.\n" +
		"\n" +
		"• Running sleep 25; echo first-done\n" +
		"\n" +
		"• Working (12s • esc to interrupt)\n" +
		"\n"
	composer := "› Ask Codex to do anything\n" +
		"  gpt-5.1-codex default · /home/dev"
	queued := transcript +
		"• Queued follow-up inputs\n" +
		"  ↳ Also, after that finishes, tell me in one plain sentence what a terminal multiplexer is.\n" +
		"    shift + ← edit last queued message\n" +
		"\n" +
		composer
	want, _, _ := engine.LastMessage("codex", transcript+composer)
	if line, _, ok := engine.LastMessage("codex", queued); !ok || line != want {
		t.Fatalf("queued pane quote = %q ok=%v, want %q as without the queued block", line, ok, want)
	}

	narrow := transcript +
		"• Queued follow-up\n" +
		"  inputs\n" +
		"  ↳ Also, after that\n" +
		"    finishes, tell me\n" +
		"    what tmux is.\n" +
		"    shift + ← edit\n" +
		"    last queued\n" +
		"    message\n" +
		"\n" +
		composer
	if line, _, ok := engine.LastMessage("codex", narrow); !ok || line != want {
		t.Fatalf("narrow queued pane quote = %q ok=%v, want %q as without the queued block", line, ok, want)
	}

	wrapped := transcript +
		"• Queued\n" +
		"  follow-up\n" +
		"  inputs\n" +
		"  ↳ Also, after that\n" +
		"    finishes, tell me\n" +
		"    what tmux is.\n" +
		"    shift + ← edit\n" +
		"    last queued\n" +
		"    message\n" +
		"\n" +
		composer
	if line, _, ok := engine.LastMessage("codex", wrapped); !ok || line != want {
		t.Fatalf("wrapped 22-col queued pane quote = %q ok=%v, want %q as without the queued block", line, ok, want)
	}

	done := "› Tea or coffee?\n" +
		"\n" +
		"• Tea, good choice.\n" +
		"  done 12:59 AM\n" +
		"\n" +
		"› Ask Codex to do anything\n" +
		"  gpt-5.1-codex default · /home/dev"
	if line, _, ok := engine.LastMessage("codex", done); !ok || line != "Tea, good choice." {
		t.Fatalf("done pane quote = %q ok=%v, want the reply alone", line, ok)
	}

	reply := "› Status?\n" +
		"\n" +
		"• Queued\n" +
		"  done 12:59 AM\n" +
		"\n" +
		"› Ask Codex to do anything\n" +
		"  gpt-5.1-codex default · /home/dev"
	if line, _, ok := engine.LastMessage("codex", reply); !ok || line != "Queued" {
		t.Fatalf("genuine reply 'Queued' quote = %q ok=%v, want 'Queued'", line, ok)
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

	// 2026-09-26 real capture, codex 0.157.0: the timestamp label and the
	// tip row above the composer are the tool's frame, not the reply.
	codexHintPane := "› ASK me\n" +
		"• Which one do you want?\n" +
		"  02:42\n" +
		"                          Tip: Start a fresh idea with /new; the previous session stays in history.\n" +
		"› Ask Codex to do anything\n" +
		"  gpt-5.1-codex default · /private/tmp/am586/work"
	if line, anchored, ok := engine.LastMessage("codex", codexHintPane); !ok || !anchored || line != "Which one do you want?" {
		t.Fatalf("codex reply under hint rows = %q anchored=%v ok=%v", line, anchored, ok)
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

// agy draws its composer, then adds the account tier to its header a second
// later; that redraw is frame, while a new transcript row is not.
func TestRegionContentLeavesTheFrameOut(t *testing.T) {
	engine := defaultEngine(t)
	header := func(account string) string {
		return "\n      ▄▀▀▄        Antigravity CLI 1.2.14\n     ▀▀▀▀▀▀       " + account + "\n    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)\n   ▄▀▀    ▀▀▄     /tmp/agy/proj6\n  ▄▀▀      ▀▀▄\n\n" + agyRule + "\n"
	}
	composer := ">\n" + agyRule + "\n" + agyFooter("? for shortcuts") + agyTail
	content := func(pane string) string {
		t.Helper()
		region, ok := engine.ActivityRegion("antigravity", pane)
		if !ok {
			t.Fatalf("no activity region in %q", pane)
		}
		return engine.RegionContent("antigravity", region)
	}
	booting := content(header("dev@example.com") + composer)
	if booted := content(header("dev@example.com (Google AI Plus)") + composer); booted != booting {
		t.Errorf("the header filling in changed the content: %q to %q", booting, booted)
	}
	if replied := content(header("dev@example.com") + "> hi\n\n  Hello!\n\n" + agyRule + "\n" + composer); replied == booting {
		t.Error("a new transcript row left the content unchanged")
	}
}

// gemini draws a message queued during a turn under the reply, with its edit
// hint, until the turn picks it up. Frames captured from gemini v0.61.0.
func TestLastMessageSkipsGeminiQueuedMessage(t *testing.T) {
	engine := defaultEngine(t)
	echo := " > Write a 600-word essay about terminal multiplexers in plain prose paragraphs. No headings, no lists, no tools.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n"
	reply := "✦ Terminal multiplexers let one terminal hold many sessions. A multiplexer keeps programs\n" +
		"  running after the connection drops.\n"
	queued := "  Queued (press ↑ to edit):\n" +
		"    Also, after that finishes, tell me in one plain sentence what a terminal multiplexer is, keeping\n" +
		"    it short.\n"
	footer := "\n" +
		" ⠦ Thinking... (esc to cancel, 14s)                                                       ? for shortcuts\n" +
		"────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
		" Shift+Tab to accept edits                                                       1 MCP server · 1 skill\n" +
		"▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n" +
		" >   Type your message or @path/to/file\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		" workspace (/directory)                          sandbox                                   /model\n" +
		" /tmp/gtest                                      no sandbox                                  Auto"
	want, _, _ := engine.LastMessage("gemini", echo+reply+footer)
	if want != "Terminal multiplexers let one terminal hold many sessions. A multiplexer keeps programs running after the connection drops." {
		t.Fatalf("quote without the queued block = %q", want)
	}
	if line, anchored, ok := engine.LastMessage("gemini", echo+reply+queued+footer); !ok || !anchored || line != want {
		t.Fatalf("queued pane quote = %q anchored=%v ok=%v, want %q as without the queued block", line, anchored, ok, want)
	}
	// before the reply starts, the queued block is all there is under the echo
	if line, _, ok := engine.LastMessage("gemini", echo+queued+footer); !ok || strings.Contains(line, "Queued") || strings.Contains(line, "Also, after") {
		t.Fatalf("queued pane with no reply yet quotes %q ok=%v", line, ok)
	}
}

// gemini's approval dialog replaces the composer, so the newest "> " row is the
// echo of the prompt that raised it and the dialog sits below. The reply line
// quotes what the dialog asks, not the previous answer. Frame captured from
// gemini v0.61.0.
func TestLastMessageQuotesGeminiApprovalQuestion(t *testing.T) {
	engine := defaultEngine(t)
	pane := " > Tea or coffee?\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		"✦ Tea, good choice.\n" +
		"\n" +
		"▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n" +
		" > Run the shell command `sleep 15; echo second-done` in the foreground and wait for it to finish.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		"\n" +
		"╭────────────────────────────────────────────────────────────────╮\n" +
		"│ ? Shell  sleep 15; echo second-done                            │\n" +
		"│ ╭────────────────────────────────────────────────────────────╮ │\n" +
		"│ │ sleep 15; echo second-done                                 │ │\n" +
		"│ ╰────────────────────────────────────────────────────────────╯ │\n" +
		"│ Allow execution of [Shell]?                                    │\n" +
		"│                                                                │\n" +
		"│ ● 1. Allow once                                                │\n" +
		"│   2. Allow for this session                                    │\n" +
		"│   3. No, suggest changes (esc)                                 │\n" +
		"╰────────────────────────────────────────────────────────────────╯"
	if state, ok := engine.Match("gemini", pane); !ok || state != Waiting {
		t.Fatalf("approval pane state = %q ok=%v, want waiting", state, ok)
	}
	if line, anchored, ok := engine.LastMessage("gemini", pane); !ok || !anchored || line != "Allow execution of [Shell]?" {
		t.Fatalf("approval pane quote = %q anchored=%v ok=%v, want the dialog's question", line, anchored, ok)
	}
}

// a message sent with Enter during a turn is drawn under the running step as
// "Messages to be submitted ..." until the tool call ends, and a rejected
// steer re-appears under an end-of-turn heading; the heading wraps on a
// narrow pane, and none of it is the reply the row quotes.
func TestLastMessageSkipsCodexPendingMessages(t *testing.T) {
	engine := defaultEngine(t)
	transcript := "› Run the shell command `sleep 60` in the foreground and wait for it to finish.\n" +
		"\n" +
		"• Running sleep 60\n" +
		"\n" +
		"• Working (12s • esc to interrupt)\n" +
		"\n"
	composer := "› Ask Codex to do anything\n" +
		"  gpt-5.1-codex default · /home/dev"
	want, _, _ := engine.LastMessage("codex", transcript+composer)
	blocks := map[string]string{
		"120 cols": "• Messages to be submitted after next tool call (press esc to interrupt and send immediately)\n" +
			"  ↳ Please also say hello when done.\n",
		"22 cols": "• Messages to be\n" +
			"  submitted after\n" +
			"  next tool call\n" +
			"  (press esc to\n" +
			"  interrupt and send\n" +
			"  immediately)\n" +
			"  ↳ Please also say\n" +
			"    hello when done.\n",
		"15 cols": "• Messages to\n" +
			"  be submitted\n" +
			"  after next\n" +
			"  tool call\n" +
			"  (press esc to\n" +
			"  interrupt and\n" +
			"  send\n" +
			"  immediately)\n" +
			"  ↳ Please also\n" +
			"    say hello\n" +
			"    when done.\n",
		"end of turn, 120 cols": "• Messages to be submitted at end of turn\n" +
			"  ↳ Rejected steer that will be retried.\n",
		"end of turn, 15 cols": "• Messages to\n" +
			"  be submitted at\n" +
			"  end of turn\n" +
			"  ↳ Rejected\n" +
			"    steer.\n",
	}
	for name, block := range blocks {
		pane := transcript + block + "\n" + composer
		if line, _, ok := engine.LastMessage("codex", pane); !ok || line != want {
			t.Errorf("%s pending block quote = %q ok=%v, want %q as without the block", name, line, ok, want)
		}
	}

	reply := "› Status?\n" +
		"\n" +
		"• Messages arrive in order.\n" +
		"  done 12:59 AM\n" +
		"\n" +
		composer
	if line, _, ok := engine.LastMessage("codex", reply); !ok || line != "Messages arrive in order." {
		t.Fatalf("genuine reply starting 'Messages' quote = %q ok=%v", line, ok)
	}

	for name, text := range map[string]struct{ body, want string }{
		"one line":  {"• Messages to be retried go to the dead-letter queue.\n", "Messages to be retried go to the dead-letter queue."},
		"wrapped":   {"• Messages to\n  be retried go to the DLQ.\n", "Messages to be retried go to the DLQ."},
		"submitted": {"• Messages to be submitted soon are batched.\n", "Messages to be submitted soon are batched."},
	} {
		pane := "› Status?\n\n" + text.body + "  done 12:59 AM\n\n" + composer
		if line, _, ok := engine.LastMessage("codex", pane); !ok || line != text.want {
			t.Errorf("%s genuine reply quote = %q ok=%v, want %q", name, line, ok, text.want)
		}
	}
}

func TestChromeBlockRows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		lines   []string
		want    []bool
	}{
		{
			name:    "ordinary block ends at blank",
			pattern: `^Help improve Grok|^\s+❯ `,
			lines:   []string{"Help improve Grok", "  details", "", "reply"},
			want:    []bool{true, true, false, false},
		},
		{
			name:    "prompt inside ordinary block",
			pattern: `^Help improve Grok|^\s+❯ `,
			lines:   []string{"Help improve Grok", "     ❯ prompt", "", "       continued", "     reply"},
			want:    []bool{true, true, true, true, false},
		},
		{
			name:    "prompt crosses blank and ends at equal indent",
			pattern: `^\s+❯ `,
			lines:   []string{"     ❯ prompt", "", "       second paragraph", "     reply"},
			want:    []bool{true, true, true, false},
		},
		{
			name:    "prompt ends at lesser indent",
			pattern: `^\s+❯ `,
			lines:   []string{"     ❯ prompt", "       continuation", "    reply"},
			want:    []bool{true, true, false},
		},
		{
			name:    "prompt reaches end of pane",
			pattern: `^\s+❯ `,
			lines:   []string{"     ❯ prompt", "", "       continuation"},
			want:    []bool{true, true, true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := toolRules{
				chromeBlock:    regexp.MustCompile(tc.pattern),
				activityCutoff: regexp.MustCompile(`^❯`),
			}
			if got := tr.chromeBlockRows(tc.lines); !slices.Equal(got, tc.want) {
				t.Fatalf("chromeBlockRows(%q) = %v, want %v", tc.lines, got, tc.want)
			}
		})
	}
}

// A web_fetch dialog draws the tool's prompt above its own question; the quote
// is the question nearest the options, not the first row ending in "?".
func TestLastMessageQuotesGeminiQuestionNearestOptions(t *testing.T) {
	engine := defaultEngine(t)
	pane := " > Read https://example.com and tell me, what is its main heading?\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n\n" +
		"╭──────────────────────────────────────────────────────────────────────╮\n" +
		"│ Read https://example.com and tell me, what is its main heading?      │\n" +
		"│                                                                      │\n" +
		"│ URLs to fetch:                                                       │\n" +
		"│  - https://example.com/                                              │\n" +
		"│ Do you want to proceed?                                              │\n" +
		"│                                                                      │\n" +
		"│ ● 1. Allow once                                                      │\n" +
		"│   2. Allow for this session                                          │\n" +
		"│   3. No, suggest changes (esc)                                       │\n" +
		"╰──────────────────────────────────────────────────────────────────────╯"
	if line, anchored, ok := engine.LastMessage("gemini", pane); !ok || !anchored || line != "Do you want to proceed?" {
		t.Fatalf("web_fetch dialog quote = %q anchored=%v ok=%v, want the dialog's question", line, anchored, ok)
	}
}

// A question wider than the pane wraps across rows; the quote joins them.
func TestLastMessageJoinsWrappedGeminiQuestion(t *testing.T) {
	engine := defaultEngine(t)
	pane := " > Ask me which shell I prefer.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n\n" +
		"╭──────────────────────────────────────────────────────────────────────╮\n" +
		"│ ? Ask User                                                           │\n" +
		"│ Which shell do you prefer for daily work on remote servers and on   │\n" +
		"│ local machines alike?                                                      │\n" +
		"│                                                                      │\n" +
		"│ ● 1. bash                                                            │\n" +
		"│   2. zsh                                                             │\n" +
		"╰──────────────────────────────────────────────────────────────────────╯"
	want := "Which shell do you prefer for daily work on remote servers and on local machines alike?"
	if line, anchored, ok := engine.LastMessage("gemini", pane); !ok || !anchored || line != want {
		t.Fatalf("wrapped question quote = %q anchored=%v ok=%v, want %q", line, anchored, ok, want)
	}
}

// A question's wrapped rows are measured in terminal cells: a wide-character
// word that fits by rune count can still not fit on the row above it.
func TestLastMessageJoinsWrappedGeminiQuestionByCells(t *testing.T) {
	engine := defaultEngine(t)
	pane := " > Ask me.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n\n" +
		"╭──────────────────────────────────────────────────────────────────────╮\n" +
		"│ ? Ask User                                                           │\n" +
		"│ Which shell do you prefer for daily work on remote servers           │\n" +
		"│ 世界你好吗 and more?                                                 │\n" +
		"│                                                                      │\n" +
		"│ ● 1. bash                                                            │\n" +
		"╰──────────────────────────────────────────────────────────────────────╯"
	want := "Which shell do you prefer for daily work on remote servers 世界你好吗 and more?"
	if line, anchored, ok := engine.LastMessage("gemini", pane); !ok || !anchored || line != want {
		t.Fatalf("wide-character question quote = %q anchored=%v ok=%v, want %q", line, anchored, ok, want)
	}
}

// A list item that fills its row does not continue the question below it.
func TestLastMessageKeepsFullListRowOutOfGeminiQuestion(t *testing.T) {
	engine := defaultEngine(t)
	pane := " > Fetch it.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n\n" +
		"╭──────────────────────────────────────────────────────────────────────╮\n" +
		"│ URLs to fetch:                                                       │\n" +
		"│ - https://example.com/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   │\n" +
		"│ Do you want to proceed?                                              │\n" +
		"│                                                                      │\n" +
		"│ ● 1. Allow once                                                      │\n" +
		"╰──────────────────────────────────────────────────────────────────────╯"
	if line, anchored, ok := engine.LastMessage("gemini", pane); !ok || !anchored || line != "Do you want to proceed?" {
		t.Fatalf("quote with a full URL row = %q anchored=%v ok=%v, want the dialog's question", line, anchored, ok)
	}
}
