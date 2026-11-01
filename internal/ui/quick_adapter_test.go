package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uiquicksend "github.com/YoanWai/agent-manager/internal/ui/quick/send"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// pasteQuickImage runs a full ctrl+v paste against a fake clipboard that
// yields the given file, and returns the id of the chip it left behind.
func pasteQuickImage(t *testing.T, m *Model, path string) int {
	t.Helper()
	orig := composer.CaptureImage
	defer func() { composer.CaptureImage = orig }()
	composer.CaptureImage = func() (string, error) { return path, nil }

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("ctrl+v should start an async clipboard read")
	}
	id := m.quick().LastImageID
	msg, ok := cmd().(composer.PasteImageMsg)
	if !ok {
		t.Fatalf("clipboard cmd returned %T", cmd())
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
	if m.errBar.text != "" {
		t.Fatalf("paste: %q", m.errBar.text)
	}
	return id
}

func tempImage(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func quickOffsetOf(t *testing.T, m *Model, needle string) int {
	t.Helper()
	idx := strings.Index(m.quick().Input.Value(), needle)
	if idx < 0 {
		t.Fatalf("%q not in prompt %q", needle, m.quick().Input.Value())
	}
	return utf8.RuneCountInString(m.quick().Input.Value()[:idx])
}

func applyQuickKeyCmd(t *testing.T, m *Model, msg tea.KeyMsg) {
	t.Helper()
	_, cmd := m.handleQuickKey(msg)
	if cmd != nil {
		m.applyTestMsg(t, cmd())
	}
}

func TestOpenQuickModeDefersSettingsReadAndGuardsDefaults(t *testing.T) {
	t.Parallel()
	t.Run("pristine", func(t *testing.T) {
		m := buildModel(t)
		reader := &blockedSettingReader{
			started: make(chan struct{}),
			release: make(chan struct{}),
			values: map[string]string{
				"default_tool":               "ready-tool",
				uisettings.QuickCloseSetting: "close",
				uisettings.WorktreeSetting:   "on",
			},
		}
		cmd := m.openQuickModeWithReader(reader)
		select {
		case <-reader.started:
			t.Fatal("opening quick mode read settings before its Bubble Tea command")
		default:
		}
		completed := make(chan tea.Msg, 1)
		go func() { completed <- cmd() }()
		<-reader.started
		close(reader.release)
		m.applyTestMsg(t, <-completed)
		if got := m.quickTool(); got != "ready-tool" {
			t.Fatalf("refreshed quick tool = %q, want ready-tool", got)
		}
		if !m.quick().CloseAfterSend || !m.quick().Worktree() {
			t.Fatalf("quick defaults were not refreshed: close=%t worktree=%t", m.quick().CloseAfterSend, m.quick().Worktree())
		}
	})

	t.Run("local choice", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openQuickMode()
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyTab})
		chosen := m.quickTool()
		if err := m.services.store.SetSetting("default_tool", "ready-tool"); err != nil {
			t.Fatal(err)
		}
		m.applyTestMsg(t, cmd())
		if got := m.quickTool(); got != chosen {
			t.Fatalf("async defaults replaced local quick tool: got %q want %q", got, chosen)
		}
	})

	t.Run("reopened", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openQuickMode()
		generation := m.quick().Gen
		if err := m.services.store.SetSetting("default_tool", "ready-tool"); err != nil {
			t.Fatal(err)
		}
		loaded := cmd()
		m.openQuickMode()
		if m.quick().Gen == generation {
			t.Fatal("reopened quick mode reused its generation")
		}
		m.applyTestMsg(t, loaded)
		if got := m.quickTool(); got == "ready-tool" {
			t.Fatal("old defaults replaced reopened quick mode")
		}
	})
}

func TestQuickPromptDeadSessionSetsError(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.SetKeys(m.services.listKeys.With(keybind.Revive, bindingOf(t, "U")).With(keybind.Restart, bindingOf(t))); err != nil {
		t.Fatal(err)
	}
	createSession(t, m, "gone", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "gone")

	m.openQuickMode()
	m.quick().Input.SetValue("hello?")
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.errBar.text != "session is dead - press U to revive" {
		t.Fatalf("err = %q", m.errBar.text)
	}
	if m.quick() == nil {
		t.Fatal("quick mode should stay open after a failed send")
	}
	if _, err := m.services.store.Get(sess.ID); err != nil {
		t.Fatalf("session record should survive: %v", err)
	}
}

func TestQuickPromptSendClearsAcked(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.selectSessionRow(t, "answer-me")

	m.openQuickMode()
	if m.quick() == nil {
		t.Fatalf("quick mode should activate, err = %q", m.errBar.text)
	}
	m.quick().Input.SetValue("carry on with the plan")
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	if m.quick() == nil {
		t.Fatal("quick mode should stay active after a send")
	}
	if m.quick().Input.Value() != "" {
		t.Fatal("input should clear after a send")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Acked {
		t.Fatal("quick prompt send should clear the acked flag")
	}
}

func TestQuickPasteInsertsChipAtCursor(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	m.quick().Input.SetValue("fix header and footer")
	m.quick().Input.SetCursor(len("fix header"))
	path := tempImage(t, "paste-test.png")
	id := pasteQuickImage(t, m, path)

	want := "fix header " + composer.Token(id) + " and footer"
	if got := m.quick().Input.Value(); got != want {
		t.Fatalf("chip should land at the caret: got %q, want %q", got, want)
	}
	if len(m.quick().Attachments) != 1 || m.quick().Attachments[0].Path != path {
		t.Fatalf("attachment = %+v", m.quick().Attachments)
	}
	// Typing continues after the chip, leaving the text around it intact.
	for _, r := range " now" {
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.quick().Input.Value(); got != "fix header "+composer.Token(id)+" now and footer" {
		t.Fatalf("typing after the chip disturbed the text: %q", got)
	}
}

func TestQuickChipRendersInlineInTheInput(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := buildModel(t)
	m.openQuickMode()
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/agent-manager-pastes/paste-123.png"}}
	m.quick().Input.SetValue("look at " + composer.Token(1) + " please")
	m.quick().Input.SetWidth(60)

	view := m.quick().Composer.View(composerPaint())
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "look at "+composer.Token(1)+" please") {
		t.Fatalf("chip should read inline with the text, got %q", plain)
	}
	if strings.Contains(plain, "paste-123") {
		t.Fatalf("chip should not show the temp path, got %q", plain)
	}
	if plain == view {
		t.Fatal("chip should be styled in the rendered prompt")
	}
	// Styling must add color only: the token's width is what the textarea
	// already used to wrap the line.
	if lipgloss.Width(ansi.Strip(uitheme.ImageChip(composer.Token(1)))) != lipgloss.Width(composer.Token(1)) {
		t.Fatal("styled chip must keep the token's width")
	}

	m.quick().Attachments = []composer.Attachment{{ID: 2}}
	m.quick().Input.SetValue(composer.Token(2))
	pending := m.quick().Composer.View(composerPaint())
	if !strings.Contains(pending, uitheme.ImageChipPasting(composer.Token(2))) {
		t.Fatal("a chip waiting on the clipboard should wear the pasting style")
	}
}

func TestQuickMessageSubstitutesPathsInPlace(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/a.png"}, {ID: 2, Path: "/tmp/b.png"}}
	m.quick().Input.SetValue("compare " + composer.Token(1) + " with " + composer.Token(2))
	if got := m.quick().Message(); got != "compare /tmp/a.png with /tmp/b.png" {
		t.Fatalf("compose = %q", got)
	}

	m.quick().Input.SetValue(composer.Token(2) + " " + composer.Token(1))
	if got := m.quick().Message(); got != "/tmp/b.png /tmp/a.png" {
		t.Fatalf("paths should follow the order they were pasted in: %q", got)
	}
}

func TestQuickBackspaceDeletesTheWholeChipAtTheCaret(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	first := tempImage(t, "first.png")
	second := tempImage(t, "second.png")
	m.quick().LastImageID = 2
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: first}, {ID: 2, Path: second}}
	m.quick().Input.SetValue("this " + composer.Token(1) + " and that " + composer.Token(2))
	m.quick().Input.SetCursor(quickOffsetOf(t, m, composer.Token(1)) + utf8.RuneCountInString(composer.Token(1)))

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.quick().Input.Value(); got != "this  and that "+composer.Token(2) {
		t.Fatalf("backspace beside a chip should remove it whole, got %q", got)
	}
	if len(m.quick().Attachments) != 1 || m.quick().Attachments[0].ID != 2 {
		t.Fatalf("the other chip should stay: %+v", m.quick().Attachments)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("deleted chip's temp file should be removed, err=%v", err)
	}
	if m.quick().Message() != "this  and that "+second {
		t.Fatalf("the surviving chip should still send its path: %q", m.quick().Message())
	}

	// Away from a chip, backspace is ordinary text editing.
	m.quick().Input.CursorEnd()
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.quick().Attachments) != 1 {
		t.Fatalf("plain backspace should leave chips alone: %+v", m.quick().Attachments)
	}
}

