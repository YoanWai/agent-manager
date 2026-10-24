package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInstallStartDefersWorkOffUpdate(t *testing.T) {
	m := buildModel(t)
	installFixture(t, m, "sleep 30")

	started := make(chan struct{})
	release := make(chan struct{})
	home := t.TempDir()
	original := uiinstall.HomeDir
	uiinstall.HomeDir = func() (string, error) {
		close(started)
		<-release
		return home, nil
	}
	t.Cleanup(func() { uiinstall.HomeDir = original })

	pressInLaunchHint(t, m, 'i')
	if m.effects.Main.Active == nil {
		t.Fatal("install press queued no effect")
	}
	if _, ok := m.effects.Main.Active.Request.(uiinstall.StartRequest); !ok {
		t.Fatalf("active effect = %T, want uiinstall.StartRequest", m.effects.Main.Active.Request)
	}
	if m.ledger.install.Pending != nil || storeRows(t, m) != 0 {
		t.Fatalf("install ran on Update: pending=%+v rows=%d", m.ledger.install.Pending, storeRows(t, m))
	}

	completed := make(chan tea.Msg, 1)
	go func() { completed <- m.effects.Main.Active.Command() }()
	<-started
	updated := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on the install worker")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)
	if m.ledger.install.Pending == nil || storeRows(t, m) != 1 {
		t.Fatalf("completion = pending %+v, rows %d; want one running install", m.ledger.install.Pending, storeRows(t, m))
	}
}

func TestInstallStartRefusesDuplicateWhileQueued(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	installFixture(t, m, "sleep 30")

	pressInLaunchHint(t, m, 'i')
	first := m.effects.Main.Active
	pressInLaunchHint(t, m, 'i')

	if m.effects.Main.Active != first || len(m.effects.Main.Pending) != 0 {
		t.Fatalf("duplicate install changed the queue: active=%p first=%p pending=%d", m.effects.Main.Active, first, len(m.effects.Main.Pending))
	}
	if !strings.Contains(m.errBar.text, "already") {
		t.Fatalf("duplicate status = %q, want the in-progress install named", m.errBar.text)
	}
}

func TestInstallStartKeepsDurableShellRowWhenSendIsRefused(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, "true")
	original := uiinstall.SendText
	calls := 0
	uiinstall.SendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		calls++
		if calls == 1 {
			return tmux.SendResult{Phase: tmux.SendPhaseLoadStarted}, errors.New("fixture send failed")
		}
		return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
	}
	t.Cleanup(func() { uiinstall.SendText = original })

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))

	if m.ledger.install.Pending != nil {
		t.Fatalf("failed send left a pending install: %+v", m.ledger.install.Pending)
	}
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the same setup dialog kept", m.screenID())
	}
	if storeRows(t, m) != 1 || len(m.workspace.sessions) != 1 {
		t.Fatalf("durable shell row was not reconciled: store=%d workspace=%d", storeRows(t, m), len(m.workspace.sessions))
	}
	if !m.services.tmux.Exists(m.workspace.sessions[0].ID) {
		t.Fatal("durable installer shell pane is missing")
	}
	if !strings.Contains(m.errBar.text, "fixture send failed") {
		t.Fatalf("status = %q, want send failure", m.errBar.text)
	}
	if len(hintFix(m).Images) != 1 || !imageExists(t, image) {
		t.Fatal("the retained dialog lost the refused prompt image")
	}
	sess := m.workspace.sessions[0]
	for _, path := range []string{
		m.services.hooks.InstallStatusFile(sess.ID),
		filepath.Join(filepath.Dir(m.services.hooks.InstallStatusFile(sess.ID)), sess.ID+".install.sh"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("refused transport kept %s: %v", path, err)
		}
	}

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	if calls != 2 {
		t.Fatalf("retry sent %d commands, want two total attempts", calls)
	}
}

