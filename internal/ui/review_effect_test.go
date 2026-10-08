package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func openReviewTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

type scriptedReviewWriter struct {
	alive      bool
	sendResult tmux.SendResult
	sendErr    error
	sendCalls  []string
}

func (w *scriptedReviewWriter) exists(id string) (bool, error) { return w.alive, nil }
func (w *scriptedReviewWriter) sendText(id, text string) (tmux.SendResult, error) {
	if w.sendErr != nil {
		return w.sendResult, w.sendErr
	}
	w.sendCalls = append(w.sendCalls, text)
	return w.sendResult, nil
}

func createReviewSendSession(t *testing.T, st *store.Store, id string) store.Session {
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

func reviewSendRequest(request *uireview.SendRequest, session store.Session) reviewEffectRequest {
	return reviewEffectRequest{
		op:            reviewOpSend,
		targetID:      request.Target.ID,
		targetName:    request.Target.Name,
		repoRoot:      request.RepoRoot,
		state:         request.State,
		previousState: request.PreviousState,
		prompt:        request.Prompt,
		commentIDs:    request.CommentIDs,
		previousRound: request.PreviousRound,
		round:         request.Round,
		count:         request.Count,
		sendTarget:    captureReviewSendTarget(session),
	}
}

func TestReviewSendDeadSessionWritesNothing(t *testing.T) {
	st := openReviewTestStore(t)
	session := createReviewSendSession(t, st, "dead1")
	writer := &scriptedReviewWriter{alive: false}
	req := reviewSendRequest(&uireview.SendRequest{
		Target:   uireview.Target{ID: "dead1", Name: "dead"},
		RepoRoot: "/repo", Prompt: "prompt",
		State:         uireview.SavedState{Round: uireview.Round{Number: 1}},
		PreviousState: uireview.SavedState{},
		Round:         1, Count: 1,
	}, session)
	effect, err := runReviewWithWriter(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendRefused || result.send.Err == nil || result.send.Err.Error() != deadSessionHint {
		t.Fatalf("dead session outcome = %+v", result.send)
	}
	state, err := st.ReviewState("dead1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 0 {
		t.Fatalf("dead session persisted a round: %+v", state)
	}
}

func TestReviewSendRollsBackWhenDeliveryIsRefused(t *testing.T) {
	st := openReviewTestStore(t)
	session := createReviewSendSession(t, st, "s1")
	writer := &scriptedReviewWriter{
		alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseLoaded}, sendErr: errors.New("send failed"),
	}
	req := reviewSendRequest(&uireview.SendRequest{
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
	effect, err := runReviewWithWriter(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendRefused || result.send.Err == nil || !strings.Contains(result.send.Err.Error(), "send failed") {
		t.Fatalf("failed send outcome = %+v", result.send)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 1 || len(state.Comments) != 1 || state.Comments[0].Text != "old" {
		t.Fatalf("state not rolled back: %+v", state)
	}
}

func TestReviewSendKeepsRoundWhenDeliveryIsUncertain(t *testing.T) {
	st := openReviewTestStore(t)
	session := createReviewSendSession(t, st, "s1")
	writer := &scriptedReviewWriter{
		alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, sendErr: errors.New("tmux reply lost"),
	}
	req := reviewSendRequest(&uireview.SendRequest{
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
	effect, err := runReviewWithWriter(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendUncertain || result.send.Err == nil || !strings.Contains(result.send.Err.Error(), "may have reached") {
		t.Fatalf("uncertain send outcome = %+v", result.send)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 1 || state.Comments[0].Text != "new" {
		t.Fatalf("uncertain send restored resendable state: %+v", state)
	}
}

func TestReviewSendRefusesUnconfirmedSuccess(t *testing.T) {
	st := openReviewTestStore(t)
	session := createReviewSendSession(t, st, "s1")
	writer := &scriptedReviewWriter{alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseLoaded}}
	request := reviewSendRequest(&uireview.SendRequest{
		Target: uireview.Target{ID: "s1", Name: "one"}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}}, Round: 1, Count: 1,
	}, session)

	effect, err := runReviewWithWriter(request, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendRefused || result.send.Err == nil || !strings.Contains(result.send.Err.Error(), "not confirmed submitted") {
		t.Fatalf("unconfirmed success outcome = %+v", result.send)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil || state.Round.Number != 0 {
		t.Fatalf("unconfirmed success persisted a round: state=%+v err=%v", state, err)
	}
}

func TestReviewSendRefusesRelaunchedTargetBeforePersistenceOrTransport(t *testing.T) {
	st := openReviewTestStore(t)
	session := createReviewSendSession(t, st, "s1")
	request := reviewSendRequest(&uireview.SendRequest{
		Target: uireview.Target{ID: "s1", Name: "one"}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}}, Round: 1, Count: 1,
	}, session)
	if err := st.SetAgentLaunchedAt("s1", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	writer := &scriptedReviewWriter{alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseSubmitted}}

	effect, err := runReviewWithWriter(request, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendRefused || result.send.Err == nil || !strings.Contains(result.send.Err.Error(), "target changed") {
		t.Fatalf("relaunch outcome = %+v", result.send)
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

func TestReviewSendCommandCapturesSessionIncarnation(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "review-incarnation", t.TempDir(), "")
	session := reviewSessionByName(t, m, "review-incarnation")
	m.review = uireview.New(false)
	m.review.Open(uireview.Target{ID: session.ID, Name: session.Name, Tool: session.Tool, Cwd: session.Cwd}, 0, "/repo")
	m.mode = modeDiff
	m.reviewSendCmd(uireview.SendRequest{
		Target: uireview.Target{ID: session.ID, Name: session.Name}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}}, Round: 1, Count: 1,
	})
	if err := m.services.store.SetAgentLaunchedAt(session.ID, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	m.drainEffects(t)
	state, err := m.services.store.ReviewState(session.ID, "/repo")
	if err != nil || state.Round.Number != 0 {
		t.Fatalf("queued send reached replacement incarnation: state=%+v err=%v", state, err)
	}
	if !strings.Contains(m.errBar.text, "target changed") {
		t.Fatalf("relaunch refusal was not surfaced: %q", m.errBar.text)
	}
}

func TestReviewSendPersistsRoundAndClearsAck(t *testing.T) {
	st := openReviewTestStore(t)
	writer := &scriptedReviewWriter{alive: true, sendResult: tmux.SendResult{Phase: tmux.SendPhaseSubmitted}}
	session := createReviewSendSession(t, st, "s1")
	if err := st.SetAcked("s1", true); err != nil {
		t.Fatal(err)
	}
	req := reviewSendRequest(&uireview.SendRequest{
		Target:   uireview.Target{ID: "s1", Name: "one"},
		RepoRoot: "/repo", Prompt: "the prompt",
		State: uireview.SavedState{
			Round:    uireview.Round{Number: 2},
			Comments: []uireview.Comment{{ID: "c1", File: "a.go", Line: 1, Text: "new", Round: 2}},
		},
		CommentIDs: []string{"c1"}, Round: 2, Count: 1,
	}, session)
	effect, err := runReviewWithWriter(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendConfirmed || result.send.Err != nil {
		t.Fatalf("send outcome = %+v", result.send)
	}
	if len(writer.sendCalls) != 1 || writer.sendCalls[0] != "the prompt" {
		t.Fatalf("delivered prompts = %v", writer.sendCalls)
	}
	state, err := st.ReviewState("s1", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 1 || state.Comments[0].Text != "new" {
		t.Fatalf("persisted state = %+v", state)
	}
	rows, err := st.ListSessions(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "s1" && row.Acked {
			t.Fatal("send did not clear the alert ack")
		}
	}
}

func TestReviewSavePersistsCapturedStatePreservingHandled(t *testing.T) {
	st := openReviewTestStore(t)
	req := reviewEffectRequest{
		op: reviewOpSave, targetID: "s1", repoRoot: "/repo",
		state: uireview.SavedState{
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
	effect, err := runReviewWithWriter(req, st, &scriptedReviewWriter{})
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.save.Err != nil {
		t.Fatal(result.save.Err)
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

func TestReviewHandleReportsFoundFlag(t *testing.T) {
	st := openReviewTestStore(t)
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
		effect, err := runReviewWithWriter(reviewEffectRequest{
			op: reviewOpHandle, targetID: "s1", repoRoot: "/repo",
			commentID: tc.commentID, handled: true,
		}, st, &scriptedReviewWriter{})
		if err != nil {
			t.Fatal(err)
		}
		result := effect.(reviewEffectResult)
		if result.handle.Err != nil || result.handle.Found != tc.found {
			t.Fatalf("handle %s = %+v", tc.commentID, result.handle)
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

func TestReviewStatusReportsOnlyRoundComments(t *testing.T) {
	st := openReviewTestStore(t)
	if err := st.MergeReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{
			{ID: "sent", File: "a.go", Line: 1, Text: "t", Round: 2, Resolved: true},
			{ID: "draft", File: "a.go", Line: 2, Text: "d", Round: 0},
		},
		Round: store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	effect, err := runReviewWithWriter(reviewEffectRequest{
		op: reviewOpStatus, targetID: "s1", repoRoot: "/repo", generation: 3,
	}, st, &scriptedReviewWriter{})
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.status.Err != nil || result.status.Generation != 3 {
		t.Fatalf("status = %+v", result.status)
	}
	if len(result.status.Handled) != 1 || !result.status.Handled["sent"] {
		t.Fatalf("handled = %+v", result.status.Handled)
	}
}

func TestReviewNormalizePersistsCurrentShape(t *testing.T) {
	st := openReviewTestStore(t)
	if err := st.SetReviewState("s1", "/repo", store.ReviewState{
		Comments: []store.ReviewComment{{File: "a.go", Line: 1, Text: "t", Round: 2}},
		Round:    store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	req := reviewEffectRequest{op: reviewOpNormalize, targetID: "s1", repoRoot: "/repo"}
	for i := 0; i < 2; i++ {
		if _, err := runReviewWithWriter(req, st, &scriptedReviewWriter{}); err != nil {
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
	if _, err := runReviewWithWriter(req, st, &scriptedReviewWriter{}); err != nil {
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

func TestReviewSaveDefersToEffectLane(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "savelane", gitRepoWithTwoChangedFiles(t))
	sess := reviewSessionByName(t, m, "savelane")
	path := m.currentFileDiff().File.Path
	mark := m.reviewedMarkKey(path)

	cmd := m.toggleReviewed()
	state, err := m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := state.Reviewed[mark]; set {
		t.Fatal("save wrote the store on the update path")
	}
	m.applyCmd(t, cmd)
	state, err = m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := state.Reviewed[mark]; !set {
		t.Fatal("accepted save did not persist through the effect lane")
	}
}

func TestReviewSaveFailureSurfacesAfterReviewChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{"closed", func(m *Model) { m.review.Close(); m.mode = modeList }},
		{"retargeted", func(m *Model) { _, _ = m.review.SelectRepo("/other") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			m.review = uireview.New(false)
			m.review.Open(uireview.Target{ID: "s1", Cwd: "/repo"}, 0, "/repo")
			m.mode = modeDiff
			tc.mutate(m)
			modeBefore := m.mode

			cmd := m.handleReviewSave(uireview.SaveResult{TargetID: "s1", RepoRoot: "/repo", Err: errors.New("disk full")})
			if cmd != nil {
				t.Fatal("save failure started navigation")
			}
			if m.mode != modeBefore || !strings.Contains(m.errBar.text, "saving review state: disk full") {
				t.Fatalf("accepted save failure hidden or navigated: mode=%v err=%q", m.mode, m.errBar.text)
			}
		})
	}
}

func TestReviewStatusSeesPriorSave(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "statuslane", gitRepoWithTwoChangedFiles(t))
	sess := reviewSessionByName(t, m, "statuslane")
	repo := m.review.Snapshot().RepoSelected

	if err := m.services.store.MergeReviewState(sess.ID, repo, store.ReviewState{
		Comments: []store.ReviewComment{{ID: "sent1", File: "a.go", Line: 1, Text: "t", Round: 2, Resolved: true}},
		Round:    store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	m.toggleReviewed() // enqueues a save behind which the status rides
	m.reviewStatusCmd(uireview.StatusRequest{TargetID: sess.ID, RepoRoot: repo, Generation: m.review.Snapshot().Generation})
	m.drainEffects(t)

	state, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Reviewed) == 0 {
		t.Fatal("save did not persist")
	}
	if m.errBar.text != "" {
		t.Fatalf("status reconciliation failed: %q", m.errBar.text)
	}
}

func TestReviewSendDrainsOnQuit(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "sendquit", gitRepoWithTwoChangedFiles(t))
	sess := reviewSessionByName(t, m, "sendquit")

	m.openAnnotate()
	typeReviewAnnotation(m, "ship it")
	m.saveAnnotation()
	m.drainEffects(t)

	m.sendAnnotations()
	if m.effects.active != nil || len(m.effects.pending) != 1 {
		t.Fatalf("send not queued: active=%v pending=%d", m.effects.active, len(m.effects.pending))
	}
	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded an accepted review send")
	}
	m.drainEffects(t)
	if len(m.effects.pending) != 0 {
		t.Fatalf("quit left review work in the queue: %d pending", len(m.effects.pending))
	}
	state, err := m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number == 0 {
		t.Fatal("accepted send did not drain to a persisted round on quit")
	}
	if _, ok := m.nextEffectCmd()().(tea.QuitMsg); !ok {
		t.Fatal("drained queue did not quit")
	}
}

func TestReviewLoadNormalizesThroughLane(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := resolveSymlinksOrSelf(gitRepoWithTwoChangedFiles(t))
	createSession(t, m, "norm", repo, "")
	sess := reviewSessionByName(t, m, "norm")
	if err := m.services.store.SetReviewState(sess.ID, repo, store.ReviewState{
		Comments: []store.ReviewComment{
			{File: "a.go", Line: 2, Text: "first", Round: 3},
			{File: "b.go", Line: 4, Text: "second", Round: 3},
		},
		Round: store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	m.selectSessionRow(t, "norm")
	m.drainCmds(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, err = %q", m.errBar.text)
	}

	state, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 2 || state.Comments[0].ID == "" || state.Comments[0].Point != 1 || state.Comments[1].Point != 2 {
		t.Fatalf("stored state not normalized through the lane: %+v", state.Comments)
	}
	if len(m.review.SavedState().Comments) != 2 || m.review.SavedState().Comments[0].ID == "" {
		t.Fatalf("in-memory state not normalized: %+v", m.review.SavedState())
	}

	m.drainCmds(t, m.cycleDiffScope())
	m.drainEffects(t)
	if m.effects.active != nil || len(m.effects.pending) != 0 {
		t.Fatalf("reload left review work in the lane: active=%v pending=%d", m.effects.active, len(m.effects.pending))
	}
	state, err = m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 2 || state.Comments[0].ID == "" {
		t.Fatalf("reload changed the normalized state: %+v", state.Comments)
	}
}

func TestReviewSendOnceRefusesDuplicateSubmit(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "sendonce", gitRepoWithTwoChangedFiles(t))

	m.openAnnotate()
	typeReviewAnnotation(m, "once")
	m.saveAnnotation()
	m.drainEffects(t)

	_, cmd := m.sendAnnotations()
	if m.effects.active != nil || len(m.effects.pending) != 1 {
		t.Fatalf("send did not reach the effect lane: active=%v pending=%d", m.effects.active, len(m.effects.pending))
	}
	second := m.review.BeginSend()
	if second.Error == "" || second.Requests.Send != nil {
		t.Fatalf("duplicate send accepted: %+v", second)
	}
	if len(m.effects.pending) != 1 {
		t.Fatalf("duplicate submit enqueued a second job: %d pending", len(m.effects.pending))
	}
	m.applyCmd(t, cmd)
}

func TestReviewNormalizeDispatchesAsNormalizeOp(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := resolveSymlinksOrSelf(gitRepoWithTwoChangedFiles(t))
	createSession(t, m, "normop", repo, "")
	sess := reviewSessionByName(t, m, "normop")
	if err := m.services.store.SetReviewState(sess.ID, repo, store.ReviewState{
		Comments: []store.ReviewComment{{File: "a.go", Line: 2, Text: "first", Round: 3}},
		Round:    store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	m.selectSessionRow(t, "normop")
	open := m.openDiff()
	_, ready := m.handleReviewPreferences(open().(reviewPreferencesMsg))
	batch, ok := ready().(tea.BatchMsg)
	if !ok {
		t.Fatalf("openDiff command = %T, want batch", open)
	}
	var load reviewLoadMsg
	for _, child := range batch {
		if child == nil {
			continue
		}
		if msg, ok := child().(reviewLoadMsg); ok {
			load = msg
		}
	}
	if load.normalize == nil {
		t.Fatal("load did not capture a normalization to persist")
	}
	if load.normalize.op != reviewOpNormalize {
		t.Fatalf("normalize request dispatched as op %d, want %d", load.normalize.op, reviewOpNormalize)
	}

	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, load)
	m.drainEffects(t)
	if !strings.Contains(m.errBar.text, "persisting review normalization") {
		t.Fatalf("failed normalize surfaced as: %q", m.errBar.text)
	}
	if strings.Contains(m.errBar.text, "saving review round") {
		t.Fatalf("normalize failure invoked send/save reconciliation: %q", m.errBar.text)
	}
}

func TestStaleNormalizeDoesNotOverwriteNewerSave(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := resolveSymlinksOrSelf(gitRepoWithTwoChangedFiles(t))
	createSession(t, m, "stalenorm", repo, "")
	sess := reviewSessionByName(t, m, "stalenorm")

	oldShape := store.ReviewState{
		Comments: []store.ReviewComment{{File: "a.go", Line: 1, Text: "old", Round: 3}},
		Round:    store.ReviewRound{Number: 3},
	}
	newer := store.ReviewState{
		Comments: []store.ReviewComment{{ID: "n1", File: "b.go", Line: 9, Text: "new", Round: 4, Point: 1}},
		Round:    store.ReviewRound{Number: 4},
	}
	if err := m.services.store.SetReviewState(sess.ID, repo, oldShape); err != nil {
		t.Fatal(err)
	}
	m.enqueueEffect(reviewEffectRequest{op: reviewOpSave, targetID: sess.ID, repoRoot: repo, state: reviewStateFromStore(newer)}, 0, false)
	stale, _ := uireview.NormalizeSavedState(reviewStateFromStore(oldShape))
	m.enqueueEffect(reviewEffectRequest{op: reviewOpNormalize, targetID: sess.ID, repoRoot: repo, state: stale}, 0, false)
	m.drainEffects(t)

	got, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Round.Number != 4 || len(got.Comments) != 1 || got.Comments[0].ID != "n1" || got.Comments[0].Text != "new" {
		t.Fatalf("stale normalize overwrote the newer state: %+v", got)
	}
}

func TestReviewSendProbeErrorPersistsNothing(t *testing.T) {
	st := openReviewTestStore(t)
	session := createReviewSendSession(t, st, "s1")
	writer := &probeFailingWriter{probeErr: errors.New("tmux probe failed")}
	req := reviewSendRequest(&uireview.SendRequest{
		Target:   uireview.Target{ID: "s1", Name: "one"},
		RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}},
		Round: 1, Count: 1,
	}, session)
	effect, err := runReviewWithWriter(req, st, writer)
	if err != nil {
		t.Fatal(err)
	}
	result := effect.(reviewEffectResult)
	if result.send.Outcome != uireview.SendRefused || result.send.Err == nil || result.send.Err.Error() != "tmux probe failed" {
		t.Fatalf("probe failure outcome = %+v", result.send)
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

type probeFailingWriter struct {
	probeErr error
	sent     []string
}

func (w *probeFailingWriter) exists(id string) (bool, error) { return false, w.probeErr }
func (w *probeFailingWriter) sendText(id, text string) (tmux.SendResult, error) {
	w.sent = append(w.sent, text)
	return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
}

func reviewSessionByName(t *testing.T, m *Model, name string) store.Session {
	t.Helper()
	for _, sess := range m.sessionRows() {
		if sess.Name == name {
			return sess
		}
	}
	t.Fatalf("session %q not found", name)
	return store.Session{}
}
