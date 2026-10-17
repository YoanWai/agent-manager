package review_test

import (
	"strings"
	"testing"

	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPickerKeysReturnTypedOutcomes(t *testing.T) {
	var p review.Picker
	rows := []review.PickerRow{{Label: "alpha", Root: "/w/alpha"}, {Label: "beta", Root: "/w/beta"}, {Label: "gamma", Root: "/w/gamma"}}
	p.Open(rows, "⌥ Review repo", review.PickRepo, "/w/beta", review.PickerSource{TargetID: "s1"}, "")
	if p.Cursor() != 1 {
		t.Fatalf("open: cursor=%d", p.Cursor())
	}
	if body := p.Body(review.RenderContext{}, 54, 30); p.Title() != "⌥ Review repo" || !strings.Contains(body, "gamma") {
		t.Fatalf("view: title=%q\n%s", p.Title(), body)
	}

	p.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mm")}, true)
	if got := p.FilteredRows(); len(got) != 1 || got[0].Label != "gamma" {
		t.Fatalf("filter rows = %v", got)
	}
	result := p.Key(tea.KeyMsg{Type: tea.KeyEnter}, true)
	if !result.Close || result.Choice == nil || *result.Choice != (review.PickerChoice{Kind: review.PickRepo, Value: "/w/gamma"}) {
		t.Fatalf("enter: %+v", result)
	}

	p.Open([]review.PickerRow{{Label: "auto"}, {Label: "main", Root: "main"}}, "⌥ Diff base", review.PickBase, "", review.PickerSource{}, "/w/alpha")
	p.Key(tea.KeyMsg{Type: tea.KeyDown}, true)
	if result := p.Key(tea.KeyMsg{Type: tea.KeyEnter}, false); !result.Close || result.Choice != nil || result.Error == "" {
		t.Fatalf("stale enter: %+v", result)
	}
	result = p.Key(tea.KeyMsg{Type: tea.KeyEnter}, true)
	if result.Choice == nil || *result.Choice != (review.PickerChoice{Kind: review.PickBase, Value: "main"}) {
		t.Fatalf("base enter chose %+v", result.Choice)
	}
	if result := p.Key(tea.KeyMsg{Type: tea.KeyEsc}, true); !result.Close || result.Choice != nil {
		t.Fatalf("esc: %+v", result)
	}
	if result := p.Key(tea.KeyMsg{Type: tea.KeyCtrlC}, true); !result.Quit {
		t.Fatal("ctrl+c did not ask root to quit")
	}
}

func TestPickerSourceNamesTheOpenReview(t *testing.T) {
	model := review.New(false)
	request := model.Open(review.Target{ID: "s1"}, git.ScopeUncommitted, "/repo")
	source := review.PickerSource{Generation: request.Generation, TargetID: "s1", RepoRoot: "/repo"}
	if !model.PickerSourceCurrent(source) {
		t.Fatal("the open review's own source was refused")
	}
	for name, stale := range map[string]review.PickerSource{
		"generation": {Generation: request.Generation + 1, TargetID: "s1", RepoRoot: "/repo"},
		"target":     {Generation: request.Generation, TargetID: "other", RepoRoot: "/repo"},
		"repo":       {Generation: request.Generation, TargetID: "s1", RepoRoot: "/other"},
	} {
		if model.PickerSourceCurrent(stale) {
			t.Errorf("%s: a stale source was accepted", name)
		}
	}
	model.Close()
	if model.PickerSourceCurrent(source) {
		t.Fatal("a closed review accepted its old source")
	}
}