func TestInstallStartKeepsUncertainSendWithoutBlindRetry(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, "true")
	original := uiinstall.SendText
	calls := 0
	uiinstall.SendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		calls++
		return tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, errors.New("fixture post-paste failure")
	}
	t.Cleanup(func() { uiinstall.SendText = original })

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))

	if m.ledger.install.Pending == nil {
		t.Fatal("uncertain send lost the pending install tracker")
	}
	if !m.onList() {
		t.Fatalf("screen = %v, want the retry dialog closed", m.screenID())
	}
	if _, err := os.Stat(m.ledger.install.Pending.Script); err != nil {
		t.Fatalf("uncertain send removed its script: %v", err)
	}
	if !imageExists(t, image) || len(m.ledger.install.Pending.Images) != 1 {
		t.Fatal("uncertain send lost its prompt image ownership")
	}
	if !strings.Contains(m.errBar.text, "may be running") || !strings.Contains(m.errBar.text, "inspect") {
		t.Fatalf("status = %q, want explicit uncertain-send guidance", m.errBar.text)
	}
	hintStartInstall(m)
	if calls != 1 {
		t.Fatalf("uncertain install was sent %d times, want no blind retry", calls)
	}
}

func TestInstallStartCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	_, firstImage := installFixture(t, m, "sleep 30")
	pressInLaunchHint(t, m, 'i')
	first := m.effects.Main.Active.Command

	secondImage := tempImage(t, "second.png")
	m.openLaunchHint(uiinstall.Fix{
		Text:    "second setup step",
		Command: "true",
		Script:  "true",
		Binary:  "second-cli",
		Images:  []composer.Attachment{{ID: 2, Path: secondImage}},
	})
	newGen := m.gens.dialog

	m.applyTestMsg(t, first())
	m.drainEffects(t)

	if !hintOpen(m) || m.gens.dialog != newGen || hintFix(m).Binary != "second-cli" {
		t.Fatalf("completion replaced newer dialog: screen = %v gen=%d fix=%+v", m.screenID(), m.gens.dialog, hintFix(m))
	}
	if m.ledger.install.Pending == nil || m.ledger.install.Pending.Binary != "am-fake-cli" {
		t.Fatalf("first install was not reconciled behind the dialog: %+v", m.ledger.install.Pending)
	}
	if len(hintFix(m).Images) != 1 || !imageExists(t, secondImage) {
		t.Fatal("newer dialog lost its image")
	}
	if len(m.ledger.install.Pending.Images) != 1 || !imageExists(t, firstImage) {
		t.Fatal("pending install lost the first prompt image")
	}
}

func TestInstallStartStaleFailureDefersImageCleanupToCommand(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	image := tempImage(t, "stale.png")
	m.gens.dialog = 2
	m.presentLaunchHint(uiinstall.Fix{Binary: "newer-cli"})

	cmd := m.ledger.install.Started(installRoot{m},
		uiinstall.StartRequest{Gen: 1, Images: []composer.Attachment{{ID: 1, Path: image}}},
		uiinstall.StartResult{},
		errors.New("fixture start failed"),
	)

	if !imageExists(t, image) {
		t.Fatal("applying the stale completion performed image I/O")
	}
	if cmd == nil {
		t.Fatal("stale completion returned no captured cleanup command")
	}
	cmd()
	if imageExists(t, image) {
		t.Fatal("captured cleanup command kept the stale prompt image")
	}
	if !hintOpen(m) || hintFix(m).Binary != "newer-cli" {
		t.Fatalf("stale completion replaced the newer dialog: screen = %v fix=%+v", m.screenID(), hintFix(m))
	}
}

func TestInstallBlocksQuitUntilPendingRetrySettles(t *testing.T) {
	t.Run("start queued", func(t *testing.T) {
		m := buildModel(t)
		installFixture(t, m, "true")
		pressInLaunchHint(t, m, 'i')

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = updated.(*Model)
		if cmd != nil || m.effects.Quitting {
			t.Fatalf("quit was accepted during install start: cmd=%v quitting=%t", cmd, m.effects.Quitting)
		}
		if m.effects.Main.Active == nil {
			t.Fatal("refused quit discarded the accepted install start")
		}
		if !strings.Contains(m.errBar.text, "starting") || !strings.Contains(m.errBar.text, "wait") {
			t.Fatalf("status = %q, want actionable starting guidance", m.errBar.text)
		}
	})

	t.Run("pending settles and retries", func(t *testing.T) {
		m := buildModel(t)
		retryName, _ := installFixture(t, m, fakeInstallCommand(t))
		m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
		if m.ledger.install.Pending == nil {
			t.Fatal("fixture install did not become pending")
		}

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = updated.(*Model)
		if cmd != nil || m.effects.Quitting {
			t.Fatalf("quit was accepted during pending install: cmd=%v quitting=%t", cmd, m.effects.Quitting)
		}
		if !strings.Contains(m.errBar.text, "kill") || !strings.Contains(m.errBar.text, m.ledger.install.Pending.Name) {
			t.Fatalf("status = %q, want the installer shell and lifecycle guidance", m.errBar.text)
		}

		waitForInstallToSettle(t, m)
		if !hasSessionNamed(m, retryName) {
			t.Fatalf("blocked quit lost the captured retry: %v", sessionNames(m))
		}
		_, cmd = m.requestQuit()
		if cmd == nil || !m.effects.Quitting {
			t.Fatal("quit stayed blocked after the install and retry settled")
		}
	})
}