func TestQuickDeleteRemovesTheChipInFront(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "forward.png")
	m.quick().LastImageID = 1
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: path}}
	m.quick().Input.SetValue("head " + composer.Token(1) + " tail")
	m.quick().Input.SetCursor(quickOffsetOf(t, m, composer.Token(1)))

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDelete})
	if got := m.quick().Input.Value(); got != "head  tail" {
		t.Fatalf("delete in front of a chip should remove it whole, got %q", got)
	}
	if len(m.quick().Attachments) != 0 {
		t.Fatalf("attachment should be released: %+v", m.quick().Attachments)
	}
}

func TestQuickArrowsStepOverAChip(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().LastImageID = 1
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/a.png"}}
	m.quick().Input.SetValue("a " + composer.Token(1) + " b")
	start := quickOffsetOf(t, m, composer.Token(1))
	end := start + utf8.RuneCountInString(composer.Token(1))
	m.quick().Input.SetCursor(end)

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyLeft})
	if got := m.quick().CursorOffset(); got != start {
		t.Fatalf("left should clear the chip in one step: offset %d, want %d", got, start)
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.quick().CursorOffset(); got != end {
		t.Fatalf("right should clear the chip in one step: offset %d, want %d", got, end)
	}
}

func TestQuickChipsReflowAcrossWrappedRows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/a.png"}}
	m.quick().Input.SetWidth(24)
	m.quick().Input.SetHeight(uiquick.BarMaxRows)
	m.quick().Input.SetValue(strings.Repeat("word ", 5) + composer.Token(1) + " tail")

	rendered := ansi.Strip(m.quick().Composer.View(composerPaint()))
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) > 24 {
			t.Fatalf("wrapped row overflows the bar: %q", line)
		}
	}
	if !strings.Contains(rendered, composer.Token(1)) {
		t.Fatalf("chip should survive wrapping, got %q", rendered)
	}
}

func TestQuickBulkDeleteReleasesTheImage(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "orphan.png")
	m.quick().LastImageID = 1
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: path}}
	m.quick().Input.SetValue("note " + composer.Token(1))
	m.quick().Input.CursorEnd()

	// ctrl+u wipes the line without going through the chip-aware keys.
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(m.quick().Attachments) != 0 {
		t.Fatalf("a chip cut by a bulk delete should release its image: %+v", m.quick().Attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("orphaned temp file should be removed, err=%v", err)
	}
}

func TestQuickPasteReservesTheChipUntilTheReadLands(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("ctrl+v should start an async clipboard read")
	}
	id := m.quick().LastImageID
	if !strings.Contains(m.quick().Input.Value(), composer.Token(id)) {
		t.Fatalf("the chip should hold its spot while the read runs, got %q", m.quick().Input.Value())
	}
	if !m.quick().Pasting() {
		t.Fatal("the reserved chip should read as pasting")
	}
	// The caret sits after the chip, so typing keeps flowing.
	for _, r := range "later" {
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.quick().Input.Value(); got != composer.Token(id)+" later" {
		t.Fatalf("typing during a paste = %q", got)
	}
	_, _ = m.submitQuick()
	if m.errBar.text == "" {
		t.Fatal("submitting mid-paste should say the image is not ready")
	}

	path := tempImage(t, "landed.png")
	updated, _ := m.Update(composer.PasteImageMsg{Target: composer.Quick, Gen: m.quick().Gen, ID: id, Path: path})
	m = updated.(*Model)
	if m.quick().Pasting() {
		t.Fatal("the chip should settle once the read lands")
	}
	if got := m.quick().Input.Value(); got != composer.Token(id)+" later" {
		t.Fatalf("the chip should resolve in place, got %q", got)
	}
	if got := m.quick().Message(); got != path+" later" {
		t.Fatalf("compose after the read = %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "png-bytes" {
		t.Fatalf("attachment file: %v %q", err, data)
	}
}

func TestQuickChipRemovalKeepsAMultiLinePrompt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().LastImageID = 1
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/a.png"}}
	m.quick().Input.SetValue("first line\nsecond " + composer.Token(1) + " line\nthird line")
	m.quick().Input.CursorUp()
	m.quick().Input.SetCursor(len("second ") + utf8.RuneCountInString(composer.Token(1)))
	if m.quick().Input.Line() != 1 {
		t.Fatalf("test setup: caret on row %d", m.quick().Input.Line())
	}

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.quick().Input.Value(); got != "first line\nsecond  line\nthird line" {
		t.Fatalf("removing a chip should leave the other rows alone, got %q", got)
	}
	if m.quick().Input.Line() != 1 || m.quick().CursorOffset() != len("first line\nsecond ") {
		t.Fatalf("caret should stay where the chip was: row %d offset %d", m.quick().Input.Line(), m.quick().CursorOffset())
	}
}

