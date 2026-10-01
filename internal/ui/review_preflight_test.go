package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
)

type reviewReadProbe struct {
	calls int
	err   error
}

func (r *reviewReadProbe) ReviewScope(string) (string, error) { r.calls++; return "branch", r.err }
func (r *reviewReadProbe) ReviewRepo(string) (string, error)  { r.calls++; return "", r.err }

func TestReviewOpeningDefersPreferencesAndKeepsReadErrors(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-read", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	reader := &reviewReadProbe{err: errors.New("read denied")}
	cmd := m.openDiffWithReader(reader)
	if reader.calls != 0 {
		t.Fatal("opening review read preferences on Update")
	}
	msg := cmd().(reviewPreferencesMsg)
	_, _ = m.handleReviewPreferences(msg)
	if m.review.Scope() != git.ScopeUncommitted {
		t.Fatal("failed preference read fabricated a scope")
	}
	if !strings.Contains(m.errBar.text, "read denied") {
		t.Fatalf("read error lost: %q", m.errBar.text)
	}
}

func TestReviewPreferencesCannotRetargetReopenedReview(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-stale", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	cmd := m.openDiffWithReader(&reviewReadProbe{})
	m.review.Close()
	m.review.Open(reviewTarget(s), git.ScopeStaged, "")
	gen := m.review.Generation()
	_, next := m.handleReviewPreferences(cmd().(reviewPreferencesMsg))
	if next != nil || m.review.Generation() != gen || m.review.Scope() != git.ScopeStaged {
		t.Fatal("stale preferences replaced reopened review")
	}
}

func TestReviewPreferencesCannotOpenDuringQuitOrAfterRelaunch(t *testing.T) {
	for _, quitting := range []bool{false, true} {
		t.Run(map[bool]string{false: "relaunch", true: "quit"}[quitting], func(t *testing.T) {
			m := buildModel(t)
			if m.services.gitDrv == nil {
				t.Skip("git unavailable")
			}
			s := store.Session{ID: "review-changed", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
			m.workspace.sessions = []store.Session{s}
			m.rebuildRows()
			m.selectSessionRow(t, s.Name)
			cmd := m.openDiffWithReader(&reviewReadProbe{})
			gen := m.review.Generation()
			if quitting {
				m.effects.quitting = true
			} else {
				m.workspace.sessions[0].AgentLaunchedAt = time.Now()
			}
			_, next := m.handleReviewPreferences(cmd().(reviewPreferencesMsg))
			if next != nil || m.review.Generation() != gen {
				t.Fatal("late preferences initiated a load")
			}
		})
	}
}

func TestReviewPreferencesFinishBehindHelpWithoutStealingIt(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-help", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	cmd := m.openDiffWithReader(&reviewReadProbe{})
	m.openHelp()
	_, load := m.handleReviewPreferences(cmd().(reviewPreferencesMsg))
	if m.mode != modeHelp || load == nil || m.review.Scope() != git.ScopeBranch {
		t.Fatal("help interrupted review preparation or was replaced")
	}
}

func TestReviewPreferencesFinishBehindPickerWithoutLeavingLoadingStuck(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-picker-prefs", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	m.ledger.pickedRepos = map[string]string{s.ID: s.Cwd}
	cmd := m.openDiffWithReader(&reviewReadProbe{})
	state := m.review.Snapshot()
	m.openPick([]pickRow{{label: "branch", root: s.Cwd}}, "branches", pickRepo, s.Cwd, reviewPickerSource{generation: state.Generation, targetID: s.ID, repoRoot: s.Cwd}, "")
	_, load := m.handleReviewPreferences(cmd().(reviewPreferencesMsg))
	if m.mode != modeRepoPick || load == nil {
		t.Fatal("picker stranded the sole review load")
	}
	if m.reviewPickerSourceCurrent(m.repoPick.source) {
		t.Fatal("old picker survived review preparation")
	}
}

func TestReviewFocusReturnKeepsItsCapturedSession(t *testing.T) {
	m := buildModel(t)
	a := store.Session{ID: "return-a", Name: "A", Tool: "claude"}
	b := store.Session{ID: "return-b", Name: "B", Tool: "claude"}
	m.workspace.sessions = []store.Session{a, b}
	m.rebuildRows()
	m.selectSessionRow(t, b.Name)
	m.review.Open(reviewTarget(a), git.ScopeUncommitted, "")
	m.mode = modeDiff
	m.reviewReturn = reviewReturn{kind: reviewReturnFocus, sessionID: a.ID}
	m.closeDiff()
	if m.effects.active == nil || m.effects.active.request.(focusRequest).sessionID != a.ID {
		t.Fatal("review returned into a different session")
	}
}
