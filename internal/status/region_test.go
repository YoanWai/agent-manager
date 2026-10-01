package status

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
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