func TestQuickPasteRefusedWhenThePromptIsFull(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.SetValue(strings.Repeat("x", m.quick().Input.CharLimit))
	m.quick().Input.CursorEnd()

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		t.Fatal("a full prompt should not start a clipboard read")
	}
	if m.errBar.text == "" {
		t.Fatal("a refused paste should say why")
	}
	if len(m.quick().Attachments) != 0 {
		t.Fatalf("no chip should be reserved: %+v", m.quick().Attachments)
	}
	if strings.Contains(m.quick().Input.Value(), "[Image") {
		t.Fatal("a truncated token must never reach the prompt")
	}
}

func TestQuickEscReleasesPastedImages(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "abandoned.png")
	q := m.quick()
	q.Attachments = []composer.Attachment{{ID: 1, Path: path}}
	q.Input.SetValue("never mind " + composer.Token(1))

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quick() != nil || len(q.Attachments) != 0 {
		t.Fatalf("closing the bar should close it and release its images: open %t, %+v", m.quick() != nil, q.Attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("abandoned temp file should be removed, err=%v", err)
	}
}

func TestQuickImageMsgNoImageReachesTextPaste(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick().Input.SetValue("keep this")
	m.quick().Input.CursorEnd()

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	id := m.quick().LastImageID
	updated, cmd := m.Update(composer.PasteImageMsg{Target: composer.Quick, Gen: m.quick().Gen, ID: id, NoImage: true})
	m = updated.(*Model)
	if m.quick().Pasting() || len(m.quick().Attachments) != 0 {
		t.Fatalf("a clipboard with no image should take the chip back out: %+v", m.quick().Attachments)
	}
	if got := m.quick().Input.Value(); got != "keep this" {
		t.Fatalf("removing the chip should restore the typed text, got %q", got)
	}
	// Textarea paste returns a cmd for its own clipboard read, or nil if empty.
	_ = cmd
}

func TestQuickAttachImageRealErrorSurfaces(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick().Input.SetValue("look")
	m.quick().Input.CursorEnd()

	orig := composer.CaptureImage
	defer func() { composer.CaptureImage = orig }()
	composer.CaptureImage = func() (string, error) {
		return "", errors.New("install wl-clipboard or xclip to paste images")
	}

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	msg := cmd().(composer.PasteImageMsg)
	updated, _ := m.Update(msg)
	m = updated.(*Model)
	if m.errBar.text == "" {
		t.Fatal("a real clipboard error should surface through m.errBar.text")
	}
	if m.quick().Pasting() || len(m.quick().Attachments) != 0 {
		t.Fatalf("a failed read should take the chip back out: %+v", m.quick().Attachments)
	}
	if got := m.quick().Input.Value(); got != "look" {
		t.Fatalf("a failed read should leave the typed text, got %q", got)
	}
}

func TestQuickSpawnOnGroupCreatesSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.services.store.SetDefaultTool("claude"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}

	m.openQuickMode()
	m.quick().Input.SetValue("build the api")
	_, cmd := m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("quick spawn: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d want 1", len(sessions))
	}
	spawned := sessions[0]
	if spawned.Group != "backend" || spawned.Tool != "claude" || spawned.Cwd != dir {
		t.Fatalf("spawned session fields wrong: %+v", spawned)
	}
	if !m.services.tmux.Exists(spawned.ID) {
		t.Fatal("tmux session should exist after quick spawn")
	}
	if m.quick().Input.Value() != "" {
		t.Fatal("input should clear after a spawn")
	}
}

func TestQuickSpawnUsesTabCycledTool(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}

	m.openQuickMode()
	if m.quickTool() != "claude" {
		t.Fatalf("quick tool starts at %q want claude", m.quickTool())
	}
	if _, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyTab}); m.quickTool() != "claude-hooked" {
		t.Fatalf("after tab, quick tool = %q want claude-hooked", m.quickTool())
	}

	m.quick().Input.SetValue("build the api")
	_, cmd := m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("quick spawn: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d want 1", len(sessions))
	}
	if sessions[0].Tool != "claude-hooked" {
		t.Fatalf("spawned tool = %q want claude-hooked", sessions[0].Tool)
	}
}

func TestQuickShiftTabStepsToolBack(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	names := quickTools(m)
	if len(names) < 2 {
		t.Fatalf("need at least two tools, got %v", names)
	}
	setQuickTool(m, 1)
	if m.handleQuickKey(tea.KeyMsg{Type: tea.KeyShiftTab}); m.quickTool() != names[0] {
		t.Fatalf("shift+tab from the second tool = %q want %q", m.quickTool(), names[0])
	}
	if m.handleQuickKey(tea.KeyMsg{Type: tea.KeyShiftTab}); m.quickTool() != names[len(names)-1] {
		t.Fatalf("shift+tab from the first tool = %q want the last, %q", m.quickTool(), names[len(names)-1])
	}
	if m.quick().WorktreeTouched() {
		t.Fatal("shift+tab must not touch the worktree toggle")
	}
}

