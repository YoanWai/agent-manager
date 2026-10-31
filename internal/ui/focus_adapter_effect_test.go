package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/tmux"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFocusEntersOnlyAfterTheProbeRuns(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	if m.focused() {
		t.Fatal("focus entered on the update path before the probe")
	}
	if m.effects.Main.Active == nil {
		t.Fatal("the focus probe should be accepted before the entry")
	}
	m.applyCmd(t, cmd)
	if !m.focused() {
		t.Fatalf("probe should enter focus, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
}

func TestFocusFenceKeepsNewerSelection(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.selectSessionRow(t, "beta")
	m.applyCmd(t, cmd)
	if !m.onList() {
		t.Fatalf("a moved cursor should drop the entry, screen = %v", m.screenID())
	}
	if m.errBar.text != "" {
		t.Fatalf("a fenced entry should report nothing, err = %q", m.errBar.text)
	}
}

func TestFocusDeadSessionNeverEnters(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	if m.errBar.text != "" {
		t.Fatalf("the probe reports from its completion, not the update path: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if !m.onList() {
		t.Fatalf("a dead pane should not enter focus, screen = %v", m.screenID())
	}
	if m.errBar.text != uisession.DeadHint(keybind.DefaultList()) {
		t.Fatalf("err = %q, want %q", m.errBar.text, uisession.DeadHint(keybind.DefaultList()))
	}
}

func TestFocusProbeAcknowledgesFinished(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.selectSessionRow(t, "alert-me")
	_, cmd := m.focusSelected()
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished {
		t.Fatalf("the update path acknowledged the session: %q", got.Status)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("after the probe, status = %q acked = %v", got.Status, got.Acked)
	}
	if !m.focused() {
		t.Fatalf("the probe should enter focus, screen = %v", m.screenID())
	}
}

func TestAckDefersTheStoreWrite(t *testing.T) {
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
	_, cmd := m.acknowledgeSelected()
	if cmd == nil {
		t.Fatalf("the acknowledgement should be accepted, err = %q", m.errBar.text)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished {
		t.Fatalf("the update path wrote the store: %q", got.Status)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("after completion, status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestAckIgnoresGoneSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "gone", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	result, err := m.bindEffect(focusruntime.AckRequest{SessionID: sess.ID})()
	if err == nil {
		t.Fatal("acknowledging a gone session should report the missing row")
	}
	if result != nil {
		t.Fatalf("unexpected result type %T", result)
	}
	if _, lookupErr := m.services.store.Get(sess.ID); lookupErr == nil {
		t.Fatal("the gone session came back")
	}
}

func TestAttachDeadSessionReportsFromCompletion(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "alpha")
	_, cmd := m.attachSelected()
	if m.errBar.text != "" {
		t.Fatalf("the probe reports from its completion, not the update path: %q", m.errBar.text)
	}
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	if m.errBar.text != uisession.DeadHint(keybind.DefaultList()) {
		t.Fatalf("err = %q, want %q", m.errBar.text, uisession.DeadHint(keybind.DefaultList()))
	}
}

func TestAttachWorkerAcknowledgesFinished(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
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
	work := m.bindEffect(uilifecycle.AttachRequest{ID: sess.ID})
	if _, err := work(); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("after the worker, status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestAttachWorkerKeepsWorking(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "busy-one", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Working); err != nil {
		t.Fatalf("set working: %v", err)
	}
	m.selectSessionRow(t, "busy-one")
	work := m.bindEffect(uilifecycle.AttachRequest{ID: sess.ID})
	if _, err := work(); err != nil {
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

func TestDetachProbeReadsAndClearsInOneStep(t *testing.T) {
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
	if m.effects.Main.Active == nil {
		t.Fatal("the detach probe should be accepted before the marker is read")
	}
	if request, err := m.services.tmux.PendingRequest(sess.ID); err != nil || request != tmux.RequestReview {
		t.Fatalf("the marker should be untouched on the update path: %q, %v", request, err)
	}
	m.drainEffects(t)
	if m.base != baseReview {
		t.Fatalf("marker set should enter review, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	request, err := m.services.tmux.PendingRequest(sess.ID)
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("opening review should consume the marker once, got %q", request)
	}
}

func TestFocusFenceDropsEntryAfterDialogOpened(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.openForm()
	m.applyCmd(t, cmd)
	if !formOpen(m) {
		t.Fatalf("a dialog opened after the probe should win, screen = %v", m.screenID())
	}
	if m.focus.pane.Pane().SessionID != "" {
		t.Fatalf("focus entered behind the dialog")
	}
}

func TestPasteAndFollowingRawKeyShareTheOrderedLane(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "ordered", t.TempDir(), "")
	m.selectSessionRow(t, "ordered")
	_, cmd := m.focusSelected()
	m.applyCmd(t, cmd)
	delivered := false
	original := pasteFocused
	pasteFocused = func(d *tmux.Driver, id, text string) error { delivered = true; return nil }
	t.Cleanup(func() { pasteFocused = original })
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello"), Paste: true})
	if delivered {
		t.Fatal("paste ran synchronously in key handling")
	}
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if !delivered {
		t.Fatal("accepted paste did not drain before following raw input")
	}
}

func TestFocusCompletionDoesNotReenterAfterNewerInput(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if m.focused() {
		t.Fatal("stale focus stole foreground after newer input")
	}
}

func TestDetachCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	request := focusruntime.DetachRequest{SessionID: sess.ID}
	m.openForm()
	m.applyDetachEffect(request, focusruntime.Detach{Request: tmux.RequestReview}, nil)
	if !formOpen(m) {
		t.Fatal("detach completion replaced newer dialog")
	}
}

func TestAcknowledgementCompletionFencesOlderPolls(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions[0].Status = status.Finished
	before := time.Now()
	m.effects.latestObservation = before
	_, cmd := m.acknowledgeSelected()
	m.applyCmd(t, cmd)
	if !m.effects.latestObservation.After(before) {
		t.Fatal("acknowledgment did not fence an older finished-status poll")
	}
}

func TestDetachCompletionDoesNotOpenReviewDuringQuit(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	m.requestQuit()
	m.applyDetachEffect(focusruntime.DetachRequest{SessionID: sess.ID}, focusruntime.Detach{Request: tmux.RequestReview}, nil)
	if !m.onList() {
		t.Fatal("detach completion opened review during quit")
	}
}

// detachWithReviewRequest leaves the marker Ctrl+R inside alpha leaves and
// detaches, so the probe waits on the lane behind the reflow.
func detachWithReviewRequest(t *testing.T, m *Model) {
	t.Helper()
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	clearRequestOnCleanup(t, m, sess.ID)
	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
}

func TestDetachRequestSurvivesListNavigation(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	detachWithReviewRequest(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if sess, _ := m.selected(); sess.Name != "beta" {
		t.Fatalf("j should move the cursor first, selected %q", sess.Name)
	}
	m.drainEffects(t)
	if m.base != baseReview {
		t.Fatalf("a review asked for inside the session was lost to list navigation, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if sess, _ := m.selected(); sess.Name != "alpha" {
		t.Fatalf("review should open for the session that asked, selected %q", sess.Name)
	}
}

func TestDetachRequestDroppedForNewerDialogIsReported(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	detachWithReviewRequest(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.openForm()
	m.drainEffects(t)
	if !formOpen(m) {
		t.Fatalf("the newer dialog should keep the foreground, screen = %v", m.screenID())
	}
	if !m.errBar.warned() || !strings.Contains(m.errBar.text, "review") || !strings.Contains(m.errBar.text, "alpha") {
		t.Fatalf("a dropped review should say so, status = %q", m.errBar.text)
	}
}

func TestDetachRequestYieldsToQueuedFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	detachWithReviewRequest(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.focusSelected()
	m.drainEffects(t)
	if sess, _ := m.selected(); !m.focused() || sess.Name != "beta" {
		t.Fatalf("the focus asked for after detaching should win, screen = %v, selected %q", m.screenID(), sess.Name)
	}
	if !m.errBar.warned() || !strings.Contains(m.errBar.text, "review") {
		t.Fatalf("a dropped review should say so, status = %q", m.errBar.text)
	}
}

func TestPendingFocusSurvivesTheInitiatingMouseRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease})
	m.applyCmd(t, cmd)
	if !m.focused() {
		t.Fatal("release of the initiating click cancelled its pending focus")
	}
}

func TestPendingFocusDistinguishesIgnoredAndBoundKeys(t *testing.T) {
	for _, bound := range []bool{false, true} {
		name := "ignored"
		if bound {
			name = "opens help"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := buildModel(t)
			createSession(t, m, "alpha", t.TempDir(), "")
			m.drainEffects(t)
			m.selectSessionRow(t, "alpha")
			if bound {
				key, err := keybind.Parse("f12")
				if err != nil {
					t.Fatal(err)
				}
				m.services.listKeys = m.services.listKeys.With(keybind.Help, keybind.Keys(key))
			}
			_, pending := m.focusSelected()
			m.Update(tea.KeyMsg{Type: tea.KeyF12})
			if bound {
				if !helpOpen(m) {
					t.Fatal("bound key did not open help")
				}
				m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			}
			m.applyCmd(t, pending)
			if bound && !m.onList() {
				t.Fatal("old focus reopened after help was dismissed")
			}
			if !bound && !m.focused() {
				t.Fatal("ignored key cancelled pending focus")
			}
		})
	}
}

// A live focus watcher carries keys over its existing control client. The
// driver wrapper logs every new tmux process, so an empty log after runInput
// proves the acknowledged control command did not also take the forked path.
func TestInputKeysUseServingControlWithoutDriverFallback(t *testing.T) {
	skipControlMode(t)
	m := buildModel(t)
	createSession(t, m, "control-input", t.TempDir(), "")
	sess := m.sessionRows()[0]

	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "invocations")
	stub := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + tmux.ShellQuote(log) + "\nexec " + tmux.ShellQuote(realTmux) + " \"$@\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	driver, err := tmux.NewWithSocket(m.services.tmux.SocketName())
	if err != nil {
		t.Fatal(err)
	}
	watch := newFocusWatch(driver, func(tea.Msg) {})
	t.Cleanup(watch.Close)
	watch.SetFocus(sess.ID)
	deadline := time.Now().Add(5 * time.Second)
	for !watch.Serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Fatal("control client never served the session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	command, ok := focusKeyCommand(tmux.PaneTarget(sess.ID), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("control-only")})
	if !ok {
		t.Fatal("test key was not representable")
	}
	services := focusruntime.Writer{Store: m.services.store, Driver: driver, Watch: watch}
	if _, err := services.Run(focusruntime.Request{Kind: focusruntime.Keys, Session: sess, Command: command}); err != nil {
		t.Fatalf("runInput: %v", err)
	}
	invocations, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(invocations) != 0 {
		t.Fatalf("serving control key also forked tmux: %q", invocations)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		pane, err := m.services.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(pane, "control-only") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("forwarded key never reached pane: %q", pane)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The input lane may switch from the persistent control client to a forked
// paste transport. It must wait for the key command's acknowledgement first,
// or the paste can overtake a key whose write reached tmux but is still queued.
func TestControlKeyAcknowledgementPrecedesPasteTransport(t *testing.T) {
	skipControlMode(t)
	m := buildModel(t)
	createSession(t, m, "ack-ordered-input", t.TempDir(), "")
	sess := m.sessionRows()[0]
	dir := t.TempDir()
	marker := filepath.Join(dir, "key-written")
	release := filepath.Join(dir, "release-key-ack")
	stub := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
case " $* " in
  *" has-session "*) exit 0 ;;
  *" attach-session "*)
    printf '%s\n' '%begin 1 0 0' '%end 1 0 0'
    sequence=2
    while IFS= read -r command; do
      output=''
      case "$command" in
        display-message*) output='0,0,1,000,0,0,0' ;;
        send-keys*)
          : > ` + tmux.ShellQuote(marker) + `
          while [ ! -f ` + tmux.ShellQuote(release) + ` ]; do sleep 0.01; done
          ;;
      esac
      printf '%%begin %s 0 0\n' "$sequence"
      if [ -n "$output" ]; then printf '%s\n' "$output"; fi
      printf '%%end %s 0 0\n' "$sequence"
      sequence=$((sequence + 1))
    done
    ;;
esac
exit 0
`
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	driver, err := tmux.NewWithSocket(testSocket + "-ack-order")
	if err != nil {
		t.Fatal(err)
	}
	watch := newFocusWatch(driver, func(tea.Msg) {})
	t.Cleanup(watch.Close)
	watch.SetFocus(sess.ID)
	deadline := time.Now().Add(5 * time.Second)
	for !watch.Serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Fatal("control client never served the session")
		}
		time.Sleep(10 * time.Millisecond)
	}

	key, ok := focusKeyCommand(tmux.PaneTarget(sess.ID), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("before-paste")})
	if !ok {
		t.Fatal("test key was not representable")
	}
	pasteStarted := make(chan struct{})
	services := focusruntime.Writer{
		Store:  m.services.store,
		Driver: driver,
		Watch:  watch,
		Paste: func(driver *tmux.Driver, id, text string) error {
			close(pasteStarted)
			return driver.Paste(id, text)
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := services.Run(focusruntime.Request{Kind: focusruntime.Keys, Session: sess, Command: key})
		if err == nil {
			_, err = services.Run(focusruntime.Request{Kind: focusruntime.Paste, Session: sess, Text: "after-paste"})
		}
		done <- err
	}()

	// The stub marks the key write before withholding its reply. The following
	// paste must remain unstarted until that reply is released.
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("control client never wrote the key command")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-pasteStarted:
		t.Fatal("paste transport started before the control key acknowledgement")
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ordered input: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("paste did not run after the control acknowledgement")
	}
}

func TestFocusPasteUpdateDefersIO(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	m.selectSessionRow(t, "input-target")
	m.push(newFocusSurface(m))
	called := false
	original := pasteFocused
	pasteFocused = func(*tmux.Driver, string, string) error { called = true; return nil }
	t.Cleanup(func() { pasteFocused = original })
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft"), Paste: true})
	if called {
		t.Fatal("Update executed the paste instead of capturing it")
	}
	m.drainEffects(t)
	if !called {
		t.Fatal("accepted paste was not delivered by its effect")
	}
}

func TestInputPasteCannotBeOvertakenByEnter(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	sess := m.sessionRows()[0]
	dir := t.TempDir()
	log := filepath.Join(dir, "order")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + tmux.ShellQuote(log) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	driver, err := tmux.NewWithSocket(m.services.tmux.SocketName())
	if err != nil {
		t.Fatal(err)
	}
	m.services.tmux = driver
	original := pasteFocused
	pasteFocused = func(*tmux.Driver, string, string) error { return os.WriteFile(log, []byte("paste\n"), 0600) }
	t.Cleanup(func() { pasteFocused = original })
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Paste, Session: sess, Text: "draft"})
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Keys, Session: sess, Command: "send-keys -t " + tmux.PaneTarget(sess.ID) + " Enter"})
	m.drainEffects(t)
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || lines[0] != "paste" || !strings.Contains(lines[1], "Enter") {
		t.Fatalf("input order = %q", data)
	}
}

func TestInputRefusesRelaunchedTarget(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.AgentLaunchedAt = stale.LaunchTime().Add(-time.Second)
	called := false
	services := focusruntime.Writer{Store: m.services.store, Driver: m.services.tmux, Paste: func(*tmux.Driver, string, string) error { called = true; return nil }}
	_, err := services.Run(focusruntime.Request{Kind: focusruntime.Paste, Session: stale, Text: "draft"})
	if err == nil || called {
		t.Fatalf("relaunched target received stale input: called=%v err=%v", called, err)
	}
}

func TestBlockedInputWorkerKeepsUpdateResponsiveAndDrainsOnQuit(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	session := m.sessionRows()[0]
	started, release := make(chan struct{}), make(chan struct{})
	original := pasteFocused
	pasteFocused = func(*tmux.Driver, string, string) error { close(started); <-release; return nil }
	t.Cleanup(func() { pasteFocused = original })
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Paste, Session: session, Text: "draft"})
	command := m.nextEffectCmd()
	completed := make(chan tea.Msg, 1)
	go func() { completed <- command() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("input worker did not start")
	}
	// Model preparation and a quit request must return without waiting for transport.
	updated, _ := m.Update(struct{}{})
	if updated != m {
		close(release)
		t.Fatal("Update replaced model")
	}
	_, quit := m.requestQuit()
	if quit != nil || m.effects.Input.Active == nil {
		close(release)
		t.Fatal("quit abandoned active input")
	}
	close(release)
	select {
	case result := <-completed:
		_, quit = m.Update(result)
		if m.effects.Input.Active != nil || quit == nil {
			t.Fatal("completed input did not release quit drain")
		}
	case <-time.After(time.Second):
		t.Fatal("released input did not finish")
	}
}

func TestTypedPromptReceiptRefusesRelaunchedTarget(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.AgentLaunchedAt = stale.LaunchTime().Add(-time.Second)
	services := focusruntime.Writer{Store: m.services.store, Driver: m.services.tmux}
	_, err := services.Run(focusruntime.Request{Kind: focusruntime.Prompt, Session: stale, Text: "old incarnation draft"})
	if err == nil {
		t.Fatal("stale prompt receipt accepted")
	}
	current, err := m.services.store.Get(stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.LastPrompt != "" {
		t.Fatalf("stale LastPrompt: %q", current.LastPrompt)
	}
}

func TestInputRefusesMovedSocket(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.TmuxSocket = "another-owner-socket"
	called := false
	services := focusruntime.Writer{Store: m.services.store, Driver: m.services.tmux, Paste: func(*tmux.Driver, string, string) error { called = true; return nil }}
	_, err := services.Run(focusruntime.Request{Kind: focusruntime.Paste, Session: stale, Text: "draft"})
	if err == nil || called {
		t.Fatalf("moved target received stale input: called=%v err=%v", called, err)
	}
}

func TestInputRefusesChangedCreationEvenWithSameLaunch(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "input-identity", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.AgentLaunchedAt = stale.LaunchTime()
	stale.CreatedAt = stale.CreatedAt.Add(-time.Second)
	called := false
	services := focusruntime.Writer{Store: m.services.store, Driver: m.services.tmux, Paste: func(*tmux.Driver, string, string) error { called = true; return nil }}
	_, err := services.Run(focusruntime.Request{Kind: focusruntime.Paste, Session: stale, Text: "old row"})
	if err == nil || called {
		t.Fatalf("changed creation received input: called=%v err=%v", called, err)
	}
}
