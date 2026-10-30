package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAttachDoneOpensReviewWhenMarkerSet(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	sess := m.sessionRows()[0]
	clearRequestOnCleanup(t, m, sess.ID)

	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if m.base != baseReview {
		t.Fatalf("marker set should enter review, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}

	request, err := m.services.tmux.PendingRequest(sess.ID)
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("opening review should consume the marker, got %q", request)
	}
}

func TestAttachDoneStaysInListWithoutMarker(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "plainexit", t.TempDir(), "")
	m.selectSessionRow(t, "plainexit")
	if err := m.services.tmux.ClearRequest(m.sessionRows()[0].ID); err != nil {
		t.Fatalf("clear marker: %v", err)
	}

	updated, _ := m.Update(attachDoneMsg{})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("no marker should stay in list, screen = %v", m.screenID())
	}
}

func TestAttachAcknowledgesFinished(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.workspace.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "alert-me")

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished {
		t.Fatalf("the update path acknowledged the session: %q", got.Status)
	}
	if _, err := m.bindEffect(uilifecycle.AttachRequest{ID: sess.ID})(); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle {
		t.Fatalf("after attach, status = %q want %q", got.Status, status.Idle)
	}
	if !got.Acked {
		t.Fatal("attach should mark the session acked")
	}
}

func TestDotAcknowledgesOnlyCurrentFinishedStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		listed     string
		stored     string
		wantStatus string
		wantAcked  bool
		wantOffer  bool
	}{
		{name: "finished", listed: status.Finished, stored: status.Finished, wantStatus: status.Idle, wantAcked: true, wantOffer: true},
		{name: "stale snapshot", listed: status.Finished, stored: status.Working, wantStatus: status.Working, wantOffer: true},
		{name: "working", listed: status.Working, stored: status.Working, wantStatus: status.Working},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "alert-me", t.TempDir(), "")
			sess := m.sessionRows()[0]
			if err := m.services.store.UpdateStatus(sess.ID, tc.stored); err != nil {
				t.Fatalf("set stored status: %v", err)
			}
			m.workspace.sessions[0].Status = tc.listed
			m.rebuildRows()
			m.selectSessionRow(t, "alert-me")
			if offered := strings.Contains(m.viewFooter(), "mark idle"); offered != tc.wantOffer {
				t.Fatalf("dot footer offered = %v, want %v", offered, tc.wantOffer)
			}

			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
			m.drainEffects(t)
			got, err := m.services.store.Get(sess.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Status != tc.wantStatus || got.Acked != tc.wantAcked {
				t.Fatalf("after dot, status = %q acked = %v", got.Status, got.Acked)
			}
			if !m.onList() {
				t.Fatalf("dot left list mode: %v", m.screenID())
			}
		})
	}
}