func closeQuick(m *Model) {
	if m.quick() != nil {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestQuickRemembersLastSpawnTool(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	m.openQuickMode()
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.quickTool() != "claude-hooked" {
		t.Fatalf("after tab, quick tool = %q want claude-hooked", m.quickTool())
	}
	m.quick().Input.SetValue("build the api")
	_, cmd := m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("quick spawn: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	closeQuick(m)

	m.openQuickMode()
	if got := m.quickTool(); got != "claude-hooked" {
		t.Fatalf("next quick tool = %q, want last spawn", got)
	}
}

func TestQuickRemembersLastSpawnWorktree(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	m := quickGroupModel(t, repo)
	m.openQuickMode()
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.quickWorktreeOn() {
		t.Fatal("ctrl+t should turn worktree on")
	}
	m.quick().Input.SetValue("do a thing")
	_, cmd := m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("quick spawn: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	closeQuick(m)

	m.openQuickMode()
	m.applyTestMsg(t, m.quickWorktreeProbeCmd(false)())
	if !m.quickWorktreeOn() {
		t.Fatal("next quick bar should seed worktree from the last spawn")
	}
}

func TestQuickHiddenLastToolFallsBackToSettings(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	m := quickGroupModel(t, repo)
	m.openQuickMode()
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.quickWorktreeOn() {
		t.Fatal("ctrl+t should turn worktree on")
	}
	m.quick().Input.SetValue("do a thing")
	_, cmd := m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("quick spawn: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	closeQuick(m)

	if err := m.services.store.SetHiddenTools(map[string]bool{m.ledger.spawner.LastSpawnTool: true}); err != nil {
		t.Fatal(err)
	}

	m.applyTestMsg(t, m.openQuickMode()())
	m.applyTestMsg(t, m.quickWorktreeProbeCmd(false)())
	if m.quickWorktreeOn() {
		t.Fatal("hidden last tool should fall back to the settings worktree default")
	}
}

func TestQuickRemembersPickOnlyAfterInstallRetrySucceeds(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	m := quickGroupModel(t, dir)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	submitFormSession(t, m, "first")

	installCommand := fakeInstallCommand(t)
	tool := m.services.cfg.Tools["claude"]
	tool.Command = "am-fake-cli"
	m.services.cfg.Tools["claude"] = tool
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	for i, name := range quickTools(m) {
		if name == "claude" {
			setQuickTool(m, i)
		}
	}
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	m.quick().Input.SetValue("do a thing")
	_, cmd := m.submitQuick()
	m.applyCmd(t, cmd)

	if !hintOpen(m) || hintFix(m).Retry == nil {
		t.Fatalf("expected a refused launch with retry, screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	if m.ledger.spawner.LastSpawnTool != "ready-tool" || m.ledger.spawner.LastSpawnWorktree {
		t.Fatalf("failed launch changed the last pick: %q, %v", m.ledger.spawner.LastSpawnTool, m.ledger.spawner.LastSpawnWorktree)
	}
	fix := hintFix(m)
	fix.Command, fix.Script = installCommand, installCommand
	m.presentLaunchHint(fix)
	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if m.ledger.spawner.LastSpawnTool != "claude" || !m.ledger.spawner.LastSpawnWorktree {
		t.Fatalf("successful retry did not remember the pick: %q, %v; err=%q", m.ledger.spawner.LastSpawnTool, m.ledger.spawner.LastSpawnWorktree, m.errBar.text)
	}
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	m.applyTestMsg(t, m.quickWorktreeProbeCmd(false)())
	if m.quickTool() != "claude" || !m.quickWorktreeOn() {
		t.Fatal("next quick bar should use the successful retry's tool and worktree")
	}
}

func TestQuickCancelDoesNotRememberLastPick(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyTab})
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEsc})

	m.openQuickMode()
	if got := m.quickTool(); got != "claude" {
		t.Fatalf("cancelled pick must not seed the next bar, got %q", got)
	}
}

func TestQuickSeedsFromLastFormSpawn(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.openForm()
	pickFormTool(t, m, "ready-tool")
	submitFormSession(t, m, "first")

	m.selectGroupRow(t, "backend")
	m.openQuickMode()
	if got := m.quickTool(); got != "ready-tool" {
		t.Fatalf("quick tool = %q, want last form spawn", got)
	}
}

func TestQuickCloseAfterSendDefaultsToStayingOpen(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.settings.Cache.Value(uisettings.QuickCloseSetting) == "close" {
		t.Fatal("quick bar should stay open by default")
	}
	if err := m.services.store.SetSetting(uisettings.QuickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openQuickMode()())
	if !m.quick().CloseAfterSend {
		t.Fatal("refreshed close choice should opt in")
	}
}

func TestQuickPromptClosesAfterSendWhenEnabled(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	if err := m.services.store.SetSetting(uisettings.QuickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}

	m.applyTestMsg(t, m.openQuickMode()())
	m.quick().Input.SetValue("carry on with the plan")
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	if m.quick() != nil {
		t.Fatal("quick mode should close after a send when the setting is on")
	}
}

func TestQuickWorktreeToggle(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	if m.quick().Worktree() {
		t.Fatal("worktree should default off")
	}
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.quick().Worktree() {
		t.Fatal("ctrl+t should toggle worktree on")
	}
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}, Alt: true})
	if m.quick().Worktree() {
		t.Fatal("alt+w should toggle worktree back off")
	}
}

func TestQuickWorktreeSeedsFromSetting(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openQuickMode()())
	if !m.quick().Worktree() {
		t.Fatal("quick bar should seed worktree on from setting")
	}
}

// quickGroupModel selects a quick-bar target group pointing at dir.
func quickGroupModel(t *testing.T, dir string) *Model {
	t.Helper()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("grp", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	return m
}

func TestQuickWorktreeGatedInNonRepoGroup(t *testing.T) {
	dir := t.TempDir()
	m := quickGroupModel(t, dir)
	if err := m.services.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	if m.quickWorktreeOn() {
		t.Fatal("a non-repo group dir cannot host a worktree, even with the group default on")
	}
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.quickWorktreeOn() {
		t.Fatal("ctrl+t must not turn worktree on for a non-repo dir")
	}
	if !strings.Contains(m.errBar.text, "need a git repository") {
		t.Fatalf("refused toggle should say why, got %q", m.errBar.text)
	}
	if bar := ansi.Strip(m.viewQuickBar(120, uiquick.BarMaxRows)); !strings.Contains(bar, "⎇ no repo") {
		t.Fatalf("quick bar should mark worktree unavailable, got %q", bar)
	}
	m.quick().Input.SetValue("do a thing")
	_, cmd := m.submitQuick()
	m.applyCmd(t, cmd)
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("gated spawn should still launch a plain session, got %d", len(sessions))
	}
	if sessions[0].WorktreeRepo != "" {
		t.Fatalf("gated spawn must not record a worktree: %q", sessions[0].WorktreeRepo)
	}
	if sessions[0].Cwd != dir {
		t.Fatalf("cwd = %q, want the group dir %q", sessions[0].Cwd, dir)
	}
}

func TestQuickSpawnUsesGroupWorktreeDefault(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	m := quickGroupModel(t, repo)
	if err := m.services.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	m.applyTestMsg(t, m.quickWorktreeProbeCmd(false)())
	if !m.quickWorktreeOn() {
		t.Fatal("quick bar should show the group's worktree default on")
	}
	m.quick().Input.SetValue("do a thing")
	_, cmd := m.submitQuick()
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("worktree spawn should succeed: %q", m.errBar.text)
	}
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 || sessions[0].WorktreeRepo == "" {
		t.Fatalf("group default should have spawned a worktree session, got %+v", sessions)
	}
}

func TestQuickWorktreeToggleOverridesGroupDefault(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	m := quickGroupModel(t, repo)
	if err := m.services.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.quickWorktreeOn() {
		t.Fatal("ctrl+t should override the group default off")
	}
	m.quick().Input.SetValue("do a thing")
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.errBar.text != "" {
		t.Fatalf("plain spawn should succeed: %q", m.errBar.text)
	}
}

// The quick prompt is where people type sentences at an agent. SendText
// pastes and presses Enter, so on a shell row that sentence would run as a
// command: the target's tool is checked before anything is delivered.
func TestQuickPromptRefusesAShell(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	m.openQuickMode()
	m.quick().Input.SetValue("summarise what you just did")
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.errBar.text != uisession.ShellPromptHint(sess.Name) {
		t.Fatalf("err = %q, want the shell refusal", m.errBar.text)
	}
	if m.quick().Input.Value() == "" {
		t.Fatal("a refused prompt should stay in the bar, not vanish")
	}
}

