package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// pasteQuickImage runs a full ctrl+v paste against a fake clipboard that
// yields the given file, and returns the id of the chip it left behind.
func pasteQuickImage(t *testing.T, m *Model, path string) int {
	t.Helper()
	orig := captureClipboardImage
	defer func() { captureClipboardImage = orig }()
	captureClipboardImage = func() (string, error) { return path, nil }

	_, cmd := m.handleQuickKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+v should start an async clipboard read")
	}
	id := m.quick.lastImageID
	msg, ok := cmd().(pasteImageMsg)
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
	idx := strings.Index(m.quick.input.Value(), needle)
	if idx < 0 {
		t.Fatalf("%q not in prompt %q", needle, m.quick.input.Value())
	}
	return utf8.RuneCountInString(m.quick.input.Value()[:idx])
}

func TestQuickPromptDeadSessionSetsError(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "gone", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "gone")

	m.openQuickMode()
	m.quick.input.SetValue("hello?")
	if _, _ = m.submitQuick(); m.errBar.text != "session is dead - press v to revive or R to restart" {
		t.Fatalf("err = %q", m.errBar.text)
	}
	if !m.quick.active {
		t.Fatal("quick mode should stay open after a failed send")
	}
	if _, err := m.store.Get(sess.ID); err != nil {
		t.Fatalf("session record should survive: %v", err)
	}
}

func TestQuickPromptSendClearsAcked(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.selectSessionRow(t, "answer-me")

	m.openQuickMode()
	if !m.quick.active {
		t.Fatalf("quick mode should activate, err = %q", m.errBar.text)
	}
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	if !m.quick.active {
		t.Fatal("quick mode should stay active after a send")
	}
	if m.quick.input.Value() != "" {
		t.Fatal("input should clear after a send")
	}
	got, err := m.store.Get(sess.ID)
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

	m.quick.input.SetValue("fix header and footer")
	m.quick.input.SetCursorColumn(len("fix header"))
	path := tempImage(t, "paste-test.png")
	id := pasteQuickImage(t, m, path)

	want := "fix header " + imageToken(id) + " and footer"
	if got := m.quick.input.Value(); got != want {
		t.Fatalf("chip should land at the caret: got %q, want %q", got, want)
	}
	if len(m.quick.attachments) != 1 || m.quick.attachments[0].path != path {
		t.Fatalf("attachment = %+v", m.quick.attachments)
	}
	// Typing continues after the chip, leaving the text around it intact.
	for _, r := range " now" {
		_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.quick.input.Value(); got != "fix header "+imageToken(id)+" now and footer" {
		t.Fatalf("typing after the chip disturbed the text: %q", got)
	}
}

func TestQuickChipRendersInlineInTheInput(t *testing.T) {

	m := buildModel(t)
	m.openQuickMode()
	m.quick.attachments = []imageAttachment{{id: 1, path: "/tmp/agent-manager-pastes/paste-123.png"}}
	m.quick.input.SetValue("look at " + imageToken(1) + " please")
	m.quick.input.SetWidth(60)

	view := m.quick.renderChips(m.quick.input.View())
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "look at "+imageToken(1)+" please") {
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
	if lipgloss.Width(ansi.Strip(imageChip(imageToken(1)))) != lipgloss.Width(imageToken(1)) {
		t.Fatal("styled chip must keep the token's width")
	}

	m.quick.attachments = []imageAttachment{{id: 2}}
	m.quick.input.SetValue(imageToken(2))
	pending := m.quick.renderChips(m.quick.input.View())
	if !strings.Contains(pending, imageChipPasting(imageToken(2))) {
		t.Fatal("a chip waiting on the clipboard should wear the pasting style")
	}
}

func TestQuickMessageSubstitutesPathsInPlace(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	m.quick.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}, {id: 2, path: "/tmp/b.png"}}
	m.quick.input.SetValue("compare " + imageToken(1) + " with " + imageToken(2))
	if got := m.quick.message(); got != "compare /tmp/a.png with /tmp/b.png" {
		t.Fatalf("compose = %q", got)
	}

	m.quick.input.SetValue(imageToken(2) + " " + imageToken(1))
	if got := m.quick.message(); got != "/tmp/b.png /tmp/a.png" {
		t.Fatalf("paths should follow the order they were pasted in: %q", got)
	}
}