func TestInstallSettleDefersStatusAndBinaryChecks(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "install.status")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := tempImage(t, "settle.png")
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusFile,
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}

	started := make(chan struct{})
	release := make(chan struct{})
	original := uiinstall.CheckInstalled
	uiinstall.CheckInstalled = func(string) error {
		close(started)
		<-release
		return nil
	}
	t.Cleanup(func() { uiinstall.CheckInstalled = original })

	m.settleInstall()
	if m.effects.Main.Active != nil {
		t.Fatal("settle started its worker instead of only queuing it")
	}
	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("settle queue = %d, want one request", len(m.effects.Main.Pending))
	}
	if _, err := os.Stat(statusFile); err != nil {
		t.Fatalf("Update-path settle touched the status file: %v", err)
	}
	m.nextEffectCmd()
	completed := make(chan tea.Msg, 1)
	go func() { completed <- m.effects.Main.Active.Command() }()
	<-started
	updated := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 151, Height: 46})
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on the settle worker")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)

	if m.ledger.install.Pending != nil {
		t.Fatalf("completed settle still pending: %+v", m.ledger.install.Pending)
	}
	if imageExists(t, image) {
		t.Fatal("successful install with no retry kept an unowned prompt image")
	}
	for _, path := range []string{statusFile, script} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed by the worker, stat err = %v", path, err)
		}
	}
}

func TestInstallSettleKeepsTrackerOnTransientStatusReadFailure(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	statusDir := filepath.Join(dir, "status-dir")
	if err := os.Mkdir(statusDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusDir, "keep"), []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := tempImage(t, "transient.png")
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusDir,
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}

	m.settleInstall()
	m.drainEffects(t)

	if m.ledger.install.Pending == nil {
		t.Fatal("transient status read failure cleared the install tracker")
	}
	for _, path := range []string{statusDir, script, image} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("transient observer failure removed %s: %v", path, err)
		}
	}
	if !strings.Contains(m.errBar.text, "read") || !strings.Contains(m.errBar.text, m.ledger.install.Pending.Name) {
		t.Fatalf("status = %q, want the observer failure and installer named", m.errBar.text)
	}

	statusFile := filepath.Join(dir, "install.status")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.ledger.install.Pending.StatusFile = statusFile
	original := uiinstall.CheckInstalled
	uiinstall.CheckInstalled = func(string) error { return nil }
	t.Cleanup(func() { uiinstall.CheckInstalled = original })
	m.settleInstall()
	m.drainEffects(t)
	if m.ledger.install.Pending != nil {
		t.Fatalf("later successful observation kept pending install: %+v", m.ledger.install.Pending)
	}
}

