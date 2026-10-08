package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// A live focus watcher carries keys over its existing control client. The
// driver wrapper logs every new tmux process, so an empty log after runInput
// proves the acknowledged control command did not also take the forked path.
func TestInputKeysUseServingControlWithoutDriverFallback(t *testing.T) {
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
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	watch := newFocusWatch(driver, func(tea.Msg) {})
	t.Cleanup(watch.Close)
	watch.setFocus(sess.ID)
	deadline := time.Now().Add(5 * time.Second)
	for !watch.serving(sess.ID) {
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
	services := effectServices{store: m.services.store, driver: driver, watch: watch}
	if _, err := services.runInput(inputRequest{kind: inputKeys, session: sess, command: command}); err != nil {
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
	watch.setFocus(sess.ID)
	deadline := time.Now().Add(5 * time.Second)
	for !watch.serving(sess.ID) {
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
	services := effectServices{
		store:  m.services.store,
		driver: driver,
		watch:  watch,
		pasteInput: func(driver *tmux.Driver, id, text string) error {
			close(pasteStarted)
			return driver.Paste(id, text)
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := services.runInput(inputRequest{kind: inputKeys, session: sess, command: key})
		if err == nil {
			_, err = services.runInput(inputRequest{kind: inputPaste, session: sess, text: "after-paste"})
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
	m.mode = modeFocus
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
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	m.services.tmux = driver
	original := pasteFocused
	pasteFocused = func(*tmux.Driver, string, string) error { return os.WriteFile(log, []byte("paste\n"), 0600) }
	t.Cleanup(func() { pasteFocused = original })
	m.dispatchInput(inputRequest{kind: inputPaste, session: sess, text: "draft"})
	m.dispatchInput(inputRequest{kind: inputKeys, session: sess, command: "send-keys -t " + tmux.PaneTarget(sess.ID) + " Enter"})
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
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.AgentLaunchedAt = stale.LaunchTime().Add(-time.Second)
	called := false
	services := effectServices{store: m.services.store, driver: m.services.tmux, pasteInput: func(*tmux.Driver, string, string) error { called = true; return nil }}
	_, err := services.runInput(inputRequest{kind: inputPaste, session: stale, text: "draft"})
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
	m.dispatchInput(inputRequest{kind: inputPaste, session: session, text: "draft"})
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
	if quit != nil || m.effects.input.active == nil {
		close(release)
		t.Fatal("quit abandoned active input")
	}
	close(release)
	select {
	case result := <-completed:
		_, quit = m.Update(result)
		if m.effects.input.active != nil || quit == nil {
			t.Fatal("completed input did not release quit drain")
		}
	case <-time.After(time.Second):
		t.Fatal("released input did not finish")
	}
}

func TestTypedPromptReceiptRefusesRelaunchedTarget(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.AgentLaunchedAt = stale.LaunchTime().Add(-time.Second)
	services := effectServices{store: m.services.store, driver: m.services.tmux}
	_, err := services.runInput(inputRequest{kind: inputPrompt, session: stale, text: "old incarnation draft"})
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
	m := buildModel(t)
	createSession(t, m, "input-target", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.TmuxSocket = "another-owner-socket"
	called := false
	services := effectServices{store: m.services.store, driver: m.services.tmux, pasteInput: func(*tmux.Driver, string, string) error { called = true; return nil }}
	_, err := services.runInput(inputRequest{kind: inputPaste, session: stale, text: "draft"})
	if err == nil || called {
		t.Fatalf("moved target received stale input: called=%v err=%v", called, err)
	}
}

func TestInputRefusesChangedCreationEvenWithSameLaunch(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "input-identity", t.TempDir(), "")
	stale := m.sessionRows()[0]
	stale.AgentLaunchedAt = stale.LaunchTime()
	stale.CreatedAt = stale.CreatedAt.Add(-time.Second)
	called := false
	services := effectServices{store: m.services.store, driver: m.services.tmux, pasteInput: func(*tmux.Driver, string, string) error { called = true; return nil }}
	_, err := services.runInput(inputRequest{kind: inputPaste, session: stale, text: "old row"})
	if err == nil || called {
		t.Fatalf("changed creation received input: called=%v err=%v", called, err)
	}
}
