package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiquicksend "github.com/YoanWai/agent-manager/internal/ui/quick/send"
	tea "github.com/charmbracelet/bubbletea"
)

func TestQuickSendRefusalRestoresAcceptedImages(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "target", t.TempDir(), "")
	m.selectSessionRow(t, "target")
	m.openQuickMode()
	path := tempImage(t, "retry.png")
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: path}}
	m.quick().Input.SetValue(composer.Token(1) + " retry this")
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		return tmux.SendResult{Phase: tmux.SendPhaseLoaded}, errors.New("submit refused")
	}

	_, cmd := m.submitQuick()
	if cmd == nil || len(m.quick().Attachments) != 0 {
		t.Fatalf("accepted send was not deferred with moved images: cmd=%v images=%v", cmd != nil, m.quick().Attachments)
	}
	m.applyTestMsg(t, cmd())
	if got := m.quick().Input.Value(); got != composer.Token(1)+" retry this" {
		t.Fatalf("refused draft = %q", got)
	}
	if len(m.quick().Attachments) != 1 || m.quick().Attachments[0].Path != path {
		t.Fatalf("refused images = %+v", m.quick().Attachments)
	}
	if !strings.Contains(m.errBar.text, "was not sent") {
		t.Fatalf("refusal = %q", m.errBar.text)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("returned image was removed: %v", err)
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
