package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	tea "github.com/charmbracelet/bubbletea"
)

func TestReviewSendCommandCapturesSessionIncarnation(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "review-incarnation", t.TempDir(), "")
	session := reviewSessionByName(t, m, "review-incarnation")
	m.review = uireview.New(false)
	m.review.Open(uireview.Target{ID: session.ID, Name: session.Name, Tool: session.Tool, Cwd: session.Cwd}, 0, "/repo")
	m.base = baseReview
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

func TestReviewSaveDefersToEffectLane(t *testing.T) {
	t.Parallel()
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
		{"closed", func(m *Model) { m.review.Close(); m.base = baseList }},
		{"retargeted", func(m *Model) { _, _ = m.review.SelectRepo("/other") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			m.review = uireview.New(false)
			m.review.Open(uireview.Target{ID: "s1", Cwd: "/repo"}, 0, "/repo")
			m.base = baseReview
			tc.mutate(m)
			before := m.screenID()

			cmd := m.handleReviewSave(uireview.SaveResult{TargetID: "s1", RepoRoot: "/repo", Err: errors.New("disk full")})
			if cmd != nil {
				t.Fatal("save failure started navigation")
			}
			if m.screenID() != before || !strings.Contains(m.errBar.text, "saving review state: disk full") {
				t.Fatalf("accepted save failure hidden or navigated: screen = %v err=%q", m.screenID(), m.errBar.text)
			}
		})
	}
}

func TestReviewStatusSeesPriorSave(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 1 {
		t.Fatalf("send not queued: active=%v pending=%d", m.effects.Main.Active, len(m.effects.Main.Pending))
	}
	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded an accepted review send")
	}
	m.drainEffects(t)
	if len(m.effects.Main.Pending) != 0 {
		t.Fatalf("quit left review work in the queue: %d pending", len(m.effects.Main.Pending))
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
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := uireviewsource.Canonical(gitRepoWithTwoChangedFiles(t))
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
	if m.base != baseReview {
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
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 0 {
		t.Fatalf("reload left review work in the lane: active=%v pending=%d", m.effects.Main.Active, len(m.effects.Main.Pending))
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
	t.Parallel()
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
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 1 {
		t.Fatalf("send did not reach the effect lane: active=%v pending=%d", m.effects.Main.Active, len(m.effects.Main.Pending))
	}
	second := m.review.BeginSend()
	if second.Error == "" || second.Requests.Send != nil {
		t.Fatalf("duplicate send accepted: %+v", second)
	}
	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("duplicate submit enqueued a second job: %d pending", len(m.effects.Main.Pending))
	}
	m.applyCmd(t, cmd)
}

func TestReviewNormalizeDispatchesAsNormalizeOp(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := uireviewsource.Canonical(gitRepoWithTwoChangedFiles(t))
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
	_, ready := m.handleReviewPreferences(open().(uireviewsource.PreferencesMsg))
	batch, ok := ready().(tea.BatchMsg)
	if !ok {
		t.Fatalf("openDiff command = %T, want batch", open)
	}
	var load uireviewsource.LoadMsg
	for _, child := range batch {
		if child == nil {
			continue
		}
		if msg, ok := child().(uireviewsource.LoadMsg); ok {
			load = msg
		}
	}
	if load.Normalize == nil {
		t.Fatal("load did not capture a normalization to persist")
	}
	if load.Normalize.TargetID != sess.ID || load.Normalize.RepoRoot != repo {
		t.Fatalf("normalize captured %s in %s, want %s in %s", load.Normalize.TargetID, load.Normalize.RepoRoot, sess.ID, repo)
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
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := uireviewsource.Canonical(gitRepoWithTwoChangedFiles(t))
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
	m.enqueueEffect(reviewEffectRequest{op: uireviewsource.Save, targetID: sess.ID, repoRoot: repo, state: uireviewsource.StateFromStore(newer)}, 0, false)
	stale, _ := uireview.NormalizeSavedState(uireviewsource.StateFromStore(oldShape))
	m.enqueueEffect(reviewEffectRequest{op: uireviewsource.Normalize, targetID: sess.ID, repoRoot: repo, state: stale}, 0, false)
	m.drainEffects(t)

	got, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Round.Number != 4 || len(got.Comments) != 1 || got.Comments[0].ID != "n1" || got.Comments[0].Text != "new" {
		t.Fatalf("stale normalize overwrote the newer state: %+v", got)
	}
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
