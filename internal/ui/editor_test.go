package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// captureEditor swaps both editor seams: PATH answers only for the names
// given, and the launch is recorded instead of run.
func captureEditor(t *testing.T, installed ...string) *[]string {
	t.Helper()
	var launched []string
	prevLook, prevStart := lookPath, startEditor
	lookPath = func(name string) (string, error) {
		if slices.Contains(installed, name) {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	startEditor = func(cmd *exec.Cmd) error {
		launched = cmd.Args
		return nil
	}
	t.Cleanup(func() { lookPath, startEditor = prevLook, prevStart })
	for _, key := range []string{"AGENT_MANAGER_EDITOR", "VISUAL", "EDITOR"} {
		t.Setenv(key, "")
	}
	return &launched
}

func TestOpenEditorLaunchesGUIEditorOnSessionDirectory(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	// The launch belongs to a command, not to Update, so the status line
	// only names the editor once that command has run.
	_, cmd := m.openEditor()
	if cmd == nil {
		t.Fatalf("openEditor returned no command, err = %q", m.errBar.text)
	}
	if len(*launched) != 0 {
		t.Fatalf("the editor started on the update path: %v", *launched)
	}
	m.applyCmd(t, cmd)

	// The live pane answers with the directory tmux resolved, which on
	// macOS is the target of the /var symlink the temp dir sits behind.
	want := []string{"code", resolved(t, dir)}
	if !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
	if !strings.Contains(m.errBar.text, "code") {
		t.Fatalf("status line should name the editor, got %q", m.errBar.text)
	}
}

// A configured editor outranks anything found on PATH, and an argument
// carrying a space stays one argument without a shell to group it.
func TestOpenEditorPrefersConfiguredCommand(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	m.cfg.Editor = `open -a 'Visual Studio Code'`
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	_, cmd := m.openEditor()
	m.applyCmd(t, cmd)

	want := []string{"open", "-a", "Visual Studio Code", dir}
	if !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
}

// The line is argv, not a script: a repo that sets EDITOR in an .envrc gets
// no shell to write into, so the operators stay literal text.
func TestEditorLineIsNeverHandedToAShell(t *testing.T) {
	cmd, ok := editorCommand(`code; touch /tmp/pwned`, "/repo")
	if !ok {
		t.Fatal("editorCommand refused a usable line")
	}
	want := []string{"code;", "touch", "/tmp/pwned", "/repo"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("argv = %v, want %v", cmd.Args, want)
	}
}

func TestSplitEditorLineGroupsOnQuotes(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"code", []string{"code"}},
		{"code -n", []string{"code", "-n"}},
		{`open -a "Visual Studio Code"`, []string{"open", "-a", "Visual Studio Code"}},
		{`'/Applications/My App/bin/edit' -w`, []string{"/Applications/My App/bin/edit", "-w"}},
		{"   ", nil},
		{"", nil},
	} {
		if got := splitEditorLine(tc.line); !slices.Equal(got, tc.want) {
			t.Errorf("splitEditorLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// A dead pane no longer answers for its directory, so the recorded one
// is what opens.
func TestOpenEditorFallsBackToRecordedCwd(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")
	entry, ok := m.selectedRow()
	if !ok {
		t.Fatal("no selected row")
	}
	if err := m.tmux.Kill(entry.sess.ID); err != nil {
		t.Fatalf("kill session: %v", err)
	}

	_, cmd := m.openEditor()
	m.applyCmd(t, cmd)

	want := []string{"code", entry.sess.Cwd}
	if !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
}

func TestReviewOpensCurrentFileInEditor(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := gitTestRepo(t)
	openReviewOn(t, m, "opener", dir)
	fd := m.currentFileDiff()
	if fd == nil {
		t.Fatal("review has no selected file")
	}
	want := filepath.Join(m.diff.set.Repo.Root, fd.File.Path)
	_, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatalf("o returned no command, err = %q", m.errBar.text)
	}
	m.applyCmd(t, m.stepCmd(t, cmd))
	if len(*launched) == 0 {
		t.Fatal("the editor never launched")
	}
	opened := (*launched)[len(*launched)-1]
	if opened != want {
		t.Fatalf("opened %q, want %q", opened, want)
	}
}

func TestReviewRefusesToOpenAFileThatIsGone(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	openReviewOn(t, m, "opener", gitTestRepo(t))
	fd := m.currentFileDiff()
	if fd == nil {
		t.Fatal("review has no selected file")
	}
	path := filepath.Join(m.diff.set.Repo.Root, fd.File.Path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.openDiffFile()
	if next := m.stepCmd(t, cmd); next != nil {
		t.Fatal("a vanished file should not launch an editor")
	}
	if len(*launched) != 0 {
		t.Fatalf("editor launched for a vanished file: %v", *launched)
	}
	if want := "file no longer exists: " + path; m.errBar.text != want {
		t.Fatalf("status line = %q, want %q", m.errBar.text, want)
	}
}

func TestReviewReportsFileCheckErrors(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	openReviewOn(t, m, "opener", gitTestRepo(t))
	fd := m.currentFileDiff()
	if fd == nil {
		t.Fatal("review has no selected file")
	}
	path := filepath.Join(m.diff.set.Repo.Root, fd.File.Path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// A relative target resolves beside the link, so its own base name is
	// what makes it point at itself whatever directory the file sits in.
	if err := os.Symlink(filepath.Base(path), path); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.openDiffFile()
	if next := m.stepCmd(t, cmd); next != nil {
		t.Fatal("a file with a failed stat should not launch an editor")
	}
	if len(*launched) != 0 {
		t.Fatalf("editor launched for a file that failed its check: %v", *launched)
	}
	wantPrefix := "checking file " + path + ": "
	if !strings.HasPrefix(m.errBar.text, wantPrefix) {
		t.Fatalf("status line = %q, want prefix %q", m.errBar.text, wantPrefix)
	}
}

// Each boundary of the resolution order, with both neighbours present and
// the higher one expected.
func TestResolveEditorPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured string
		env        map[string]string
		installed  []string
		want       string
	}{
		{"config over environment", "cfg-edit", map[string]string{"AGENT_MANAGER_EDITOR": "env-edit"}, []string{"code"}, "cfg-edit"},
		{"environment over PATH", "", map[string]string{"AGENT_MANAGER_EDITOR": "env-edit"}, []string{"code"}, "env-edit"},
		{"first GUI editor on PATH wins", "", nil, []string{"zed", "cursor"}, "cursor"},
		{"PATH over $VISUAL", "", map[string]string{"VISUAL": "vim"}, []string{"code"}, "code"},
		{"$VISUAL over $EDITOR", "", map[string]string{"VISUAL": "vim", "EDITOR": "nano"}, nil, "vim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{cfg: config.Config{Editor: tc.configured}}
			captureEditor(t, tc.installed...)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := m.resolveEditor(); got != tc.want {
				t.Fatalf("resolveEditor() = %q, want %q", got, tc.want)
			}
		})
	}
}

// $EDITOR is usually the editor set for git, so it only decides when this
// machine has no GUI editor at all - and a terminal editor takes the screen
// rather than being started where it cannot draw.
func TestResolveEditorFallsBackToEnvironment(t *testing.T) {
	m := buildModel(t)
	captureEditor(t)
	t.Setenv("EDITOR", "nvim")

	if got := m.resolveEditor(); got != "nvim" {
		t.Fatalf("resolveEditor() = %q, want nvim", got)
	}
	if detachedEditors[editorName("nvim")] {
		t.Fatal("nvim draws in this terminal and must not start detached")
	}
}

// An editor nobody listed is handed the screen: wrong for a windowed one
// costs a repaint, wrong for a terminal one loses the editor entirely.
func TestUnknownEditorTakesTheScreen(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t)
	m.cfg.Editor = "my-own-edit-wrapper"
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	if _, cmd := m.openEditor(); cmd == nil {
		t.Fatal("an unknown editor should run through ExecProcess")
	}
	if len(*launched) != 0 {
		t.Fatalf("an unknown editor must not start detached, got %v", *launched)
	}
}

func TestOpenEditorWithoutAnyEditorExplainsItself(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t)
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	m.openEditor()

	if len(*launched) != 0 {
		t.Fatalf("nothing should launch without an editor, got %v", *launched)
	}
	if !strings.Contains(m.errBar.text, "config.toml") {
		t.Fatalf("status line should point at the setting, got %q", m.errBar.text)
	}
}