func TestQuickBackspaceDeletesTheWholeChipAtTheCaret(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	first := tempImage(t, "first.png")
	second := tempImage(t, "second.png")
	m.quick.lastImageID = 2
	m.quick.attachments = []imageAttachment{{id: 1, path: first}, {id: 2, path: second}}
	m.quick.input.SetValue("this " + imageToken(1) + " and that " + imageToken(2))
	m.quick.input.SetCursorColumn(quickOffsetOf(t, m, imageToken(1)) + utf8.RuneCountInString(imageToken(1)))

	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.quick.input.Value(); got != "this  and that "+imageToken(2) {
		t.Fatalf("backspace beside a chip should remove it whole, got %q", got)
	}
	if len(m.quick.attachments) != 1 || m.quick.attachments[0].id != 2 {
		t.Fatalf("the other chip should stay: %+v", m.quick.attachments)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("deleted chip's temp file should be removed, err=%v", err)
	}
	if m.quick.message() != "this  and that "+second {
		t.Fatalf("the surviving chip should still send its path: %q", m.quick.message())
	}

	// Away from a chip, backspace is ordinary text editing.
	m.quick.input.CursorEnd()
	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m.quick.attachments) != 1 {
		t.Fatalf("plain backspace should leave chips alone: %+v", m.quick.attachments)
	}
}

func TestQuickDeleteRemovesTheChipInFront(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "forward.png")
	m.quick.lastImageID = 1
	m.quick.attachments = []imageAttachment{{id: 1, path: path}}
	m.quick.input.SetValue("head " + imageToken(1) + " tail")
	m.quick.input.SetCursorColumn(quickOffsetOf(t, m, imageToken(1)))

	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	if got := m.quick.input.Value(); got != "head  tail" {
		t.Fatalf("delete in front of a chip should remove it whole, got %q", got)
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("attachment should be released: %+v", m.quick.attachments)
	}
}

func TestQuickArrowsStepOverAChip(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.lastImageID = 1
	m.quick.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}
	m.quick.input.SetValue("a " + imageToken(1) + " b")
	start := quickOffsetOf(t, m, imageToken(1))
	end := start + utf8.RuneCountInString(imageToken(1))
	m.quick.input.SetCursorColumn(end)

	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := m.quick.cursorOffset(); got != start {
		t.Fatalf("left should clear the chip in one step: offset %d, want %d", got, start)
	}
	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.quick.cursorOffset(); got != end {
		t.Fatalf("right should clear the chip in one step: offset %d, want %d", got, end)
	}
}

func TestQuickChipsReflowAcrossWrappedRows(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}
	m.quick.input.SetWidth(24)
	m.quick.input.SetHeight(quickBarMaxRows)
	m.quick.input.SetValue(strings.Repeat("word ", 5) + imageToken(1) + " tail")

	rendered := ansi.Strip(m.quick.renderChips(m.quick.input.View()))
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) > 24 {
			t.Fatalf("wrapped row overflows the bar: %q", line)
		}
	}
	if !strings.Contains(rendered, imageToken(1)) {
		t.Fatalf("chip should survive wrapping, got %q", rendered)
	}
}

func TestQuickBulkDeleteReleasesTheImage(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "orphan.png")
	m.quick.lastImageID = 1
	m.quick.attachments = []imageAttachment{{id: 1, path: path}}
	m.quick.input.SetValue("note " + imageToken(1))
	m.quick.input.CursorEnd()

	// ctrl+u wipes the line without going through the chip-aware keys.
	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if len(m.quick.attachments) != 0 {
		t.Fatalf("a chip cut by a bulk delete should release its image: %+v", m.quick.attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("orphaned temp file should be removed, err=%v", err)
	}
}

