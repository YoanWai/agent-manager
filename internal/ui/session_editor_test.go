package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

func resolveEditorLookup(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	updated, next := m.Update(cmd())
	*m = *updated.(*Model)
	return m.foregroundTestCmd(t, next)
}

func applyEditorLookup(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if launch := resolveEditorLookup(t, m, cmd); launch != nil {
		m.applyCmd(t, launch)
	}
}

func TestEditorDirectoryCommandResolvesExecutableOffUpdate(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")
	launched := captureEditor(t, "code")
	lookupCalls := 0
	resolvedLookup := lookPath
	lookPath = func(name string) (string, error) {
		lookupCalls++
		return resolvedLookup(name)
	}

	_, cmd := m.openEditor()
	msg := cmd()
	if lookupCalls == 0 {
		t.Fatal("directory command returned before resolving the editor executable")
	}
	prepared, ok := msg.(terminalDirectoryMsg)
	if !ok || prepared.editor.command == nil {
		t.Fatalf("directory command did not prepare the editor process: %#v", msg)
	}
	lookPath = func(string) (string, error) {
		t.Fatal("Update performed executable resolution")
		return "", errors.New("unreachable")
	}
	updated, launch := m.Update(msg)
	*m = *updated.(*Model)
	m.applyCmd(t, m.foregroundTestCmd(t, launch))
	if want := []string{"code", dir}; !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
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
	applyEditorLookup(t, m, cmd)

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

func TestEditorDirectoryCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")
	reader := &blockedTerminalDirectoryReader{
		started: make(chan struct{}), release: make(chan struct{}), dir: dir,
	}
	_, cmd := m.openEditorWithReader(reader)
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-reader.started
	m.openForm()
	close(reader.release)
	updated, next := m.Update(<-completed)
	m = updated.(*Model)
	if next != nil {
		m.applyTestMsg(t, next())
	}
	if len(*launched) != 0 {
		t.Fatalf("stale directory result launched an editor over the newer form: %v", *launched)
	}
	if m.mode != modeForm {
		t.Fatalf("stale editor result replaced the newer dialog: mode=%v", m.mode)
	}
}

