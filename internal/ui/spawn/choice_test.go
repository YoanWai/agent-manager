package spawn

import (
	"slices"
	"testing"

	catalog "github.com/YoanWai/agent-manager/internal/catalog/value"
)

var fiveLevels = []string{"low", "medium", "high", "xhigh", "max"}

var claudeCaps = Caps{Catalog: true, Models: true, Efforts: true}

var claudeAnswer = catalog.Catalog{Models: []catalog.Model{
	{ID: "default", Label: "Default (recommended)", Efforts: fiveLevels, Default: true},
	{ID: "opus", Label: "Opus 5.5", Efforts: fiveLevels},
	{ID: "haiku", Label: "Haiku 4.5"},
}}

// fakeChoiceHost answers a choice, and the features built on one, without
// a root model: the tool flags, the catalog answer and the settings cache
// are plain maps, and the status bar is a list.
type fakeChoiceHost struct {
	caps     map[string]Caps
	answers  map[string]*Answer
	settings map[string]string
	errs     []string
	cleared  int
}

func newFakeChoiceHost() *fakeChoiceHost {
	return &fakeChoiceHost{
		caps:     map[string]Caps{"claude": claudeCaps},
		answers:  map[string]*Answer{"claude": {Cat: claudeAnswer, Loaded: true}},
		settings: map[string]string{},
	}
}

func (h *fakeChoiceHost) ChoiceCaps(toolName string) Caps { return h.caps[toolName] }

func (h *fakeChoiceHost) ChoiceAnswer(toolName string) *Answer { return h.answers[toolName] }

func (h *fakeChoiceHost) ChoiceSetting(key string) string { return h.settings[key] }

func (h *fakeChoiceHost) SaveChoiceSetting(key, value string) { h.settings[key] = value }

func (h *fakeChoiceHost) ReportErr(text string) { h.errs = append(h.errs, text) }

func (h *fakeChoiceHost) ClearErr() { h.cleared++ }

// A choice restores, keeps and remembers through its host alone.
func TestChoiceThroughAFakeHost(t *testing.T) {
	h := newFakeChoiceHost()
	h.settings[savedChoiceKey("claude")] = `{"Model":"opus","Effort":"high"}`
	ch := NewChoice(h, "claude")
	if got := ch.current(h, "claude"); got != (Pick{Model: "opus", Effort: "high"}) {
		t.Fatalf("restored choice = %+v", got)
	}
	ch.pickModel(h, "claude", "default")
	if got := ch.EffortLevel(h, "claude"); got != "high" {
		t.Fatalf("a model offering the same level kept %q", got)
	}
	if saved := savedChoice(h, "claude"); saved == nil || saved.Model != "default" {
		t.Fatalf("the pick was not kept: %+v", saved)
	}
	ch.pickModel(h, "claude", "haiku")
	if shown, _ := ch.EffortShown(h, "claude"); shown || ch.EffortLevel(h, "claude") != "" {
		t.Fatal("haiku lists no levels, so no effort should show")
	}
	RememberModel(h, "claude", Pick{Model: "opus"})
	if recent := recentModels(h, "claude"); !slices.Equal(recent, []string{"opus"}) {
		t.Fatalf("recent = %v", recent)
	}
	if _, err := ch.Launch(h, "claude", "nope"); err == nil {
		t.Fatal("a model the CLI does not list launched")
	}
	h.settings[recentModelsKey("claude")] = "{"
	if recentModels(h, "claude") != nil || len(h.errs) != 1 {
		t.Fatalf("an unreadable setting should report once: %v", h.errs)
	}
}

// A recent pick kept under a bare id still lists its model once, though
// the CLI now routes it through a provider.
func TestModelListListsARecentPickOnce(t *testing.T) {
	h := newFakeChoiceHost()
	h.caps["hermes"] = Caps{Catalog: true, Models: true}
	h.answers["hermes"] = &Answer{Loaded: true, Cat: catalog.Catalog{Models: []catalog.Model{{ID: "grok-4.6", Provider: "xai-oauth"}, {ID: "claude-opus-5", Provider: "anthropic"}}}}
	h.settings[recentModelsKey("hermes")] = `["grok-4.6"]`
	ch := NewChoice(h, "hermes")
	if keys := ch.Suggestions(h, "hermes"); !slices.Equal(keys, []string{"xai-oauth:grok-4.6", "anthropic:claude-opus-5"}) {
		t.Fatalf("list = %v", keys)
	}
}

// The saved choice keeps config.Choice's stored shape, field for field.
func TestPickKeepsTheStoredChoiceShape(t *testing.T) {
	h := newFakeChoiceHost()
	ch := NewChoice(h, "claude")
	ch.pickModel(h, "claude", "opus")
	if got, want := h.settings[savedChoiceKey("claude")], `{"Provider":"","Model":"opus","Effort":"","Profile":""}`; got != want {
		t.Fatalf("kept %s, want %s", got, want)
	}
}
