package quick

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// sendStore opens a store holding one session the send may target.
func sendStore(t *testing.T) (*store.Store, store.Session) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	sess := store.Session{
		ID:         "s1",
		Name:       "one",
		Tool:       "claude",
		Cwd:        t.TempDir(),
		CreatedAt:  time.Now(),
		Status:     status.Idle,
		TmuxSocket: "amtest",
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	current, err := st.Get(sess.ID)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	return st, current
}

func sendServices(t *testing.T, st *store.Store) Services {
	t.Helper()
	return Services{
		Store:  st,
		Config: config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat", DefaultStatus: status.Idle}}},
		SessionExists: func(*tmux.Driver, string) (bool, error) {
			return true, nil
		},
	}
}

func TestSendRejectsChangedIncarnationBeforeTransport(t *testing.T) {
	for _, change := range []struct {
		name string
		run  func(*store.Store, string) error
	}{
		{name: "relaunched", run: func(st *store.Store, id string) error {
			return st.SetAgentLaunchedAt(id, time.Now().Add(time.Minute))
		}},
		{name: "moved socket", run: func(st *store.Store, id string) error {
			return st.SetTmuxSocket(id, "replacement-owner")
		}},
		{name: "changed tool", run: func(st *store.Store, id string) error {
			return st.UpdateTool(id, "ready-tool")
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			st, captured := sendStore(t)
			if err := change.run(st, captured.ID); err != nil {
				t.Fatal(err)
			}
			called := false
			services := sendServices(t, st)
			services.SendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
				called = true
				return tmux.SendResult{}, nil
			}
			_, err := RunSend(services, SendRequest{Session: captured, Text: "stale"})
			if err == nil || called {
				t.Fatalf("changed target received prompt: called=%t err=%v", called, err)
			}
		})
	}
}

func TestSendClassifiesTransportAndRecordsOnlyConfirmed(t *testing.T) {
	tests := []struct {
		name    string
		result  tmux.SendResult
		sendErr error
		want    SendOutcome
	}{
		{name: "refused", result: tmux.SendResult{Phase: tmux.SendPhaseLoaded}, sendErr: errors.New("submit refused"), want: SendRefused},
		{name: "uncertain", result: tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, sendErr: errors.New("reply lost"), want: SendUncertain},
		{name: "confirmed", result: tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, want: SendConfirmed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, captured := sendStore(t)
			if err := st.SetAcked(captured.ID, true); err != nil {
				t.Fatal(err)
			}
			services := sendServices(t, st)
			services.SendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
				return test.result, test.sendErr
			}
			value, err := RunSend(services, SendRequest{Session: captured, Text: "captured prompt"})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if value.Outcome != test.want {
				t.Fatalf("outcome = %v, want %v", value.Outcome, test.want)
			}
			stored, err := st.Get(captured.ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == SendConfirmed {
				if stored.Acked || stored.LastPrompt != "captured prompt" {
					t.Fatalf("confirmed metadata = acked %t prompt %q", stored.Acked, stored.LastPrompt)
				}
			} else if !stored.Acked || stored.LastPrompt != "" {
				t.Fatalf("unconfirmed send changed metadata: acked %t prompt %q", stored.Acked, stored.LastPrompt)
			}
		})
	}
}

func TestSendRefusesAShellTarget(t *testing.T) {
	st, captured := sendStore(t)
	services := sendServices(t, st)
	services.Config = config.Config{Tools: map[string]config.Tool{"claude": {Command: "sh", Shell: true}}}
	if _, err := RunSend(services, SendRequest{Session: captured, Text: "hello"}); err == nil {
		t.Fatal("a shell target accepted an agent prompt")
	}
}