// The same guard from the other end: what the user typed never reaches the
// shell, so it never runs.
func TestQuickPromptNeverRunsWhatIsTypedAtAShell(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	marker := filepath.Join(t.TempDir(), "executed")
	m.openQuickMode()
	m.quick().Input.SetValue("touch " + marker)
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())

	// A shell that took the line would have created the file well inside
	// this window; a guard that holds leaves nothing to wait for.
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("the quick prompt executed %q in the shell", marker)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestQuickSendRecordsLastPrompt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick().Input.SetValue("carry on with the plan")
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	got, err := m.services.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastPrompt != "carry on with the plan" {
		t.Fatalf("last prompt = %q, want the quick send", got.LastPrompt)
	}
}

// seedTwoGroups gives the list two rows so a selection move is observable.
func seedTwoGroups(t *testing.T, m *Model) {
	t.Helper()
	for _, name := range []string{"alpha", "beta"} {
		if err := m.services.store.CreateGroup(name, t.TempDir()); err != nil {
			t.Fatalf("create group: %v", err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	if len(railRows(m)) < 2 {
		t.Fatalf("rows = %d, want at least 2", len(railRows(m)))
	}
}

func TestQuickUpDownMoveTheCaretBetweenPromptRows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	m.openQuickMode()
	m.quick().Input.SetWidth(40)
	m.quick().Input.SetHeight(uiquick.BarMaxRows)
	m.quick().Input.SetValue("first\nsecond\nthird")

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.quick().Input.Line() != 1 || m.rail.Cursor() != 1 {
		t.Fatalf("up from the last row: caret line %d, selection %d; want caret 1, selection 1", m.quick().Input.Line(), m.rail.Cursor())
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.quick().Input.Line() != 0 || m.rail.Cursor() != 1 {
		t.Fatalf("up from the middle row: caret line %d, selection %d; want caret 0, selection 1", m.quick().Input.Line(), m.rail.Cursor())
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.quick().Input.Line() != 0 || m.rail.Cursor() != 0 {
		t.Fatalf("up from the first row: caret line %d, selection %d; want caret 0, selection 0", m.quick().Input.Line(), m.rail.Cursor())
	}

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.quick().Input.Line() != 1 || m.rail.Cursor() != 0 {
		t.Fatalf("down from the first row: caret line %d, selection %d; want caret 1, selection 0", m.quick().Input.Line(), m.rail.Cursor())
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.quick().Input.Line() != 2 || m.rail.Cursor() != 1 {
		t.Fatalf("down from the last row: caret line %d, selection %d; want caret 2, selection 1", m.quick().Input.Line(), m.rail.Cursor())
	}
}

func TestQuickUpDownWalkSoftWrappedRows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	m.openQuickMode()
	m.quick().Input.SetWidth(12)
	m.quick().Input.SetHeight(uiquick.BarMaxRows)
	m.quick().Input.SetValue("one two three four five six seven")
	if height := m.quick().Input.LineInfo().Height; height < 3 {
		t.Fatalf("value should soft-wrap over at least 3 rows, got %d", height)
	}
	last := m.quick().Input.LineInfo().RowOffset
	if height := m.quick().Input.LineInfo().Height; last != height-1 {
		t.Fatalf("the caret starts on row %d of %d, want the last wrapped row", last, height)
	}

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.quick().Input.LineInfo().RowOffset; got != last-1 || m.rail.Cursor() != 1 {
		t.Fatalf("up inside a wrapped line: row %d, selection %d; want row %d, selection 1", got, m.rail.Cursor(), last-1)
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.quick().Input.LineInfo().RowOffset; got != last || m.rail.Cursor() != 1 {
		t.Fatalf("down inside a wrapped line: row %d, selection %d; want row %d, selection 1", got, m.rail.Cursor(), last)
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.rail.Cursor() == 1 {
		t.Fatal("down from the last wrapped row should move the selection")
	}
}

func TestQuickOneRowPromptKeepsArrowsOnTheList(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	m.openQuickMode()
	m.quick().Input.SetWidth(40)
	m.quick().Input.SetValue("short answer")

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.rail.Cursor() != 0 {
		t.Fatalf("up on a one-row prompt should move the selection, got %d", m.rail.Cursor())
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.rail.Cursor() != 1 {
		t.Fatalf("down on a one-row prompt should move the selection, got %d", m.rail.Cursor())
	}
}

// A chip the caret lands inside snaps toward the step's direction, so ↑
// always leaves the row it started on instead of reading as a dead key.
func TestQuickUpStepsOffTheRowWhenAChipIsInTheWay(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	m.openQuickMode()
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/a.png"}}
	m.quick().Input.SetWidth(21)
	m.quick().Input.SetHeight(uiquick.BarMaxRows)
	m.quick().Input.SetValue("aaaa bbbb " + composer.Token(1) + " cccc dddd eeee ffff")
	m.quick().Input.SetCursor(33)

	for !m.quick().CaretOnFirstRow() {
		before := m.quick().Input.LineInfo().RowOffset
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
		if got := m.quick().Input.LineInfo().RowOffset; got >= before {
			t.Fatalf("up left the caret on row %d, want a row above %d", got, before)
		}
		if m.rail.Cursor() != 1 {
			t.Fatal("up inside the prompt should not move the selection")
		}
	}
}

func TestQuickPromptKeepsItsFirstRowAsItWrapsAndShrinks(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		m := buildModel(t)
		// Every key renders the whole screen; a narrow one wraps the bar
		// in fewer keys and paints each frame for less.
		m.layout.width = 60
		seedTwoGroups(t, m)
		setRailCursor(m, 1)
		m.prefs.fullLayout = full
		m.openQuickMode()
		_ = m.View()
		firstRowShown := func() bool {
			return strings.Contains(ansi.Strip(m.View()), "❯ first")
		}
		for _, r := range "first " + strings.Repeat("word ", 200) {
			m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			_, rows := m.quick().DisplayRows()
			if m.quick().Input.Length() > len("first") && rows <= m.quick().MaxRows && !firstRowShown() {
				t.Fatalf("full layout %v: typing to %d rows scrolled the first row away", full, rows)
			}
			// A few rows past the bar is overflow enough; each key past
			// it renders the whole bar again and checks nothing.
			if rows > m.quick().MaxRows+2 {
				break
			}
		}
		if _, rows := m.quick().DisplayRows(); rows <= m.quick().MaxRows {
			t.Fatalf("full layout %v: the prompt should overflow the bar, got %d rows", full, rows)
		}
		for {
			_, rows := m.quick().DisplayRows()
			if rows <= m.quick().MaxRows && !firstRowShown() {
				t.Fatalf("full layout %v: deleting back to %d rows left the first row scrolled away", full, rows)
			}
			if rows == 1 {
				break
			}
			m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
		}
	}
}

func TestQuickPromptModeLeadsTheFooterAndNamesTheOpenBar(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	if leads := m.rowLegend().Leads; !slices.Equal(leads, [][2]string{{"space", "quick prompt mode"}}) {
		t.Fatalf("group row leads = %q, want quick prompt mode", leads)
	}
	footer := m.listFooter()
	if !strings.Contains(footer, uitheme.KeyCapLead("space", "quick prompt mode")) {
		t.Fatalf("footer should carry the filled quick prompt mode binding:\n%q", footer)
	}
	m.services.listKeys = m.services.listKeys.With(keybind.Prompt, bindingOf(t))
	if strings.Contains(m.listFooter(), "quick prompt mode") {
		t.Fatalf("an unbound prompt key should leave the lead out:\n%s", ansi.Strip(m.listFooter()))
	}
	m.services.listKeys = keybind.DefaultList()

	for _, full := range []bool{false, true} {
		m.prefs.fullLayout = full
		m.openQuickMode()
		m.quick().Input.SetValue("hello")
		frame := preparedView(m)
		if !strings.Contains(ansi.Strip(m.viewFooter()), quickModeTitle) {
			t.Fatalf("full layout %v: the open bar's footer should be titled %q:\n%s", full, quickModeTitle, ansi.Strip(m.viewFooter()))
		}
		footer := m.viewFooter()
		frameRows := strings.Split(ansi.Strip(frame), "\n")
		footerRows := strings.Split(ansi.Strip(footer), "\n")
		bottom := frameRows[len(frameRows)-len(footerRows):]
		for i := range bottom {
			bottom[i] = strings.TrimRight(bottom[i], " ")
			footerRows[i] = strings.TrimRight(footerRows[i], " ")
		}
		if !slices.Equal(bottom, footerRows) || !strings.Contains(ansi.Strip(footer), "❯ hello") {
			t.Fatalf("full layout %v: the prompt should sit in the footer:\n%s", full, ansi.Strip(frame))
		}
		rows := strings.Split(footer, "\n")
		keysHeight := lipgloss.Height(paintLegend([]legendSection{{Title: quickModeTitle, Pairs: m.quick().Legend(m.quickBarHost(), m.prefs.glyphMode)}}, m.layout.width-1, m.prefs.glyphMode).Text)
		for i, line := range rows {
			tone := uitheme.QuickModeHex()
			if i > 0 && i < len(rows)-keysHeight {
				tone = uitheme.BlockHex()
			}
			if !strings.HasPrefix(line, uitheme.BgSeq(tone)) || !strings.HasPrefix(ansi.Strip(line), m.chrome().QuickEdgeGlyph) || ansi.StringWidth(line) != m.layout.width {
				t.Fatalf("full layout %v: every bar row should open on the accent edge over the band, across the width: %q", full, line)
			}
		}
		m.closeQuick()
	}
}

func TestQuickPromptTypesWordsNamedLikeKeys(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	m.openQuickMode()
	words := []string{"the", "end", "up", "down", "left", "right", "home", "tab", "enter", "esc", "delete"}
	for i, word := range words {
		if i > 0 {
			m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		}
		m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(word)})
	}
	if want := strings.Join(words, " "); m.quick().Input.Value() != want || (m.quick() == nil) || m.rail.Cursor() != 1 {
		t.Fatalf("prompt %q, open %v, selection %d; want %q typed in an open bar on row 1", m.quick().Input.Value(), (m.quick() != nil), m.rail.Cursor(), want)
	}
}

func TestQuickSendUncertainClearsAcceptedDraftWithoutRetry(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "target", t.TempDir(), "")
	m.selectSessionRow(t, "target")
	m.openQuickMode()
	path := tempImage(t, "uncertain.png")
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: path}}
	m.quick().Input.SetValue(composer.Token(1) + " do this once")
	calls := 0
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		calls++
		return tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, errors.New("reply lost")
	}

	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if calls != 1 || m.quick().Input.Value() != "" {
		t.Fatalf("uncertain send calls=%d draft=%q", calls, m.quick().Input.Value())
	}
	if len(m.quick().Attachments) != 0 {
		t.Fatalf("uncertain send returned images for a possible retry: %+v", m.quick().Attachments)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("uncertain send removed an image the pane may still read: %v", err)
	}
	if !strings.Contains(m.errBar.text, "may have reached") || !strings.Contains(m.errBar.text, "was not sent again") {
		t.Fatalf("uncertain result = %q", m.errBar.text)
	}
	stored, err := m.services.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastPrompt != "" {
		t.Fatalf("uncertain send recorded confirmed prompt %q", stored.LastPrompt)
	}
}

