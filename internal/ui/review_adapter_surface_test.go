package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A review with two repos under one session, the repo picker's real trigger.
func pickerReview(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.base = baseReview
	target := uireview.Target{ID: "picker"}
	request := seedReviewForTest(m, target, git.ScopeUncommitted, "/tmp/one", diff.Set{}, false)
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/tmp/one", RepoRoots: []string{"/tmp/one", "/tmp/two"}, Set: diff.Set{},
	})
	return m
}

// r opens the picker over Review, which paints and names its keys, and the
// mode under it stays Review.
func TestReviewPickerOpensOnItsKey(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	*m = *updated.(*Model)
	if !m.pickerOpen() || m.base != baseReview {
		t.Fatalf("r should open the picker over review: open=%t screen = %v", m.pickerOpen(), m.screenID())
	}
	m.prepareFrame()
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "Review repo") || !strings.Contains(frame, "two") {
		t.Fatalf("the frame should show the picker and its rows:\n%s", frame)
	}
	if top, _ := m.top(); top.Owner() != ownerManager || len(top.Footer()) == 0 {
		t.Fatal("the picker should keep the keyboard on the manager and name its keys")
	}
}

// esc and a click off the card each close the picker and leave Review as
// it was: the filter is a search, not a draft.
func TestReviewPickerDismisses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		close func(m *Model)
	}{
		{"esc", func(m *Model) { m.Update(tea.KeyMsg{Type: tea.KeyEsc}) }},
		{"backdrop", func(m *Model) {
			m.prepareFrame()
			x, y := m.layout.card.left-1, m.layout.card.top-1
			m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pickerReview(t)
			m.openRepoPick()
			if top, _ := m.top(); !top.Dismissible() {
				t.Fatal("the picker should be dismissible")
			}
			tc.close(m)
			if m.pickerOpen() || m.effects.Quitting || m.base != baseReview {
				t.Fatalf("%s: open=%t quitting=%t screen = %v", tc.name, m.pickerOpen(), m.effects.Quitting, m.screenID())
			}
			if got := m.review.Snapshot().RepoSelected; got != "/tmp/one" {
				t.Fatalf("closing the picker changed the repo to %q", got)
			}
		})
	}
}

// Enter closes the picker and hands the pick to the adapter as a message,
// so the choice is checked against the review showing when it lands.
func TestReviewPickerEnterCarriesThePick(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openRepoPick()
	for _, r := range "two" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.pickerOpen() || cmd == nil {
		t.Fatalf("enter should close the picker and return the pick: open=%t cmd=%v", m.pickerOpen(), cmd != nil)
	}
	pick, ok := cmd().(uireview.PickedMsg)
	if !ok || pick.Choice.Value != "/tmp/two" || pick.Choice.Kind != uireview.PickRepo {
		t.Fatalf("pick = %+v", pick)
	}
}

// A pick for a review that has since moved on is dropped, and the user is
// told to reopen the picker instead of retargeting the newer review.
func TestReviewPickerStalePickIsRefused(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openRepoPick()
	source := m.pickerOf(t).Source()
	m.Pop()
	_, _ = m.review.SelectRepo("/tmp/two")
	generation := m.review.Generation()
	updated, cmd := m.Update(uireview.PickedMsg{Choice: uireview.PickerChoice{Kind: uireview.PickRepo, Value: "/tmp/one"}, Source: source})
	*m = *updated.(*Model)
	if cmd != nil || m.review.Generation() != generation || !strings.Contains(m.errBar.text, "reopen the picker") {
		t.Fatalf("stale pick: cmd=%v generation=%d err=%q", cmd != nil, m.review.Generation(), m.errBar.text)
	}
}

// A click on a row moves the cursor there, a click on the cursor row picks
// it, and the wheel steps the cursor the way the arrows do.
func TestReviewPickerClickAndWheel(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openReviewPicker([]uireview.PickerRow{{Label: "alpha", Root: "/w/alpha"}, {Label: "beta", Root: "/w/beta"}, {Label: "gamma", Root: "/w/gamma"}}, "Review branch", uireview.PickRepo, "/w/alpha", m.reviewPickerSource(), "")
	x, y := paintedCell(t, m, "beta")
	if m = leftClick(m, x, y); m.pickerOf(t).Cursor() != 1 || !m.pickerOpen() {
		t.Fatalf("a click on beta should move the cursor there, cursor %d open %t", m.pickerOf(t).Cursor(), m.pickerOpen())
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.pickerOf(t).Cursor() != 2 {
		t.Fatalf("the wheel should move the cursor on, cursor %d", m.pickerOf(t).Cursor())
	}
	x, y = paintedCell(t, m, "gamma")
	m, cmd := leftClickCmd(m, x, y)
	if m.pickerOpen() || cmd == nil {
		t.Fatal("a click on the cursor row should pick it and close the picker")
	}
	if pick, ok := cmd().(uireview.PickedMsg); !ok || filepath.Base(pick.Choice.Value) != "gamma" {
		t.Fatalf("pick = %+v", cmd())
	}
}

// Leaving Review with the picker up pops it, so a reopened Review starts
// clean and nothing is left on the stack over the list.
func TestReviewPickerLeavesWithReview(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openRepoPick()
	m.closeDiff()
	if m.pickerOpen() || !m.onList() {
		t.Fatalf("after closing review: open=%t screen = %v", m.pickerOpen(), m.screenID())
	}
}