func TestQuickPasteReservesTheChipUntilTheReadLands(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	_, cmd := m.handleQuickKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+v should start an async clipboard read")
	}
	id := m.quick.lastImageID
	if !strings.Contains(m.quick.input.Value(), imageToken(id)) {
		t.Fatalf("the chip should hold its spot while the read runs, got %q", m.quick.input.Value())
	}
	if !m.quick.pasting() {
		t.Fatal("the reserved chip should read as pasting")
	}
	// The caret sits after the chip, so typing keeps flowing.
	for _, r := range "later" {
		_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.quick.input.Value(); got != imageToken(id)+" later" {
		t.Fatalf("typing during a paste = %q", got)
	}
	_, _ = m.submitQuick()
	if m.errBar.text == "" {
		t.Fatal("submitting mid-paste should say the image is not ready")
	}

	path := tempImage(t, "landed.png")
	updated, _ := m.Update(pasteImageMsg{target: composerQuick, gen: m.quick.gen, id: id, path: path})
	m = updated.(*Model)
	if m.quick.pasting() {
		t.Fatal("the chip should settle once the read lands")
	}
	if got := m.quick.input.Value(); got != imageToken(id)+" later" {
		t.Fatalf("the chip should resolve in place, got %q", got)
	}
	if got := m.quick.message(); got != path+" later" {
		t.Fatalf("compose after the read = %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "png-bytes" {
		t.Fatalf("attachment file: %v %q", err, data)
	}
}

func TestQuickChipRemovalKeepsAMultiLinePrompt(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.lastImageID = 1
	m.quick.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}
	m.quick.input.SetValue("first line\nsecond " + imageToken(1) + " line\nthird line")
	m.quick.input.CursorUp()
	m.quick.input.SetCursorColumn(len("second ") + utf8.RuneCountInString(imageToken(1)))
	if m.quick.input.Line() != 1 {
		t.Fatalf("test setup: caret on row %d", m.quick.input.Line())
	}

	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.quick.input.Value(); got != "first line\nsecond  line\nthird line" {
		t.Fatalf("removing a chip should leave the other rows alone, got %q", got)
	}
	if m.quick.input.Line() != 1 || m.quick.cursorOffset() != len("first line\nsecond ") {
		t.Fatalf("caret should stay where the chip was: row %d offset %d", m.quick.input.Line(), m.quick.cursorOffset())
	}
}

func TestQuickPasteRefusedWhenThePromptIsFull(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetValue(strings.Repeat("x", m.quick.input.CharLimit))
	m.quick.input.CursorEnd()

	_, cmd := m.handleQuickKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("a full prompt should not start a clipboard read")
	}
	if m.errBar.text == "" {
		t.Fatal("a refused paste should say why")
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("no chip should be reserved: %+v", m.quick.attachments)
	}
	if strings.Contains(m.quick.input.Value(), "[Image") {
		t.Fatal("a truncated token must never reach the prompt")
	}
}

func TestQuickEscReleasesPastedImages(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "abandoned.png")
	m.quick.attachments = []imageAttachment{{id: 1, path: path}}
	m.quick.input.SetValue("never mind " + imageToken(1))

	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(m.quick.attachments) != 0 {
		t.Fatalf("closing the bar should release its images: %+v", m.quick.attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("abandoned temp file should be removed, err=%v", err)
	}
}

func TestQuickImageMsgNoImageReachesTextPaste(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick.input.SetValue("keep this")
	m.quick.input.CursorEnd()

	_, _ = m.handleQuickKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	id := m.quick.lastImageID
	updated, cmd := m.Update(pasteImageMsg{target: composerQuick, gen: m.quick.gen, id: id, noImage: true})
	m = updated.(*Model)
	if m.quick.pasting() || len(m.quick.attachments) != 0 {
		t.Fatalf("a clipboard with no image should take the chip back out: %+v", m.quick.attachments)
	}
	if got := m.quick.input.Value(); got != "keep this" {
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
	m.quick.input.SetValue("look")
	m.quick.input.CursorEnd()

	orig := captureClipboardImage
	defer func() { captureClipboardImage = orig }()
	captureClipboardImage = func() (string, error) {
		return "", errors.New("install wl-clipboard or xclip to paste images")
	}

	_, cmd := m.handleQuickKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	msg := cmd().(pasteImageMsg)
	updated, _ := m.Update(msg)
	m = updated.(*Model)
	if m.errBar.text == "" {
		t.Fatal("a real clipboard error should surface through m.errBar.text")
	}
	if m.quick.pasting() || len(m.quick.attachments) != 0 {
		t.Fatalf("a failed read should take the chip back out: %+v", m.quick.attachments)
	}
	if got := m.quick.input.Value(); got != "look" {
		t.Fatalf("a failed read should leave the typed text, got %q", got)
	}
}

func TestQuickSpawnOnGroupCreatesSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.store.SetSetting("default_tool", "claude"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}

	m.openQuickMode()
	m.quick.input.SetValue("build the api")
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
	if !m.tmux.Exists(spawned.ID) {
		t.Fatal("tmux session should exist after quick spawn")
	}
	if m.quick.input.Value() != "" {
		t.Fatal("input should clear after a spawn")
	}
}