func TestQuickSendDoesNotFabricateMetadataWhenGuardLosesRace(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "target", t.TempDir(), "")
	captured := m.sessionRows()[0]
	if err := m.services.store.SetAcked(captured.ID, true); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == captured.ID {
			m.workspace.sessions[i].Acked = true
		}
	}
	m.selectSessionRow(t, "target")
	m.openQuickMode()
	m.quick().Input.SetValue("race with relaunch")
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		if err := m.services.store.SetAgentLaunchedAt(captured.ID, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
	}

	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.quick().Input.Value() != "" {
		t.Fatal("confirmed delivery left an accepted draft available to resend")
	}
	if !strings.Contains(m.errBar.text, "prompt sent, but recording it for the row failed") {
		t.Fatalf("guard failure = %q", m.errBar.text)
	}
	for _, session := range m.workspace.sessions {
		if session.ID == captured.ID && (!session.Acked || session.LastPrompt != "") {
			t.Fatalf("local projection fabricated rejected metadata: acked=%t prompt=%q", session.Acked, session.LastPrompt)
		}
	}
}

func TestQuickSendStaleRefusalDefersImageCleanup(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	path := tempImage(t, "stale-refusal.png")
	q := pushQuick(m)
	request := uiquicksend.SendRequest{
		ComposerGen: q.Gen + 1,
		Images:      []composer.Attachment{{ID: 1, Path: path}},
	}
	cleanup := m.returnQuickSendImages(request)
	if cleanup == nil {
		t.Fatal("stale composer did not return an asynchronous cleanup command")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Update path removed image before command ran: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup command left image behind: %v", err)
	}
}

