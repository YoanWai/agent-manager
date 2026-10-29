package review_test

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfiguredHelpKeepsDraftAndConfirmationPriority(t *testing.T) {
	model, _ := loadedReview(t)
	parsedKey, err := keybind.Parse("H")
	binding := keybind.Keys(parsedKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx := review.KeyContext{CodeHeight: 10, HelpKey: binding}
	if !model.OpenAnnotation() {
		t.Fatal("annotation did not open")
	}
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}
	if result := model.Key(key, ctx); result.Navigation != review.NavigationNone || model.AnnotationValue() != "H" {
		t.Fatalf("draft result=%+v text=%q", result, model.AnnotationValue())
	}
	model.Key(tea.KeyMsg{Type: tea.KeyEsc}, ctx)
	model.SetSendConfirm(true)
	if result := model.Key(key, ctx); result.Navigation != review.NavigationNone {
		t.Fatalf("confirm opened Help: %+v", result)
	}
	model.SetSendConfirm(false)
	if result := model.Key(key, ctx); result.Navigation != review.NavigationHelp {
		t.Fatalf("Help=%+v", result)
	}
}

func TestHelpBindingCannotStealLocalReviewActions(t *testing.T) {
	for _, spec := range []string{"b", "c"} {
		t.Run(spec, func(t *testing.T) {
			model, _ := loadedReview(t)
			parsed, err := keybind.Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			binding := keybind.Keys(parsed)
			result := model.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(spec)}, review.KeyContext{HelpKey: binding, CodeHeight: 10})
			if spec == "b" && result.Navigation != review.NavigationBranchPicker {
				t.Fatalf("branch action stolen: %+v", result)
			}
			if spec == "c" && !model.Snapshot().Annotating {
				t.Fatalf("comment action stolen: %+v", result)
			}
			for _, tier := range model.Legend(binding) {
				for _, pair := range tier.Pairs {
					if pair[1] == "keys" {
						t.Fatalf("local key advertised as Help: %v", pair)
					}
				}
			}
		})
	}
}

func TestNonconflictingHelpAliasesAndIdleEnter(t *testing.T) {
	for _, test := range []struct {
		spec string
		msg  tea.KeyMsg
	}{
		{"H", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}}},
		{"f2", tea.KeyMsg{Type: tea.KeyF2}},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}},
	} {
		t.Run(test.spec, func(t *testing.T) {
			model, _ := loadedReview(t)
			parsed, err := keybind.Parse(test.spec)
			if err != nil {
				t.Fatal(err)
			}
			binding := keybind.Keys(parsed)
			if result := model.Key(test.msg, review.KeyContext{HelpKey: binding}); result.Navigation != review.NavigationHelp {
				t.Fatalf("Help: %+v", result)
			}
			model.SetSendConfirm(true)
			if result := model.Key(test.msg, review.KeyContext{HelpKey: binding}); result.Navigation == review.NavigationHelp {
				t.Fatal("Help stole send confirmation")
			}
		})
	}
}

func TestHelpAliasesCannotOverrideLocalReviewKeys(t *testing.T) {
	for _, test := range []struct {
		spec string
		msg  tea.KeyMsg
	}{
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}},
		{"pgdown", tea.KeyMsg{Type: tea.KeyPgDown}},
		{"shift+n", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}}},
		{"B", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}}},
		{"up", tea.KeyMsg{Type: tea.KeyUp}},
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}},
		{"J", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}}},
		{"f3", tea.KeyMsg{Type: tea.KeyF3}},
		{"ctrl+d", tea.KeyMsg{Type: tea.KeyCtrlD}},
	} {
		t.Run(test.spec, func(t *testing.T) {
			model, _ := loadedReview(t)
			key, err := keybind.Parse(test.spec)
			if err != nil {
				t.Fatal(err)
			}
			binding := keybind.Keys(key)
			if got := review.EffectiveHelpBinding(binding); got.Label() != "" {
				t.Fatalf("local key advertised for Help: %s", got.Label())
			}
			result := model.Key(test.msg, review.KeyContext{HelpKey: binding, CodeHeight: 10, PersistenceAvailable: true})
			if !result.Consumed || result.Navigation == review.NavigationHelp {
				t.Fatalf("local action lost its key: %+v", result)
			}
		})
	}
}