func TestInstallSettleRetriesTransientInstalledCheck(t *testing.T) {
	m := buildModel(t)
	retryName, image := installFixture(t, m, "true")
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "install.status")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusFile,
		Script:     script,
		Retry:      hintFix(m).Retry,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}
	checks := 0
	original := uiinstall.CheckInstalled
	uiinstall.CheckInstalled = func(string) error {
		checks++
		if checks == 1 {
			return errors.New("fixture PATH lookup unavailable")
		}
		return nil
	}
	t.Cleanup(func() { uiinstall.CheckInstalled = original })

	runSettle := func() {
		t.Helper()
		m.settleInstall()
		cmd := m.nextEffectCmd()
		if cmd == nil {
			t.Fatal("settle queued no worker")
		}
		m.applyTestMsg(t, cmd())
	}
	runSettle()

	if m.ledger.install.Pending == nil {
		t.Fatal("transient installed check cleared the install tracker")
	}
	for _, path := range []string{statusFile, script, image} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("transient installed check removed %s: %v", path, err)
		}
	}
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 0 {
		t.Fatal("transient installed check queued the captured retry")
	}
	if !strings.Contains(m.errBar.text, "looking") || !strings.Contains(m.errBar.text, "PATH lookup") {
		t.Fatalf("status = %q, want the installed-check failure", m.errBar.text)
	}

	runSettle()
	if m.ledger.install.Pending != nil {
		t.Fatalf("successful recheck kept the tracker: %+v", m.ledger.install.Pending)
	}
	if m.effects.Main.Active == nil {
		t.Fatal("successful recheck did not activate the captured retry")
	}
	request, ok := m.effects.Main.Active.Request.(spawnRequest)
	if !ok || request.name != retryName {
		t.Fatalf("successful recheck active effect = %#v, want captured retry %q", m.effects.Main.Active.Request, retryName)
	}
	for _, path := range []string{statusFile, script} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("successful recheck kept %s: %v", path, err)
		}
	}
	if !imageExists(t, image) {
		t.Fatal("queued retry lost its prompt image")
	}
}

func TestInstallSettleKeepsTrackerWhenShellLivenessIsUnknown(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := tempImage(t, "liveness.png")
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: filepath.Join(dir, "missing.status"),
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}
	original := uiinstall.SessionExists
	uiinstall.SessionExists = func(*tmux.Driver, string) (bool, error) {
		return false, errors.New("fixture liveness timeout")
	}
	t.Cleanup(func() { uiinstall.SessionExists = original })

	m.settleInstall()
	m.drainEffects(t)

	if m.ledger.install.Pending == nil {
		t.Fatal("unknown shell liveness cleared the install tracker")
	}
	for _, path := range []string{script, image} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unknown shell liveness removed %s: %v", path, err)
		}
	}
	if !strings.Contains(m.errBar.text, "liveness") || !strings.Contains(m.errBar.text, m.ledger.install.Pending.Name) {
		t.Fatalf("status = %q, want the liveness failure and installer named", m.errBar.text)
	}
	_, quit := m.requestQuit()
	if quit != nil || m.effects.Quitting {
		t.Fatal("unknown shell liveness allowed quit to discard the tracker")
	}
}

// The installer shell takes the screen from the quick bar. A start that
// lands after the dialog closed leaves the bar alone.
func TestInstallStartGivesTheInstallerShellTheKeys(t *testing.T) {
	session := store.Session{ID: "installer", Name: "install pi"}
	result := uiinstall.StartResult{
		Session: session,
		Pending: &uiinstall.Pending{SessionID: session.ID, Binary: "pi"},
	}
	for _, failure := range []error{nil, errors.New("send failed")} {
		m := buildModel(t)
		image := tempImage(t, "draft.png")
		pushQuick(m)
		m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
		m.gens.dialog = 1
		m.presentLaunchHint(uiinstall.Fix{})
		cmd := m.ledger.install.Started(installRoot{m}, uiinstall.StartRequest{Gen: 1, Binary: "pi"}, result, failure)
		if !imageExists(t, image) {
			t.Fatal("update deleted the image before the cleanup command ran")
		}
		m.applyCmd(t, cmd)
		if !m.onList() || (m.quick() != nil) {
			t.Fatalf("failure %v left mode %v quick %v", failure, m.screenID(), (m.quick() != nil))
		}
		if imageExists(t, image) {
			t.Fatal("the installer handoff left the draft image")
		}
	}

	m := buildModel(t)
	image := tempImage(t, "kept.png")
	pushQuick(m)
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
	m.gens.dialog = 2
	m.ledger.install.Started(installRoot{m}, uiinstall.StartRequest{Gen: 1, Binary: "pi"}, result, nil)
	if (m.quick() == nil) || !m.onList() {
		t.Fatalf("a start behind a closed dialog left mode %v quick %v", m.screenID(), (m.quick() != nil))
	}
	if !imageExists(t, image) {
		t.Fatal("a start behind a closed dialog deleted the draft image")
	}
}