func TestBlockedQuickSendsKeepUpdateResponsiveRunFIFOAndDrainOnQuit(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "target", t.TempDir(), "")
	m.selectSessionRow(t, "target")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls []string
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(_ *tmux.Driver, _ string, text string) (tmux.SendResult, error) {
		calls = append(calls, text)
		if len(calls) == 1 {
			close(started)
			<-release
		}
		return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
	}

	m.openQuickMode()
	m.quick().Input.SetValue("first")
	_, first := m.submitQuick()
	if first == nil {
		t.Fatal("first send did not activate its effect")
	}
	if _, duplicate := m.submitQuick(); duplicate != nil || m.errBar.text != "this prompt is already being sent" {
		t.Fatalf("duplicate accepted: cmd=%v err=%q", duplicate != nil, m.errBar.text)
	}
	m.openQuickMode()
	m.quick().Input.SetValue("second")
	if _, second := m.submitQuick(); second != nil || len(m.effects.Main.Pending) != 1 {
		t.Fatalf("second send did not queue behind the first: cmd=%v pending=%d", second != nil, len(m.effects.Main.Pending))
	}

	completed := make(chan tea.Msg, 1)
	go func() { completed <- first() }()
	<-started
	updated, _ := m.Update(struct{}{})
	if updated != m {
		close(release)
		t.Fatal("Update replaced the model while quick transport was blocked")
	}
	if _, quit := m.requestQuit(); quit != nil || !m.effects.Quitting {
		close(release)
		t.Fatal("quit abandoned the accepted quick-send queue")
	}
	close(release)
	msg := <-completed
	updated, _ = m.Update(msg)
	m = updated.(*Model)
	if got := m.quick().Input.Value(); got != "second" {
		t.Fatalf("first completion replaced newer composer: %q", got)
	}
	if m.effects.Main.Active == nil {
		t.Fatal("quit drain did not activate the queued quick send")
	}
	updated, quit := m.Update(m.effects.Main.Active.Command())
	m = updated.(*Model)
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("send order = %q", calls)
	}
	if m.effects.Main.Active != nil || quit == nil {
		t.Fatal("queued quick send did not finish the quit drain")
	}
}

func TestRetainedQuickPromptReadsTheCurrentGlyphMode(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.SetWidth(70)
	m.quick().Input.SetValue("❯↳⚙ literal input")
	for _, tc := range []struct {
		mode presentation.GlyphMode
		mark string
	}{{presentation.GlyphModeBasic, ">"}, {presentation.GlyphModeAdvanced, "❯"}, {presentation.GlyphModeBasic, ">"}} {
		m.prefs.glyphMode = tc.mode
		got := ansi.Strip(m.quick().Input.View())
		if !strings.Contains(got, tc.mark+" ❯↳⚙ literal input") {
			t.Fatalf("retained prompt in %s = %q", tc.mode, got)
		}
	}
}

func TestQuickWorktreeMarkKeepsItsKeyboardAndMouseTarget(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("work", gitTestRepo(t)); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	for _, tc := range []struct {
		mode presentation.GlyphMode
		mark string
	}{{presentation.GlyphModeBasic, "Y"}, {presentation.GlyphModeAdvanced, "⎇"}} {
		m.prefs.glyphMode = tc.mode
		for _, width := range []int{80, 120} {
			m.layout.width, m.layout.height = width, 30
			m.quick().SetWorktree(false)
			applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
			if !m.quickWorktreeOn() {
				t.Fatal("keyboard did not enable worktree")
			}
			frame := ansi.Strip(preparedView(m))
			if !strings.Contains(frame, tc.mark+" on") {
				t.Fatalf("%s/%d worktree mark missing:\n%s", tc.mode, width, frame)
			}
			found := false
			for _, hit := range m.quick().Hits() {
				if hit.Action == uiquick.ClickWorktree {
					found = true
					m = leftClick(m, m.quick().OriginX+hit.X0, m.quick().OriginY+hit.Line)
					break
				}
			}
			if !found || m.quickWorktreeOn() {
				t.Fatalf("%s/%d worktree click missed", tc.mode, width)
			}
		}
	}
}

// ctrl+o in the quick prompt puts a saved prompt at the caret as a chip,
// and the send carries its text where the chip stood.
func TestQuickSavedPromptChipSendsItsText(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Role", " \n instruct\n")
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	var sent string
	m.services.quickSendText = func(_ *tmux.Driver, _ string, text string) (tmux.SendResult, error) {
		sent = text
		return tmux.SendResult{}, nil
	}
	m.quick().Input.SetValue("then ")
	_, load := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	m.runBatch(t, load)
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.quick().Input.Value(); got != "then "+composer.SavedToken("Role")+" " {
		t.Fatalf("chip landed as %q", got)
	}
	if !strings.Contains(ansi.Strip(m.quick().Composer.View(composerPaint())), "[saved prompt:Role]") {
		t.Fatal("the chip is not painted")
	}
	for _, r := range "go" {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if sent != "then  \n instruct\n go" || m.errBar.text != "" {
		t.Fatalf("sent %q, err %q", sent, m.errBar.text)
	}
	if m.quick().Input.Value() != "" || len(m.quick().Saved) != 0 {
		t.Fatal("a confirmed send kept the chip")
	}
}

// pushQuick puts a bare bar on the stack, for a test that needs one open
// without the settings and catalog reads openQuickMode starts.
func pushQuick(m *Model) *uiquick.Surface {
	q := newQuickSurface(m)
	q.Composer = composer.Composer{Input: textarea.New()}
	m.push(q)
	return q
}

// The bar opens by its key, holds its draft through a stray click, and
// closes on esc, letting go of its images.
func TestQuickBarIsASurface(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.applyCmd(t, m.openQuickMode())
	if m.quick() == nil {
		t.Fatalf("the quick bar did not open, err %q", m.errBar.text)
	}
	if mustTop(t, m).Dismissible() {
		t.Fatal("the bar holds a draft and must not close on a click")
	}
	if m.cardOpen() || !m.listClear() {
		t.Fatal("the bar sits on the list; it is no card over it")
	}
	image := tempImage(t, "draft.png")
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quick() != nil || len(m.surfaces) != 0 {
		t.Fatal("esc did not close the bar")
	}
	if imageExists(t, image) {
		t.Fatal("closing the bar kept its image")
	}
}

// A settings read that lands after its bar closed, or on a newer bar, is
// dropped.
func TestStaleQuickCompletionIsDropped(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openQuickMode())
	gen := m.quick().Gen
	m.closeQuick()
	if _, ok := m.quickFor(gen); ok {
		t.Fatal("a closed bar still answers its generation")
	}
	m.applyCmd(t, m.openQuickMode())
	if _, ok := m.quickFor(gen); ok {
		t.Fatal("a newer bar answered an older generation")
	}
}

