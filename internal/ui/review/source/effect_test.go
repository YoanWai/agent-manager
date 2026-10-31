package source

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

type scriptedWriter struct {
	alive      bool
	sendResult tmux.SendResult
	sendErr    error
	sendCalls  []string
}

func (w *scriptedWriter) Exists(id string) (bool, error) { return w.alive, nil }

func (w *scriptedWriter) SendText(id, text string) (tmux.SendResult, error) {
	if w.sendErr != nil {
		return w.sendResult, w.sendErr
	}
	w.sendCalls = append(w.sendCalls, text)
	return w.sendResult, nil
}

type probeFailingWriter struct {
	probeErr error
	sent     []string
}

func (w *probeFailingWriter) Exists(id string) (bool, error) { return false, w.probeErr }

func (w *probeFailingWriter) SendText(id, text string) (tmux.SendResult, error) {
	w.sent = append(w.sent, text)
	return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
}

func createSendSession(t *testing.T, st *store.Store, id string) store.Session {
	t.Helper()
	if err := st.CreateSession(store.Session{ID: id, Name: id, Tool: "pi", Cwd: "/w", TmuxSocket: "fixture"}); err != nil {
		t.Fatal(err)
	}
	session, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func sendRequest(request uireview.SendRequest, session store.Session) Request {
	return Request{
		Op: Send, TargetID: request.Target.ID, TargetName: request.Target.Name,
		RepoRoot: request.RepoRoot, State: request.State, PreviousState: request.PreviousState,
		Prompt: request.Prompt, CommentIDs: request.CommentIDs, PreviousRound: request.PreviousRound,
		Round: request.Round, Count: request.Count, SendTarget: CaptureSendTarget(session),
	}
}

func TestSendDeadSessionWritesNothing(t *testing.T) {
	st := openTestStore(t)
	session := createSendSession(t, st, "dead1")
	writer := &scriptedWriter{alive: false}
	req := sendRequest(uireview.SendRequest{
		Target:   uireview.Target{ID: "dead1", Name: "dead"},
		RepoRoot: "/repo", Prompt: "prompt",
		State:         uireview.SavedState{Round: uireview.Round{Number: 1}},
		PreviousState: uireview.SavedState{},
		Round:         1, Count: 1,
	}, session)
	result, err := Run(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendRefused || result.Send.Err == nil {
		t.Fatalf("dead session outcome = %+v", result.Send)
	}
	if !strings.Contains(result.Send.Err.Error(), "revive or R to restart") {
		t.Fatalf("dead session hint = %q", result.Send.Err)
	}
	state, err := st.ReviewState("dead1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 0 {
		t.Fatalf("dead session persisted a round: %+v", state)
	}
}

func TestSendRollsBackWhenDeliveryIsRefused(t *testing.T) {
	st := openTestStore(t)
	session := createSendSession(t, st, "s1")
	writer := &scriptedWriter{
		alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseLoaded}, sendErr: errors.New("send failed"),
	}
	req := sendRequest(uireview.SendRequest{
		Target:   uireview.Target{ID: "s1", Name: "one"},
		RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{
			Round:    uireview.Round{Number: 2},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "new", Round: 2}},
		},
		PreviousState: uireview.SavedState{
			Round:    uireview.Round{Number: 1},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "old", Round: 1}},
		},
		CommentIDs: []string{"c1"}, Round: 2, Count: 1,
	}, session)
	result, err := Run(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendRefused || result.Send.Err == nil || !strings.Contains(result.Send.Err.Error(), "send failed") {
		t.Fatalf("failed send outcome = %+v", result.Send)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 1 || len(state.Comments) != 1 || state.Comments[0].Text != "old" {
		t.Fatalf("state not rolled back: %+v", state)
	}
}

func TestSendKeepsRoundWhenDeliveryIsUncertain(t *testing.T) {
	st := openTestStore(t)
	session := createSendSession(t, st, "s1")
	writer := &scriptedWriter{
		alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, sendErr: errors.New("tmux reply lost"),
	}
	req := sendRequest(uireview.SendRequest{
		Target: uireview.Target{ID: "s1", Name: "one"}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{
			Round:    uireview.Round{Number: 2},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "new", Round: 2}},
		},
		PreviousState: uireview.SavedState{
			Round:    uireview.Round{Number: 1},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "old", Round: 1}},
		},
		CommentIDs: []string{"c1"}, Round: 2, Count: 1,
	}, session)
	result, err := Run(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendUncertain || result.Send.Err == nil || !strings.Contains(result.Send.Err.Error(), "may have reached") {
		t.Fatalf("uncertain send outcome = %+v", result.Send)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 1 || state.Comments[0].Text != "new" {
		t.Fatalf("uncertain send restored resendable state: %+v", state)
	}
}

func TestSendRefusesUnconfirmedSuccess(t *testing.T) {
	st := openTestStore(t)
	session := createSendSession(t, st, "s1")
	writer := &scriptedWriter{alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseLoaded}}
	request := sendRequest(uireview.SendRequest{
		Target: uireview.Target{ID: "s1", Name: "one"}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}}, Round: 1, Count: 1,
	}, session)

	result, err := Run(request, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendRefused || result.Send.Err == nil || !strings.Contains(result.Send.Err.Error(), "not confirmed submitted") {
		t.Fatalf("unconfirmed success outcome = %+v", result.Send)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil || state.Round.Number != 0 {
		t.Fatalf("unconfirmed success persisted a round: state=%+v err=%v", state, err)
	}
}

func TestSendRefusesRelaunchedTargetBeforePersistenceOrTransport(t *testing.T) {
	st := openTestStore(t)
	session := createSendSession(t, st, "s1")
	request := sendRequest(uireview.SendRequest{
		Target: uireview.Target{ID: "s1", Name: "one"}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}}, Round: 1, Count: 1,
	}, session)
	if err := st.SetAgentLaunchedAt("s1", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	writer := &scriptedWriter{alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseSubmitted}}

	result, err := Run(request, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendRefused || result.Send.Err == nil || !strings.Contains(result.Send.Err.Error(), "target changed") {
		t.Fatalf("relaunch outcome = %+v", result.Send)
	}
	if len(writer.sendCalls) != 0 {
		t.Fatalf("relaunch guard typed into replacement pane: %v", writer.sendCalls)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 0 {
		t.Fatalf("relaunch guard persisted a round: %+v", state)
	}
}

func TestSendProbeErrorPersistsNothing(t *testing.T) {
	st := openTestStore(t)
	session := createSendSession(t, st, "s1")
	writer := &probeFailingWriter{probeErr: errors.New("tmux probe failed")}
	req := sendRequest(uireview.SendRequest{
		Target:   uireview.Target{ID: "s1", Name: "one"},
		RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}},
		Round: 1, Count: 1,
	}, session)
	result, err := Run(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendRefused || result.Send.Err == nil || result.Send.Err.Error() != "tmux probe failed" {
		t.Fatalf("probe failure outcome = %+v", result.Send)
	}
	if len(writer.sent) != 0 {
		t.Fatalf("probe failure delivered text: %v", writer.sent)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 0 {
		t.Fatalf("probe failure persisted a round: %+v", state)
	}
}

func TestSendPersistsRoundAndClearsAck(t *testing.T) {
	st := openTestStore(t)
	writer := &scriptedWriter{alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseSubmitted}}
	session := createSendSession(t, st, "s1")
	if err := st.SetAcked("s1", true); err != nil {
		t.Fatal(err)
	}
	req := sendRequest(uireview.SendRequest{
		Target:   uireview.Target{ID: "s1", Name: "one"},
		RepoRoot: "/repo", Prompt: "the prompt",
		State: uireview.SavedState{
			Round:    uireview.Round{Number: 2},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "new", Round: 2}},
		},
		CommentIDs: []string{"c1"}, Round: 2, Count: 1,
	}, session)
	result, err := Run(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Send.Outcome != uireview.SendConfirmed || result.Send.Err != nil {
		t.Fatalf("send outcome = %+v", result.Send)
	}
	if len(writer.sendCalls) != 1 || writer.sendCalls[0] != "the prompt" {
		t.Fatalf("delivered prompts = %v", writer.sendCalls)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 1 {
		t.Fatalf("round not persisted: %+v", state)
	}
	if result.Send.AckErr != nil {
		t.Fatal(result.Send.AckErr)
	}
}

func TestSavePersistsCapturedStatePreservingHandled(t *testing.T) {
	st := openTestStore(t)
	req := Request{
		Op: Save, TargetID: "s1", RepoRoot: "/repo",
		State: uireview.SavedState{
			Reviewed: map[string]uint64{"uncommitted\x00a.go": 7},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "t", Round: 2, Resolved: false}},
			Round:    uireview.Round{Number: 2},
		},
	}
	if err := st.MergeReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{{ID: "c1", File: "a.go", Line: 1, Text: "t", Round: 2, Resolved: true}},
		Round:    store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Run(req, st, &scriptedWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Save.Err != nil {
		t.Fatal(result.Save.Err)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Reviewed) != 1 || state.Reviewed["uncommitted\x00a.go"] != 7 {
		t.Fatalf("reviewed marks = %+v", state.Reviewed)
	}
	if len(state.Comments) != 1 || !state.Comments[0].Resolved {
		t.Fatalf("agent-resolved flag lost: %+v", state.Comments)
	}
}