func TestDotKeepsArchivedFinishedStatus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "kept", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.workspace.sessions[0].Status = status.Finished
	m.workspace.sessions[0].Archived = true
	m.rail.SetArchived(true)
	m.rebuildRows()
	m.selectSessionRow(t, "kept")
	if strings.Contains(m.viewFooter(), "mark idle") {
		t.Fatal("dot offered on an archived session")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	m.drainEffects(t)
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished || got.Acked {
		t.Fatalf("archived session changed: status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestAttachKeepsWorking(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "busy-one", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Working); err != nil {
		t.Fatalf("set working: %v", err)
	}
	m.workspace.sessions[0].Status = status.Working
	m.rebuildRows()
	m.selectSessionRow(t, "busy-one")

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	if _, err := m.bindEffect(uilifecycle.AttachRequest{ID: sess.ID})(); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Working {
		t.Fatalf("after attach, status = %q want %q", got.Status, status.Working)
	}
}

// PrepareAttach flips window-size to auto, which reflows the pane the same
// way the detach-side resize does; without clearing the cached hash first,
// the next poll compares the reflowed pane against a pre-attach hash and
// reads it as working (TestRebaselineKeepsFinishedWithoutFlashingWorking
// proves that precondition). Attach must clear it the same way detach does.
func TestAttachClearsStaleHashBeforeReflow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form().SetName("attach-reflow")
	m.form().SetDir(t.TempDir())
	setFormTool(m, 1) // claude-hooked: configured with an activity region to hash
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	if !m.onList() {
		t.Fatalf("after submit, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}

	sess := m.sessionRows()[0]
	if sess.Tool != "claude-hooked" {
		t.Fatalf("session tool = %q, want claude-hooked", sess.Tool)
	}
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	sess.Status = status.Finished
	m.workspace.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "attach-reflow")

	if err := m.services.tmux.SendText(sess.ID, "final answer line that wraps differently after attach\n❯ "); err != nil {
		t.Fatal(err)
	}
	resetExecution(m)
	m.applyCmd(t, m.refreshCmd())
	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	if _, err := m.bindEffect(uilifecycle.AttachRequest{ID: sess.ID})(); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	entered, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if entered.Status != status.Idle {
		t.Fatalf("attach must acknowledge and rebaseline the pane, got %q", entered.Status)
	}
}

// y on a dead row names the way back, the way every other lifecycle key
// does, instead of letting the capture fail with raw tmux stderr.
func TestCopyReplyOnADeadSessionHints(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "alpha")
	_, cmd := m.copyReplySelected()
	if cmd == nil {
		t.Fatal("checking the session should run asynchronously")
	}
	if m.errBar.text != "" {
		t.Fatalf("copy reported %q before the command ran", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.errBar.text != uisession.DeadHint(keybind.DefaultList()) {
		t.Fatalf("errBar = %q, want %q", m.errBar.text, uisession.DeadHint(keybind.DefaultList()))
	}
}

// A group has no reply of its own, so y on one does nothing at all.
func TestCopyReplyIgnoresGroupRows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("team", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "team")
	m.selectGroupRow(t, "team")
	_, cmd := m.copyReplySelected()
	if cmd != nil {
		t.Fatal("a group row should not run a capture")
	}
	if m.errBar.text != "" {
		t.Fatalf("errBar = %q, want no message", m.errBar.text)
	}
}

// The copy reports what landed on the clipboard, and says so plainly when
// the turn held nothing worth copying.
func TestReplyCopiedMsgReports(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if _, cmd := m.Update(replyCopiedMsg{name: "alpha"}); cmd != nil {
		t.Fatal("an empty copy should not queue more work")
	}
	if m.errBar.text != "nothing to copy from alpha" {
		t.Fatalf("errBar = %q", m.errBar.text)
	}
	if _, cmd := m.Update(replyCopiedMsg{chars: 42, name: "alpha"}); cmd != nil {
		t.Fatal("a finished copy should not queue more work")
	}
	if m.errBar.text != "copied 42 chars from alpha" || m.errBar.done != m.errBar.text {
		t.Fatalf("errBar = %q done = %q", m.errBar.text, m.errBar.done)
	}
}

// A copy the pane could not bound, and a pane no reply can be read from,
// both name the tool behind the caveat and read as warnings rather than
// as outcomes or failures.
func TestReplyCopiedMsgWarnsOnAnUnboundedCopy(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.Update(replyCopiedMsg{chars: 12426, name: "alpha", tool: "grok", unbounded: true})
	if !strings.Contains(m.errBar.text, "grok marks no turn start here") {
		t.Fatalf("errBar = %q, want the tool named", m.errBar.text)
	}
	if !m.errBar.warned() || m.errBar.worked() {
		t.Fatalf("an unbounded copy should warn, not report success: %+v", m.errBar)
	}

	m = buildModel(t)
	m.Update(replyCopiedMsg{name: "beta", tool: "pi", unreadable: true})
	if !strings.Contains(m.errBar.text, "a pi pane is not read that way") {
		t.Fatalf("errBar = %q, want the tool named", m.errBar.text)
	}
	if !m.errBar.warned() {
		t.Fatalf("an unreadable pane should warn: %+v", m.errBar)
	}
}

func TestPreparedAttachDoesNotReplaceNewerHelp(t *testing.T) {
	t.Parallel()
	for _, returning := range []bool{false, true} {
		t.Run(map[bool]string{false: "attach", true: "reattach"}[returning], func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "attach-fence", t.TempDir(), "")
			m.selectSessionRow(t, "attach-fence")
			sess, _ := m.selected()
			if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
				t.Fatal(err)
			}
			var command tea.Cmd
			if returning {
				command = m.reattach(sess.ID, m.review.Generation())
			} else {
				command = m.attachCmd(sess.ID)
			}
			request := m.effects.Main.Active.Request.(uilifecycle.AttachRequest)
			result := command().(uieffects.Completed)
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			got, err := m.services.store.Get(sess.ID)
			if err != nil || !got.Acked {
				t.Fatalf("accepted acknowledgement was lost: %+v %v", got, err)
			}
			m.gens.foreground++
			m.openHelp()
			if next := m.applyAttachEffect(request, result.Result.(uilifecycle.AttachResult), result.Err); next != nil {
				t.Fatal("prepared attach replaced newer Help")
			}
			if !helpOpen(m) {
				t.Fatal("Help was replaced")
			}
		})
	}
}