// A directory removed under a session is named in the refusal, rather than
// leaving the status line trailing off after a colon.
func TestOpenEditorNamesADirectoryThatIsGone(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	gone := t.TempDir()
	createSession(t, m, "agent", gone, "")
	m.selectSessionRow(t, "agent")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	m.openEditor()

	if len(*launched) != 0 {
		t.Fatalf("nothing should launch for a directory that is gone, got %v", *launched)
	}
	if !strings.HasSuffix(m.errBar.text, gone) {
		t.Fatalf("status line should name the directory, got %q", m.errBar.text)
	}
}

// F3 inside an attach leaves a request behind and detaches: the manager
// opens the editor for the session it was in, then hands that session its
// client back.
func TestAttachDoneOpensEditorAndReturnsToTheSession(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "editme", dir, "")
	m.selectSessionRow(t, "editme")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m)

	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, cmd := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("the request produced no launch, err = %q", m.errBar.text)
	}
	if m.editorReturnID != sess.ID {
		t.Fatalf("return armed for %q, want %q", m.editorReturnID, sess.ID)
	}
	request, err := m.tmux.PendingRequest()
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("carrying the request out should consume it, got %q", request)
	}

	done, isDone := cmd().(editorDoneMsg)
	if !isDone || done.err != nil {
		t.Fatalf("editor launch reported %#v", done)
	}
	if want := []string{"code", resolved(t, dir)}; !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}

	updated, cmd = m.Update(done)
	*m = *updated.(*Model)
	if m.editorReturnID != "" {
		t.Fatalf("the return should be consumed, still holds %q", m.editorReturnID)
	}
	if cmd == nil {
		t.Fatal("the session should get its client back")
	}
	prepared, isPrepared := cmd().(reattachPreparedMsg)
	if !isPrepared || prepared.sessID != sess.ID {
		t.Fatalf("want a reattach for %q, got %#v", sess.ID, prepared)
	}
}