func TestQuickSpawnUsesTabCycledTool(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}

	m.openQuickMode()
	if m.quickTool() != "claude" {
		t.Fatalf("quick tool starts at %q want claude", m.quickTool())
	}
	if _, _ = m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyTab}); m.quickTool() != "claude-hooked" {
		t.Fatalf("after tab, quick tool = %q want claude-hooked", m.quickTool())
	}

	m.quick.input.SetValue("build the api")
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

func TestQuickCloseAfterSendDefaultsToStayingOpen(t *testing.T) {
	m := buildModel(t)
	if m.quickCloseAfterSend() {
		t.Fatal("quick bar should stay open by default")
	}
	if err := m.store.SetSetting(quickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}
	if !m.quickCloseAfterSend() {
		t.Fatal("stored close choice should opt in")
	}
}

func TestQuickPromptClosesAfterSendWhenEnabled(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	if err := m.store.SetSetting(quickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}

	m.openQuickMode()
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	if m.quick.active {
		t.Fatal("quick mode should close after a send when the setting is on")
	}
}

func TestQuickWorktreeToggle(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	if m.quick.worktree {
		t.Fatal("worktree should default off")
	}
	m.handleQuickKey(tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	if !m.quick.worktree {
		t.Fatal("alt+w should toggle worktree on")
	}
	m.handleQuickKey(tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	if m.quick.worktree {
		t.Fatal("alt+w should toggle worktree back off")
	}
}

func TestQuickWorktreeSeedsFromSetting(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openQuickMode()
	if !m.quick.worktree {
		t.Fatal("quick bar should seed worktree on from setting")
	}
}

// quickGroupModel selects a quick-bar target group pointing at dir.
func quickGroupModel(t *testing.T, dir string) *Model {
	t.Helper()
	m := buildModel(t)
	if err := m.store.CreateGroup("grp", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	return m
}

func TestQuickWorktreeGatedInNonRepoGroup(t *testing.T) {
	dir := t.TempDir()
	m := quickGroupModel(t, dir)
	if err := m.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	if m.quickWorktreeOn() {
		t.Fatal("a non-repo group dir cannot host a worktree, even with the group default on")
	}
	m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.quickWorktreeOn() {
		t.Fatal("shift+tab must not turn worktree on for a non-repo dir")
	}
	if !strings.Contains(m.errBar.text, "need a git repository") {
		t.Fatalf("refused toggle should say why, got %q", m.errBar.text)
	}
	if hint := m.viewFooter(); !strings.Contains(hint, worktreeUnavailable) {
		t.Fatalf("footer should mark worktree unavailable, got %q", hint)
	}
	if bar := m.viewQuickBar(120); !strings.Contains(bar, "worktree "+worktreeUnavailable) {
		t.Fatalf("quick bar should name worktree as what is unavailable, got %q", bar)
	}
	m.quick.input.SetValue("do a thing")
	m.submitQuick()
	sessions, err := m.store.ListSessions(true)
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
	if err := m.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	if !m.quickWorktreeOn() {
		t.Fatal("quick bar should show the group's worktree default on")
	}
	m.quick.input.SetValue("do a thing")
	m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("worktree spawn should succeed: %q", m.errBar.text)
	}
	sessions, err := m.store.ListSessions(true)
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
	if err := m.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	m.handleQuickKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.quickWorktreeOn() {
		t.Fatal("shift+tab should override the group default off")
	}
	m.quick.input.SetValue("do a thing")
	m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("plain spawn should succeed: %q", m.errBar.text)
	}
}

// The quick prompt is where people type sentences at an agent. SendText
// pastes and presses Enter, so on a shell row that sentence would run as a
// command: the target's tool is checked before anything is delivered.
func TestQuickPromptRefusesAShell(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	m.openQuickMode()
	m.quick.input.SetValue("summarise what you just did")
	if _, _ = m.submitQuick(); m.errBar.text != shellPromptHint(sess.Name) {
		t.Fatalf("err = %q, want the shell refusal", m.errBar.text)
	}
	if m.quick.input.Value() == "" {
		t.Fatal("a refused prompt should stay in the bar, not vanish")
	}
}

// The same guard from the other end: what the user typed never reaches the
// shell, so it never runs.
func TestQuickPromptNeverRunsWhatIsTypedAtAShell(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	marker := filepath.Join(t.TempDir(), "executed")
	m.openQuickMode()
	m.quick.input.SetValue("touch " + marker)
	m.submitQuick()

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
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	got, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastPrompt != "carry on with the plan" {
		t.Fatalf("last prompt = %q, want the quick send", got.LastPrompt)
	}
}
