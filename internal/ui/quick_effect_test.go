package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func quickSendTestServices(m *Model) effectServices {
	return effectServices{
		cfg:    m.services.cfg,
		store:  m.services.store,
		driver: m.services.tmux,
		quickSessionExists: func(*tmux.Driver, string) (bool, error) {
			return true, nil
		},
	}
}

func TestQuickSendWorkerRejectsChangedIncarnationBeforeTransport(t *testing.T) {
	for _, change := range []struct {
		name string
		run  func(*Model, string) error
	}{
		{name: "relaunched", run: func(m *Model, id string) error {
			return m.services.store.SetAgentLaunchedAt(id, time.Now().Add(time.Minute))
		}},
		{name: "moved socket", run: func(m *Model, id string) error {
			return m.services.store.SetTmuxSocket(id, "replacement-owner")
		}},
		{name: "changed tool", run: func(m *Model, id string) error {
			return m.services.store.UpdateTool(id, "ready-tool")
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "target", t.TempDir(), "")
			captured := m.sessionRows()[0]
			if err := change.run(m, captured.ID); err != nil {
				t.Fatal(err)
			}
			called := false
			services := quickSendTestServices(m)
			services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
				called = true
				return tmux.SendResult{}, nil
			}
			_, err := services.runQuickSend(quickSendRequest{session: captured, text: "stale"})
			if err == nil || called {
				t.Fatalf("changed target received prompt: called=%t err=%v", called, err)
			}
		})
	}
}

func TestQuickSendWorkerClassifiesTransportAndRecordsOnlyConfirmed(t *testing.T) {
	tests := []struct {
		name    string
		result  tmux.SendResult
		sendErr error
		want    quickSendOutcome
	}{
		{name: "refused", result: tmux.SendResult{Phase: tmux.SendPhaseLoaded}, sendErr: errors.New("submit refused"), want: quickSendRefused},
		{name: "uncertain", result: tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, sendErr: errors.New("reply lost"), want: quickSendUncertain},
		{name: "confirmed", result: tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, want: quickSendConfirmed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "target", t.TempDir(), "")
			captured := m.sessionRows()[0]
			if err := m.services.store.SetAcked(captured.ID, true); err != nil {
				t.Fatal(err)
			}
			services := quickSendTestServices(m)
			services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
				return test.result, test.sendErr
			}
			value, err := services.runQuickSend(quickSendRequest{session: captured, text: "captured prompt"})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			result := value.(quickSendResult)
			if result.outcome != test.want {
				t.Fatalf("outcome = %v, want %v", result.outcome, test.want)
			}
			stored, err := m.services.store.Get(captured.ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == quickSendConfirmed {
				if stored.Acked || stored.LastPrompt != "captured prompt" {
					t.Fatalf("confirmed metadata = acked %t prompt %q", stored.Acked, stored.LastPrompt)
				}
			} else if !stored.Acked || stored.LastPrompt != "" {
				t.Fatalf("unconfirmed send changed metadata: acked %t prompt %q", stored.Acked, stored.LastPrompt)
			}
		})
	}
}

func TestQuickSendRefusalRestoresAcceptedImages(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "target", t.TempDir(), "")
	m.selectSessionRow(t, "target")
	m.openQuickMode()
	path := tempImage(t, "retry.png")
	m.quick.attachments = []imageAttachment{{id: 1, path: path}}
	m.quick.input.SetValue(imageToken(1) + " retry this")
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		return tmux.SendResult{Phase: tmux.SendPhaseLoaded}, errors.New("submit refused")
	}

	_, cmd := m.submitQuick()
	if cmd == nil || len(m.quick.attachments) != 0 {
		t.Fatalf("accepted send was not deferred with moved images: cmd=%v images=%v", cmd != nil, m.quick.attachments)
	}
	m.applyTestMsg(t, cmd())
	if got := m.quick.input.Value(); got != imageToken(1)+" retry this" {
		t.Fatalf("refused draft = %q", got)
	}
	if len(m.quick.attachments) != 1 || m.quick.attachments[0].path != path {
		t.Fatalf("refused images = %+v", m.quick.attachments)
	}
	if !strings.Contains(m.errBar.text, "was not sent") {
		t.Fatalf("refusal = %q", m.errBar.text)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("returned image was removed: %v", err)
	}
}

func TestQuickSendUncertainClearsAcceptedDraftWithoutRetry(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "target", t.TempDir(), "")
	m.selectSessionRow(t, "target")
	m.openQuickMode()
	path := tempImage(t, "uncertain.png")
	m.quick.attachments = []imageAttachment{{id: 1, path: path}}
	m.quick.input.SetValue(imageToken(1) + " do this once")
	calls := 0
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		calls++
		return tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, errors.New("reply lost")
	}

	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if calls != 1 || m.quick.input.Value() != "" {
		t.Fatalf("uncertain send calls=%d draft=%q", calls, m.quick.input.Value())
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("uncertain send returned images for a possible retry: %+v", m.quick.attachments)
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
	m.quick.input.SetValue("race with relaunch")
	m.services.quickSessionExists = func(*tmux.Driver, string) (bool, error) { return true, nil }
	m.services.quickSendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		if err := m.services.store.SetAgentLaunchedAt(captured.ID, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
	}

	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if m.quick.input.Value() != "" {
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
	m := buildModel(t)
	path := tempImage(t, "stale-refusal.png")
	request := quickSendRequest{
		composerGen: m.quick.gen + 1,
		images:      []imageAttachment{{id: 1, path: path}},
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
	m.quick.input.SetValue("first")
	_, first := m.submitQuick()
	if first == nil {
		t.Fatal("first send did not activate its effect")
	}
	if _, duplicate := m.submitQuick(); duplicate != nil || m.errBar.text != "this prompt is already being sent" {
		t.Fatalf("duplicate accepted: cmd=%v err=%q", duplicate != nil, m.errBar.text)
	}
	m.openQuickMode()
	m.quick.input.SetValue("second")
	if _, second := m.submitQuick(); second != nil || len(m.effects.pending) != 1 {
		t.Fatalf("second send did not queue behind the first: cmd=%v pending=%d", second != nil, len(m.effects.pending))
	}

	completed := make(chan tea.Msg, 1)
	go func() { completed <- first() }()
	<-started
	updated, _ := m.Update(struct{}{})
	if updated != m {
		close(release)
		t.Fatal("Update replaced the model while quick transport was blocked")
	}
	if _, quit := m.requestQuit(); quit != nil || !m.effects.quitting {
		close(release)
		t.Fatal("quit abandoned the accepted quick-send queue")
	}
	close(release)
	msg := <-completed
	updated, _ = m.Update(msg)
	m = updated.(*Model)
	if got := m.quick.input.Value(); got != "second" {
		t.Fatalf("first completion replaced newer composer: %q", got)
	}
	if m.effects.active == nil {
		t.Fatal("quit drain did not activate the queued quick send")
	}
	updated, quit := m.Update(m.effects.active.command())
	m = updated.(*Model)
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("send order = %q", calls)
	}
	if m.effects.active != nil || quit == nil {
		t.Fatal("queued quick send did not finish the quit drain")
	}
}