func TestQuickFooterKeepsTheCaretRowOnScreen(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		m := buildModel(t)
		seedTwoGroups(t, m)
		setRailCursor(m, 1)
		m.prefs.fullLayout = full
		m.layout.width = 56
		m.layout.height = 12
		m.openQuickMode()

		painted := func() string {
			frame := preparedView(m)
			if rows := len(strings.Split(frame, "\n")); rows > m.layout.height {
				t.Fatalf("full layout %v: the frame painted %d rows into a %d row terminal", full, rows, m.layout.height)
			}
			return ansi.Strip(frame)
		}
		painted()
		m = applyMsg(t, m, composer.PasteTextMsg{
			Target: composer.Quick,
			Gen:    m.quick().Gen,
			Inner: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(
				"FIRSTROWMARK " + strings.Repeat("filler word ", 20) + "LASTROWMARK")},
		})
		painted()

		for i := 0; i < 40 && !m.quick().CaretOnFirstRow(); i++ {
			_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
			painted()
		}
		if !m.quick().CaretOnFirstRow() {
			t.Fatal("up never reached the prompt's first row")
		}
		if !strings.Contains(painted(), "FIRSTROWMARK") {
			t.Fatalf("full layout %v: the frame clipped the prompt's first row", full)
		}

		for i := 0; i < 40 && !m.quick().CaretOnLastRow(); i++ {
			_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
			painted()
		}
		if !m.quick().CaretOnLastRow() {
			t.Fatal("down never reached the prompt's last row")
		}
		if !strings.Contains(painted(), "LASTROWMARK") {
			t.Fatalf("full layout %v: the frame clipped the prompt's last row", full)
		}
	}
}

func TestQuickBarMeasuresRowsAtTheWidthItJustSet(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.SetWidth(80)
	m.quick().Input.SetValue("one two three four five six seven eight nine ten")
	bar := m.viewQuickBar(14, uiquick.BarMaxRows)
	if rows := len(uitheme.SplitLines(bar)) - 1; rows < 2 {
		t.Fatalf("rows = %d, want the wrap at width 14, not the previous width", rows)
	}
}

func TestQuickFooterFillsEveryBandCell(t *testing.T) {
	useTrueColor(t)
	for _, full := range []bool{false, true} {
		for _, width := range []int{56, 120, 180} {
			m := buildModel(t)
			seedTwoGroups(t, m)
			setRailCursor(m, 1)
			m.prefs.fullLayout = full
			m.layout.width = width
			answered(m, claudeLike, claudeAnswer)
			m.openQuickMode()
			m.quick().Input.SetValue("hello 界 " + strings.Repeat("wrapping ", 12))
			frame := preparedView(m)
			frameRows := uitheme.SplitLines(frame)
			if len(frameRows) != m.layout.height || strings.TrimSpace(strings.TrimPrefix(ansi.Strip(frameRows[len(frameRows)-1]), m.chrome().QuickEdgeGlyph)) == "" {
				t.Fatalf("full %v width %d footer must end on the last terminal row", full, width)
			}
			backgrounds, _ := cellColors(frame, m.layout.width, m.layout.height)
			footerHeight := lipgloss.Height(m.viewFooter())
			keysHeight := lipgloss.Height(paintLegend([]legendSection{{Title: quickModeTitle, Pairs: m.quick().Legend(m.quickBarHost(), m.prefs.glyphMode)}}, m.layout.width-1, m.prefs.glyphMode).Text)
			if !strings.Contains(ansi.Strip(frameRows[m.layout.height-footerHeight]), "new") {
				t.Fatalf("full %v width %d footer must begin with the target", full, width)
			}
			if m.quick().OriginY != m.layout.height-footerHeight {
				t.Fatalf("target hit row = %d, want %d", m.quick().OriginY, m.layout.height-footerHeight)
			}
			for y := m.layout.height - footerHeight; y < m.layout.height; y++ {
				if !strings.HasPrefix(ansi.Strip(frameRows[y]), m.chrome().QuickEdgeGlyph) {
					t.Fatalf("footer row %d must carry the accent edge", y)
				}
				if cells := ansi.StringWidth(frameRows[y]); cells != m.layout.width {
					t.Fatalf("band row %d width = %d, want %d", y, cells, m.layout.width)
				}
				for x := 0; x < m.layout.width; x++ {
					if y == m.layout.height-keysHeight && x >= quickGutter && x < quickGutter+ansi.StringWidth(uitheme.LegendBadgeStyle().Render(quickModeTitle)) {
						continue
					}
					want := uitheme.QuickModeHex()
					if y > m.layout.height-footerHeight && y < m.layout.height-keysHeight {
						want = uitheme.BlockHex()
					}
					if !sameColor(backgrounds[y][x], hexColor(want)) {
						t.Fatalf("full %v width %d row %d col %d background = %v, want %s", full, width, y, x, backgrounds[y][x], want)
					}
				}
			}
		}
	}
}

// Resizing a pane for prompt growth makes agents redraw their transcripts.
func TestQuickBarKeepsPaneHeight(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "sizer", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	sess := railSelectedSession(m)
	pinned := m.previewPaneHeight()

	rows := make([]string, pinned+10)
	for i := range rows {
		rows[i] = fmt.Sprintf("line%03d", i+1)
	}
	m.workspace.preview = strings.Join(rows, "\n") + "\n"
	listed := paintedPreview(m)

	m.openQuickMode()
	m.quick().Input.SetValue(strings.Repeat("filler word ", 40))
	if got := m.previewPaneHeight(); got != pinned {
		t.Fatalf("box height with the quick bar open = %d, want %d", got, pinned)
	}
	painted := paintedPreview(m)
	// The bar's rows come off the top of the view; the pane's live end stays.
	if oldest := rows[len(rows)-pinned]; !strings.Contains(listed, oldest) || strings.Contains(painted, oldest) {
		t.Fatalf("the quick bar should have cropped %s off the top:\n%s", oldest, painted)
	}
	if newest := rows[len(rows)-1]; !strings.Contains(painted, newest) {
		t.Fatalf("the quick bar hid the pane's live end (%s):\n%s", newest, painted)
	}
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != pinned {
		t.Fatalf("pane height with the quick bar open = %d, want %d", got, pinned)
	}

	m.closeQuick()
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != pinned {
		t.Fatalf("pane height after closing the quick bar = %d, want %d", got, pinned)
	}
}

func paintedPreview(m *Model) string {
	_, rightWidth := m.splitWidths()
	var painted strings.Builder
	for _, row := range m.contentLines(rightWidth, m.listBodyHeight()) {
		painted.WriteString(ansi.Strip(row.Text) + "\n")
	}
	return painted.String()
}

// A prompt the bar just cleared ignores an Enter that lands right after,
// so the key that sent it cannot also close the bar.
func TestClearingQuickPromptDebouncesEmptyEnter(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	q := m.quick()
	q.Input.SetValue("ship it")
	m.clearAccepted(q.Gen, "ship it", false)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if q, ok := surfaceOf[*uiquick.Surface](m); !ok || q.Input.Value() != "" {
		t.Fatal("Enter just after clearing should leave the empty prompt open")
	}
}