func TestAttachReportsTransportFailureWithoutCallingPaneDead(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	m := buildModel(t)
	dir := t.TempDir()
	stub := filepath.Join(dir, "tmux")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'attach liveness unavailable' >&2\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	driver, err := tmux.NewWithSocket(m.services.tmux.SocketName())
	if err != nil {
		t.Fatal(err)
	}
	services := effectServices{store: m.services.store, driver: driver}
	_, err = uilifecycle.RunAttach(services.lifecycleServices(), uilifecycle.AttachRequest{ID: "attach-probe"})
	if err == nil || !strings.Contains(err.Error(), "attach liveness unavailable") || strings.Contains(err.Error(), uisession.DeadHint(keybind.DefaultList())) {
		t.Fatalf("transport failure mislabeled: %v", err)
	}
}

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
	if !ok || prepared.msg.Editor.Command == nil {
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
	if !samePaths(*launched, want) {
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
	_, cmd := m.openEditorWithReaderForReturn(reader, "")
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
	if !formOpen(m) {
		t.Fatalf("stale editor result replaced the newer dialog: screen = %v", m.screenID())
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
	if !ok || prepared.editor.Command == nil {
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
			if got := m.captureEditorResolution().Line(); got != tc.want {
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

	if got := m.captureEditorResolution().Line(); got != "nvim" {
		t.Fatalf("editor resolution = %q, want nvim", got)
	}
	if uisession.Detached(uisession.Name("nvim")) {
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
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to remove a directory that is a live pane's working directory")
	}
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
	clearRequestOnCleanup(t, m, sess.ID)

	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, cmd := m.Update(attachDoneMsg{sessID: sess.ID})
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	cmd = resolveEditorLookup(t, m, cmd)
	if cmd == nil {
		t.Fatalf("the request produced no launch, err = %q", m.errBar.text)
	}
	request, err := m.services.tmux.PendingRequest(sess.ID)
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
	if done.returnTo.sessionID != sess.ID || done.returnTo.foregroundGen != m.gens.foreground || done.returnTo.screen != m.screenID() {
		t.Fatalf("return target = %+v, want session %q at generation %d mode %v", done.returnTo, sess.ID, m.gens.foreground, m.screenID())
	}
	if want := []string{"code", resolved(t, dir)}; !samePaths(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}

	updated, cmd = m.Update(done)
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatal("the session should get its client back")
	}
	attachment, isAttachment := m.effects.Main.Active.Request.(uilifecycle.AttachRequest)
	if !isAttachment || attachment.ID != sess.ID {
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
	clearRequestOnCleanup(t, m, sess.ID)

	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
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
	clearRequestOnCleanup(t, m, sess.ID)

	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
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
	returnTo := editorReturnTarget{sessionID: m.sessionRows()[0].ID, foregroundGen: m.gens.foreground, screen: m.screenID()}

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

			_, directory := m.openEditorWithReaderForReturn(newSystemTerminalDirectoryReader(m), sess.ID)
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
			if m.effects.Main.Active != nil {
				if _, ok := m.effects.Main.Active.Request.(uilifecycle.AttachRequest); ok {
					t.Fatalf("stale editor completion reattached through %s", tc.name)
				}
			}
			for _, job := range m.effects.Main.Pending {
				if _, ok := job.Request.(uilifecycle.AttachRequest); ok {
					t.Fatalf("stale editor completion queued a reattach through %s", tc.name)
				}
			}
			if tc.name == "help" && !helpOpen(m) {
				t.Fatalf("stale editor completion replaced Help: screen = %v", m.screenID())
			}
		})
	}
}

func TestEditorReturnCompletionsKeepTheirOwnSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	first := editorReturnTarget{sessionID: "first", foregroundGen: m.gens.foreground, screen: m.screenID()}
	second := editorReturnTarget{sessionID: "second", foregroundGen: m.gens.foreground, screen: m.screenID()}

	updated, _ := m.Update(editorDoneMsg{returnTo: first})
	*m = *updated.(*Model)
	updated, _ = m.Update(editorDoneMsg{returnTo: second})
	*m = *updated.(*Model)

	if m.effects.Main.Active == nil {
		t.Fatal("first completion did not enqueue its return")
	}
	active, ok := m.effects.Main.Active.Request.(uilifecycle.AttachRequest)
	if !ok || active.ID != "first" {
		t.Fatalf("first completion attached %+v", m.effects.Main.Active.Request)
	}
	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("pending returns = %d, want 1", len(m.effects.Main.Pending))
	}
	pending, ok := m.effects.Main.Pending[0].Request.(uilifecycle.AttachRequest)
	if !ok || pending.ID != "second" {
		t.Fatalf("second completion queued %+v", m.effects.Main.Pending[0].Request)
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
	clearRequestOnCleanup(t, m, attached.ID)

	if _, err := sessionTmuxCmd(t, attached.ID, "set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, cmd := m.Update(attachDoneMsg{sessID: attached.ID})
	cmd = m.foregroundTestCmd(t, cmd)
	*m = *updated.(*Model)
	applyEditorLookup(t, m, cmd)

	if want := []string{"code", resolved(t, attachedDir)}; !samePaths(*launched, want) {
		t.Fatalf("launched %v, want the attached session's directory %v", *launched, want)
	}

	// A session that left the list takes its request with it. psmux keeps
	// the marker on the session's own server, which leaves with it.
	if runtime.GOOS == "windows" {
		return
	}
	if _, err := sessionTmuxCmd(t, attached.ID, "set-option", "-g", "@am_request", tmux.RequestEditor).CombinedOutput(); err != nil {
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

// The return policy is decided on captured values, with no model.
func TestEditorReturnPolicy(t *testing.T) {
	focus := screenID{focused: true, depth: 1}
	target := editorReturnTarget{sessionID: "s1", foregroundGen: 4, screen: focus}
	cases := []struct {
		name     string
		target   editorReturnTarget
		gen      uint64
		current  screenID
		quitting bool
		want     bool
	}{
		{"unchanged", target, 4, focus, false, true},
		{"no session", editorReturnTarget{foregroundGen: 4, screen: focus}, 4, focus, false, false},
		{"user moved on", target, 5, focus, false, false},
		{"screen changed", target, 4, screenID{}, false, false},
		{"quitting", target, 4, focus, true, false},
	}
	for _, tc := range cases {
		if got := tc.target.resumes(tc.gen, tc.current, tc.quitting); got != tc.want {
			t.Errorf("%s: resumes = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAStaleRefreshKeepsASessionLaunchedAfterItWasListed(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	// What a poll hands the UI: the session list as it stood when the pass
	// opened, delivered once the pass has finished its tmux and ps calls.
	inFlight := m.poller.refreshOnce()

	if err := m.spawnSession("claude", "late-arrival", t.TempDir(), "", "", false, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	launched := m.sessionRows()
	if len(launched) != 1 {
		t.Fatalf("rows after the spawn = %d, want the one just launched", len(launched))
	}

	updated, _ := m.Update(inFlight)
	m.drainEffects(t)
	*m = *updated.(*Model)

	if uilifecycle.SessionGone(m.workspace.sessions, launched[0].ID) {
		t.Fatalf("the stale poll took the launched session off screen, leaving %v", m.sessionRows())
	}
}

func TestAStaleRefreshKeepsALaunchListedWhileTmuxWasStartingIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	if err := m.spawnSession("claude", "slow-window", t.TempDir(), "", "", false, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	launched := m.sessionRows()[0]

	// Building the tmux window takes tens of milliseconds, so a poll can
	// list the store after the launch began and still be missing its row.
	updated, _ := m.Update(refreshMsg{listedAt: launched.CreatedAt.Add(time.Millisecond)})
	m.drainEffects(t)
	*m = *updated.(*Model)

	if uilifecycle.SessionGone(m.workspace.sessions, launched.ID) {
		t.Fatalf("the stale poll took the launched session off screen, leaving %v", m.sessionRows())
	}
}

// pressRune sends one key through Update the way the terminal would.
func (m *Model) pressRune(t *testing.T, r rune) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	*m = *updated.(*Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ = m.Update(msg)
			m.drainEffects(t)
			*m = *updated.(*Model)
		}
	}
}

// staleRefreshAfter is the refresh a poll pass that opened before the launch
// would deliver: it carries no sessions and a listing time from before them.
func staleRefreshAfter(m *Model) refreshMsg {
	earliest := time.Now()
	for _, at := range m.ledger.lifecycle.Launched {
		if at.Before(earliest) {
			earliest = at
		}
	}
	return refreshMsg{listedAt: earliest.Add(-time.Millisecond)}
}

func TestAStaleRefreshDoesNotBringBackASessionJustDeleted(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	if err := m.spawnSession("claude", "doomed", t.TempDir(), "", "", false, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, sess.Name)
	m.pressRune(t, 'd')
	m.pressRune(t, 'y')
	if _, err := m.services.store.Get(sess.ID); err == nil {
		t.Fatal("the delete did not reach the store")
	}

	updated, _ := m.Update(staleRefreshAfter(m))
	m.drainEffects(t)
	*m = *updated.(*Model)

	if !uilifecycle.SessionGone(m.workspace.sessions, sess.ID) {
		t.Fatalf("a deleted session came back on a stale poll: %v", m.sessionRows())
	}
}

func TestAStaleRefreshDoesNotBringBackASessionJustArchived(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	if err := m.spawnSession("claude", "shelved", t.TempDir(), "", "", false, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, sess.Name)
	m.pressRune(t, 'a')
	m.pressRune(t, 'y')

	updated, _ := m.Update(staleRefreshAfter(m))
	m.drainEffects(t)
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("an archived session came back on the live tree as %q", row.Status)
		}
	}
}

// A poll that listed the store before the delete delivers its full list
// afterwards; that stale copy must not put the row back on screen.
func TestAStalePollCannotResurrectADeletedRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	deleteSession(t, m, "doomed")

	stale := sess
	stale.Status = status.Dead
	updated, _ := m.Update(refreshMsg{sessions: []store.Session{stale}, listedAt: listedAt})
	m.drainEffects(t)
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("a stale poll brought the deleted row back as %q", row.Status)
		}
	}
}