func TestHandleReportsFoundFlag(t *testing.T) {
	st := openTestStore(t)
	if err := st.MergeReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{{ID: "c1", File: "a.go", Line: 1, Text: "t", Round: 2}},
		Round:    store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		commentID string
		found     bool
	}{
		{"c1", true},
		{"missing", false},
	} {
		result, err := Run(Request{
			Op: Handle, TargetID: "s1", RepoRoot: "/repo",
			CommentID: tc.commentID, Handled: true,
		}, st, &scriptedWriter{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Handle.Err != nil || result.Handle.Found != tc.found {
			t.Fatalf("handle %s = %+v", tc.commentID, result.Handle)
		}
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || !state.Comments[0].Resolved {
		t.Fatalf("handled mark not persisted: %+v", state.Comments)
	}
}

func TestStatusReportsOnlyRoundComments(t *testing.T) {
	st := openTestStore(t)
	if err := st.MergeReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{
			{ID: "sent", File: "a.go", Line: 1, Text: "t", Round: 2, Resolved: true},
			{ID: "draft", File: "a.go", Line: 2, Text: "d", Round: 0},
		},
		Round: store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Run(Request{Op: Status, TargetID: "s1", RepoRoot: "/repo", Generation: 3}, st, &scriptedWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.Err != nil || result.Status.Generation != 3 {
		t.Fatalf("status = %+v", result.Status)
	}
	if len(result.Status.Handled) != 1 || !result.Status.Handled["sent"] {
		t.Fatalf("handled = %+v", result.Status.Handled)
	}
}

func TestNormalizePersistsCurrentShape(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{{File: "a.go", Line: 1, Text: "t", Round: 2}},
		Round:    store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	req := Request{Op: Normalize, TargetID: "s1", RepoRoot: "/repo"}
	for i := 0; i < 2; i++ {
		if _, err := Run(req, st, &scriptedWriter{}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || state.Comments[0].ID == "" || state.Comments[0].Point != 1 {
		t.Fatalf("normalized state = %+v", state.Comments)
	}
	if err := st.SetReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{{ID: "zz", File: "b.go", Line: 3, Text: "newer", Round: 5, Point: 1}},
		Round:    store.ReviewRound{Number: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(req, st, &scriptedWriter{}); err != nil {
		t.Fatal(err)
	}
	state, err = st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || state.Comments[0].ID != "zz" || state.Comments[0].Text != "newer" {
		t.Fatalf("newer state clobbered by normalize: %+v", state.Comments)
	}
}

func TestSetBaseWorkerUsesCapturedValues(t *testing.T) {
	st := openTestStore(t)
	result, err := Run(Request{
		Op: SetBase, TargetID: "s1", RepoRoot: "/captured/repo", BaseRef: "feature",
		Generation: 7, SourceRepo: "/raw/repo",
	}, st, &scriptedWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Base.TargetID != "s1" || result.Base.RepoRoot != "/captured/repo" || result.Base.SourceRepo != "/raw/repo" || result.Base.Ref != "feature" || result.Base.Generation != 7 || result.Base.Err != nil {
		t.Fatalf("base result lost captured identity: %+v", result.Base)
	}
	stored, err := st.ReviewBase("s1", "/captured/repo")
	if err != nil || stored != "feature" {
		t.Fatalf("worker persisted base=%q err=%v", stored, err)
	}
}