// A request the manager cannot carry out leaves nothing armed: the next
// editor opened from the list must not drag the session back on screen.
func TestAttachDoneRefusedEditorArmsNoReturn(t *testing.T) {
	m := buildModel(t)
	captureEditor(t)
	createSession(t, m, "editme", t.TempDir(), "")
	m.selectSessionRow(t, "editme")
	sess := m.sessionRows()[0]
	clearRequestOnCleanup(t, m)

	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, cmd := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatal("a refused request should return no command")
	}
	if m.editorReturnID != "" {
		t.Fatalf("nothing launched, so nothing to return from, got %q", m.editorReturnID)
	}
	if !strings.Contains(m.errBar.text, "no editor found") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}

// An editor that draws in the terminal takes the screen and hands it back
// on exit, so the request arms the return the same way a windowed one does.
func TestAttachDoneTerminalEditorArmsTheReturn(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t)
	m.cfg.Editor = "my-own-edit-wrapper"
	createSession(t, m, "editme", t.TempDir(), "")
	m.selectSessionRow(t, "editme")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m)

	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, cmd := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("the request produced no launch, err = %q", m.errBar.text)
	}
	if len(*launched) != 0 {
		t.Fatalf("a terminal editor must not start detached, got %v", *launched)
	}
	if m.editorReturnID != sess.ID {
		t.Fatalf("return armed for %q, want %q", m.editorReturnID, sess.ID)
	}
}

// An editor that failed to start keeps the list: going back into the
// session would hide the only account of what went wrong.
func TestEditorFailureKeepsTheListAndItsReason(t *testing.T) {
	m := buildModel(t)
	captureEditor(t, "code")
	createSession(t, m, "editme", t.TempDir(), "")
	m.selectSessionRow(t, "editme")
	m.editorReturnID = m.sessionRows()[0].ID

	updated, cmd := m.Update(editorDoneMsg{err: errors.New("exec: \"code\": file does not exist")})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatal("a failed editor should not hand the session back its client")
	}
	if m.editorReturnID != "" {
		t.Fatalf("the return should be dropped, still holds %q", m.editorReturnID)
	}
	if !strings.Contains(m.errBar.text, "does not exist") {
		t.Fatalf("status line should carry the failure, got %q", m.errBar.text)
	}
}

// The cursor is not where the request came from: a poll handled ahead of
// the attach's own message can move it. The request follows the session
// that detached, and refuses rather than acting on another one.
func TestAttachDoneEditorFollowsTheSessionThatDetached(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	attachedDir := t.TempDir()
	createSession(t, m, "attached", attachedDir, "")
	createSession(t, m, "elsewhere", t.TempDir(), "")
	attached := m.sessionRows()[0]
	if attached.Name != "attached" {
		t.Fatalf("first row is %q", attached.Name)
	}
	m.selectSessionRow(t, "elsewhere")
	clearRequestOnCleanup(t, m)

	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, cmd := m.Update(attachDoneMsg{sessID: attached.ID})
	*m = *updated.(*Model)
	m.applyCmd(t, cmd)

	if want := []string{"code", resolved(t, attachedDir)}; !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want the attached session's directory %v", *launched, want)
	}

	// A session that left the list takes its request with it.
	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	*launched = nil
	updated, cmd = m.Update(attachDoneMsg{sessID: "gone"})
	*m = *updated.(*Model)
	if cmd != nil || len(*launched) != 0 {
		t.Fatalf("a session that is gone should launch nothing, got %v", *launched)
	}
	if !strings.Contains(m.errBar.text, "left the list") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}