func TestAStalePollCannotBringAnArchivedRowBackLive(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "shelved", t.TempDir(), "")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	m.selectSessionRow(t, "shelved")
	m.archiveSelected()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	stale := sess
	stale.Status = status.Dead
	updated, _ := m.Update(refreshMsg{sessions: []store.Session{stale}, listedAt: listedAt})
	m.drainEffects(t)
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("a stale poll brought the archived row back on the live tree")
		}
	}
}

func TestAFreshListingRetiresDeletionMarkers(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	listedAt := time.Now()

	deleteSession(t, m, "doomed")

	fresh := refreshMsg{listedAt: listedAt.Add(time.Second)}
	updated, _ := m.Update(fresh)
	m.drainEffects(t)
	*m = *updated.(*Model)

	if len(m.ledger.lifecycle.Gone) != 0 {
		t.Fatalf("deletion markers outlived a listing that postdates them: %v", m.ledger.lifecycle.Gone)
	}
}

func TestAStalePollCannotRestoreADeletedGroupHeader(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	m.selectGroupRow(t, "zone")
	m.prepareDelete()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	stale := refreshMsg{
		sessions:       []store.Session{sess},
		listedAt:       listedAt,
		groups:         []string{"zone"},
		groupPaths:     map[string]string{"zone": dir},
		groupWorktrees: map[string]string{"zone": dir},
		archivedGroups: map[string]bool{"zone": true},
	}
	updated, _ := m.Update(stale)
	m.drainEffects(t)
	*m = *updated.(*Model)

	for _, r := range railRows(m) {
		if r.isGroup && r.group == "zone" {
			t.Fatalf("a stale poll brought the deleted group header back")
		}
	}
	for _, g := range m.workspace.groups {
		if g == "zone" {
			t.Fatal("a stale poll restored the deleted group path")
		}
	}
	for _, meta := range []map[string]string{m.workspace.groupPaths, m.workspace.groupWorktrees} {
		if _, ok := meta["zone"]; ok {
			t.Fatal("a stale poll restored deleted group metadata")
		}
	}
	if _, ok := m.workspace.archivedGroups["zone"]; ok {
		t.Fatal("a stale poll restored the deleted group's archive flag")
	}
}