// The editor Settings holds outranks anything found on PATH, and an argument
// carrying a space stays one argument without a shell to group it.
func TestOpenEditorPrefersConfiguredCommand(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	m.services.editor = `open -a 'Visual Studio Code'`
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	_, cmd := m.openEditor()
	applyEditorLookup(t, m, cmd)

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
	if err := m.services.tmux.Kill(entry.sess.ID); err != nil {
		t.Fatalf("kill session: %v", err)
	}

	_, cmd := m.openEditor()
	applyEditorLookup(t, m, cmd)

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
	want := filepath.Join(m.review.Snapshot().Set.Repo.Root, fd.File.Path)
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

func TestReviewFileCommandResolvesExecutableOffUpdate(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	openReviewOn(t, m, "opener", gitTestRepo(t))
	lookupCalls := 0
	resolvedLookup := lookPath
	lookPath = func(name string) (string, error) {
		lookupCalls++
		return resolvedLookup(name)
	}

	_, cmd := m.openDiffFile()
	msg := cmd()
	if lookupCalls == 0 {
		t.Fatal("file command returned before resolving the editor executable")
	}
	prepared, ok := msg.(editorFileCheckedMsg)
	if !ok || prepared.editor.command == nil {
		t.Fatalf("file command did not prepare the editor process: %#v", msg)
	}
	lookPath = func(string) (string, error) {
		t.Fatal("Update performed executable resolution")
		return "", errors.New("unreachable")
	}
	updated, launch := m.Update(msg)
	*m = *updated.(*Model)
	m.applyCmd(t, m.foregroundTestCmd(t, launch))
	if len(*launched) == 0 {
		t.Fatal("the editor never launched")
	}
}

func TestReviewEditorCompletionCannotCrossHelpOrQuit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{
			name: "help",
			mutate: func(m *Model) {
				updated, _ := m.handleKey(key("?"))
				*m = *updated.(*Model)
			},
		},
		{
			name: "quit",
			mutate: func(m *Model) {
				_, _ = m.requestQuit()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			launched := captureEditor(t, "code")
			openReviewOn(t, m, "opener", gitTestRepo(t))
			started := make(chan struct{})
			release := make(chan struct{})
			resolvedLookup := lookPath
			lookPath = func(name string) (string, error) {
				close(started)
				<-release
				return resolvedLookup(name)
			}

			_, cmd := m.openDiffFile()
			completed := make(chan tea.Msg, 1)
			go func() { completed <- cmd() }()
			<-started
			tc.mutate(m)
			close(release)

			updated, next := m.Update(<-completed)
			*m = *updated.(*Model)
			if next != nil {
				m.applyTestMsg(t, next())
			}
			if len(*launched) != 0 {
				t.Fatalf("stale editor completion launched through %s: %v", tc.name, *launched)
			}
		})
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
	path := filepath.Join(m.review.Snapshot().Set.Repo.Root, fd.File.Path)
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
	path := filepath.Join(m.review.Snapshot().Set.Repo.Root, fd.File.Path)
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
		{"Settings over environment", "cfg-edit", map[string]string{"AGENT_MANAGER_EDITOR": "env-edit"}, []string{"code"}, "cfg-edit"},
		{"environment over PATH", "", map[string]string{"AGENT_MANAGER_EDITOR": "env-edit"}, []string{"code"}, "env-edit"},
		{"first GUI editor on PATH wins", "", nil, []string{"zed", "cursor"}, "cursor"},
		{"PATH over $VISUAL", "", map[string]string{"VISUAL": "vim"}, []string{"code"}, "code"},
		{"$VISUAL over $EDITOR", "", map[string]string{"VISUAL": "vim", "EDITOR": "nano"}, nil, "vim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{services: services{editor: tc.configured}}
			captureEditor(t, tc.installed...)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := m.captureEditorResolution().resolve(); got != tc.want {
				t.Fatalf("editor resolution = %q, want %q", got, tc.want)
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

	if got := m.captureEditorResolution().resolve(); got != "nvim" {
		t.Fatalf("editor resolution = %q, want nvim", got)
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
	m.services.editor = "my-own-edit-wrapper"
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

	_, cmd := m.openEditor()
	applyEditorLookup(t, m, cmd)

	if len(*launched) != 0 {
		t.Fatalf("nothing should launch without an editor, got %v", *launched)
	}
	if !strings.Contains(m.errBar.text, "Settings > editor") {
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

	_, cmd := m.openEditor()
	applyEditorLookup(t, m, cmd)

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
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	cmd = resolveEditorLookup(t, m, cmd)
	if cmd == nil {
		t.Fatalf("the request produced no launch, err = %q", m.errBar.text)
	}
	request, err := m.services.tmux.PendingRequest()
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
	if done.returnTo.sessionID != sess.ID || done.returnTo.foregroundGen != m.gens.foreground || done.returnTo.mode != m.mode {
		t.Fatalf("return target = %+v, want session %q at generation %d mode %v", done.returnTo, sess.ID, m.gens.foreground, m.mode)
	}
	if want := []string{"code", resolved(t, dir)}; !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}

	updated, cmd = m.Update(done)
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatal("the session should get its client back")
	}
	attachment, isAttachment := m.effects.active.request.(attachRequest)
	if !isAttachment || attachment.id != sess.ID {
		t.Fatalf("reattach target=%+v", attachment)
	}
	if foreground := m.foregroundTestCmd(t, cmd); foreground == nil {
		t.Fatal("prepared reattach did not emit terminal command")
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
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	cmd = resolveEditorLookup(t, m, cmd)
	if cmd != nil {
		t.Fatal("a refused request should return no command")
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
	m.services.editor = "my-own-edit-wrapper"
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
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	cmd = resolveEditorLookup(t, m, cmd)
	if cmd == nil {
		t.Fatalf("the request produced no launch, err = %q", m.errBar.text)
	}
	if len(*launched) != 0 {
		t.Fatalf("a terminal editor must not start detached, got %v", *launched)
	}
}

// An editor that failed to start keeps the list: going back into the
// session would hide the only account of what went wrong.
func TestEditorFailureKeepsTheListAndItsReason(t *testing.T) {
	m := buildModel(t)
	captureEditor(t, "code")
	createSession(t, m, "editme", t.TempDir(), "")
	m.selectSessionRow(t, "editme")
	returnTo := editorReturnTarget{sessionID: m.sessionRows()[0].ID, foregroundGen: m.gens.foreground, mode: m.mode}

	updated, cmd := m.Update(editorDoneMsg{err: errors.New("exec: \"code\": file does not exist"), returnTo: returnTo})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatal("a failed editor should not hand the session back its client")
	}
	if !strings.Contains(m.errBar.text, "does not exist") {
		t.Fatalf("status line should carry the failure, got %q", m.errBar.text)
	}
}

func TestEditorReturnCompletionCannotCrossHelpOrQuit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{
			name: "help",
			mutate: func(m *Model) {
				updated, _ := m.handleKey(key("?"))
				*m = *updated.(*Model)
			},
		},
		{
			name: "quit",
			mutate: func(m *Model) {
				_, _ = m.requestQuit()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			captureEditor(t, "code")
			createSession(t, m, "editme", t.TempDir(), "")
			m.selectSessionRow(t, "editme")
			sess := m.sessionRows()[0]
			started := make(chan struct{})
			release := make(chan struct{})
			resolvedStart := startEditor
			startEditor = func(cmd *exec.Cmd) error {
				close(started)
				<-release
				return resolvedStart(cmd)
			}

			_, directory := m.openEditorWithReaderForReturn(systemTerminalDirectoryReader{
				tmux: m.services.tmux,
				dirs: systemDirectoryPreflight{git: m.services.gitDrv},
			}, sess.ID)
			rawDirectory := directory()
			directoryMsg, ok := rawDirectory.(terminalDirectoryMsg)
			if !ok {
				t.Fatalf("directory command returned %T", rawDirectory)
			}
			updated, launch := m.handleTerminalDirectory(directoryMsg)
			*m = *updated.(*Model)
			if launch == nil {
				t.Fatal("resolved directory did not prepare an editor launch")
			}
			completed := make(chan tea.Msg, 1)
			go func() { completed <- launch() }()
			<-started
			tc.mutate(m)
			close(release)

			done := (<-completed).(editorDoneMsg)
			if done.returnTo.sessionID != sess.ID {
				t.Fatalf("completion return target = %+v, want %q", done.returnTo, sess.ID)
			}
			updated, next := m.Update(done)
			*m = *updated.(*Model)
			if next != nil {
				m.applyTestMsg(t, next())
			}
			if m.effects.active != nil {
				if _, ok := m.effects.active.request.(attachRequest); ok {
					t.Fatalf("stale editor completion reattached through %s", tc.name)
				}
			}
			for _, job := range m.effects.pending {
				if _, ok := job.request.(attachRequest); ok {
					t.Fatalf("stale editor completion queued a reattach through %s", tc.name)
				}
			}
			if tc.name == "help" && m.mode != modeHelp {
				t.Fatalf("stale editor completion replaced Help: mode=%v", m.mode)
			}
		})
	}
}

func TestEditorReturnCompletionsKeepTheirOwnSessions(t *testing.T) {
	m := buildModel(t)
	first := editorReturnTarget{sessionID: "first", foregroundGen: m.gens.foreground, mode: m.mode}
	second := editorReturnTarget{sessionID: "second", foregroundGen: m.gens.foreground, mode: m.mode}

	updated, _ := m.Update(editorDoneMsg{returnTo: first})
	*m = *updated.(*Model)
	updated, _ = m.Update(editorDoneMsg{returnTo: second})
	*m = *updated.(*Model)

	if m.effects.active == nil {
		t.Fatal("first completion did not enqueue its return")
	}
	active, ok := m.effects.active.request.(attachRequest)
	if !ok || active.id != "first" {
		t.Fatalf("first completion attached %+v", m.effects.active.request)
	}
	if len(m.effects.pending) != 1 {
		t.Fatalf("pending returns = %d, want 1", len(m.effects.pending))
	}
	pending, ok := m.effects.pending[0].request.(attachRequest)
	if !ok || pending.id != "second" {
		t.Fatalf("second completion queued %+v", m.effects.pending[0].request)
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
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	applyEditorLookup(t, m, cmd)

	if want := []string{"code", resolved(t, attachedDir)}; !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want the attached session's directory %v", *launched, want)
	}

	// A session that left the list takes its request with it.
	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	*launched = nil
	updated, cmd = m.Update(attachDoneMsg{sessID: "gone"})
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	if cmd != nil || len(*launched) != 0 {
		t.Fatalf("a session that is gone should launch nothing, got %v", *launched)
	}
	if !strings.Contains(m.errBar.text, "left the list") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}

func TestReviewDoesNotOpenTheFileTheCursorLeft(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	openReviewOn(t, m, "opener", gitRepoWithTwoChangedFiles(t))
	first := m.currentFileDiff()
	if first == nil {
		t.Fatal("review has no selected file")
	}
	// The loaded file replaces its slot in the set, so the path is what
	// survives the reload the switch below drains.
	firstPath := first.File.Path
	_, cmd := m.openDiffFile()
	if cmd == nil {
		t.Fatal("o returned no command")
	}

	m.drainCmds(t, m.switchDiffFile(1))
	moved := m.currentFileDiff()
	if moved == nil || moved.File.Path == firstPath {
		t.Fatalf("the cursor did not move off %s", firstPath)
	}

	if next := m.stepCmd(t, cmd); next != nil {
		t.Fatal("the check for the file the cursor left should not launch an editor")
	}
	if len(*launched) != 0 {
		t.Fatalf("editor launched for the file the cursor left: %v", *launched)
	}
}

// The launch and return policy is decided on captured values, with no model.
func TestEditorLaunchAndReturnPolicy(t *testing.T) {
	if (editorLaunch{name: "code"}).takesScreen() || !(editorLaunch{name: "vim"}).takesScreen() {
		t.Fatal("a windowed editor took the screen, or a terminal one did not")
	}
	target := editorReturnTarget{sessionID: "s1", foregroundGen: 4, mode: modeFocus}
	cases := []struct {
		name     string
		target   editorReturnTarget
		gen      uint64
		current  mode
		quitting bool
		want     bool
	}{
		{"unchanged", target, 4, modeFocus, false, true},
		{"no session", editorReturnTarget{foregroundGen: 4, mode: modeFocus}, 4, modeFocus, false, false},
		{"user moved on", target, 5, modeFocus, false, false},
		{"mode changed", target, 4, modeList, false, false},
		{"quitting", target, 4, modeFocus, true, false},
	}
	for _, tc := range cases {
		if got := tc.target.resumes(tc.gen, tc.current, tc.quitting); got != tc.want {
			t.Errorf("%s: resumes = %v, want %v", tc.name, got, tc.want)
		}
	}
}