// A poll that listed the store before a restore still says the row is
// archived; that stale copy must not undo the restore for a frame.
func TestAStalePollCannotUndoARestore(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "returning", t.TempDir(), "")
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, "returning")
	m.archiveSelected()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	listedAt := time.Now()

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "returning")
	m.restoreSelected()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	stale := sess
	stale.Archived = true
	updated, _ := m.Update(refreshMsg{sessions: []store.Session{stale}, listedAt: listedAt})
	m.drainEffects(t)
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("a stale poll put the restored session back in the archived view")
		}
	}
	m.rail.SetArchived(false)
	if got := m.visibleSessions(); len(got) != 1 || got[0].Archived {
		t.Fatalf("restored session should read live after a stale listing, got %+v", got)
	}
}

func TestAStalePollCannotBringAnArchivedGroupBackLive(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	m.selectGroupRow(t, "zone")
	m.archiveSelected()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	stale := refreshMsg{
		sessions:   []store.Session{sess},
		listedAt:   listedAt,
		groups:     []string{"zone"},
		groupPaths: map[string]string{"zone": dir},
	}
	updated, _ := m.Update(stale)
	m.drainEffects(t)
	*m = *updated.(*Model)

	if !m.groupEffectivelyArchived("zone") {
		t.Fatal("a stale poll brought the archived group back on the active tree")
	}
	if got := m.visibleSessions(); len(got) != 0 {
		t.Fatalf("archived subtree should stay out of the active view, got %v", got)
	}
}
