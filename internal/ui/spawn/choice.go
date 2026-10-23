package spawn

import (
	"encoding/json"
	"fmt"
	"strings"

	catalog "github.com/YoanWai/agent-manager/internal/catalog/value"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Caps are the choice flags a CLI's configuration declares: whether it can
// be asked for a catalog, and which of profile, model and effort it takes.
type Caps struct {
	Catalog  bool
	Profiles bool
	Models   bool
	Efforts  bool
}

// Answer is a copy of the CLI's latest catalog answer.
type Answer struct {
	Cat     catalog.Catalog
	Err     error
	Loaded  bool
	Loading bool
}

// Pick is what a new session launches its CLI on, field for field the
// config.Choice the root launches with; empty keeps the CLI's default.
type Pick struct {
	// Provider routes Model where the CLI picks per provider (hermes).
	Provider string
	Model    string
	Effort   string
	Profile  string
}

// ChoiceHost is what a choice reads from the root: the CLI's flags, its
// latest catalog answer, and the cached choice settings, with the status
// bar for an unreadable one and the effect lane that keeps a new one.
type ChoiceHost interface {
	ChoiceCaps(toolName string) Caps
	ChoiceAnswer(toolName string) *Answer
	ChoiceSetting(key string) string
	SaveChoiceSetting(key, value string)
	ReportErr(text string)
	// ChoiceRemoteHost names the SSH connection the new session goes to,
	// or is empty for this machine.
	ChoiceRemoteHost() string
}

// Choice is what a new session launches its CLI with. Index 0 of profile
// and effort, and an empty model, leave the CLI's own default.
type Choice struct {
	profile int
	model   string
	effort  int
	// filter shows the pick, or typing that narrows the list when filtering.
	filter    textinput.Model
	filtering bool
	// typedEffort holds the level of a CLI that lists none (hermes).
	typedEffort textinput.Model
	sugg        modelSuggest
	recent      []string
	// saved is the CLI's last choice, waiting for its answer to place it.
	saved *Pick
}

func (ch *Choice) query() string {
	if !ch.filtering {
		return ""
	}
	return ch.filter.Value()
}

func (ch *Choice) openModelList(h ChoiceHost, toolName string) {
	ch.filtering = false
	ch.sugg = modelSuggest{open: true}
	if ch.model == "" {
		return
	}
	for i, entry := range ch.suggestions(h, toolName, "") {
		if entry.model.Key() == ch.model {
			ch.sugg.index = i
		}
	}
}

type modelSuggest struct {
	open  bool
	index int
	// chosen is set once the user moves onto the highlight; enter picks it.
	chosen bool
	offset int
}

// NewChoice starts a CLI's choice from its kept one. One for a connection
// starts on the host's defaults instead: the kept choice is this machine's,
// and the host refuses a profile or model its own CLI does not list.
func NewChoice(h ChoiceHost, toolName string) Choice {
	filter := textinput.New()
	filter.CharLimit = 200
	filter.Placeholder = toolName + "'s default"
	typed := textinput.New()
	typed.CharLimit = 40
	typed.Placeholder = "default"
	ch := Choice{filter: filter, typedEffort: typed, recent: recentModels(h, toolName)}
	if h.ChoiceRemoteHost() == "" {
		ch.saved = savedChoice(h, toolName)
	}
	ch.restore(h, toolName)
	return ch
}

func savedChoiceKey(toolName string) string { return "choice." + toolName }

func savedChoice(h ChoiceHost, toolName string) *Pick {
	raw := h.ChoiceSetting(savedChoiceKey(toolName))
	if raw == "" {
		return nil
	}
	var saved Pick
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		h.ReportErr("reading the last choice: " + err.Error())
		return nil
	}
	return &saved
}

// keep saves the choice as the CLI's own, so the next form or quick
// prompt on it starts there. A choice made for a connection is not kept.
func (ch *Choice) keep(h ChoiceHost, toolName string) {
	if h.ChoiceRemoteHost() != "" {
		return
	}
	raw, err := json.Marshal(ch.current(h, toolName))
	if err != nil {
		h.ReportErr("saving the choice: " + err.Error())
		return
	}
	h.SaveChoiceSetting(savedChoiceKey(toolName), string(raw))
}

// restore places the saved choice once the CLI's answer is in, keeping
// only what that answer still offers.
func (ch *Choice) restore(h ChoiceHost, toolName string) {
	state, _ := choiceAnswer(h, toolName)
	if ch.saved == nil || state == nil || !state.Loaded {
		return
	}
	saved := *ch.saved
	ch.saved = nil
	for i, profile := range choiceProfiles(h, toolName) {
		if profile.Name == saved.Profile {
			ch.profile = i + 1
		}
	}
	if saved.Model != "" {
		ch.model = choiceModelKey(saved)
		if _, ok := ch.pickedModel(h, toolName); ok {
			ch.filter.SetValue(ch.model)
			ch.filter.CursorEnd()
		} else {
			ch.model = ""
		}
	}
	if ch.effortTyped(h, toolName) {
		ch.typedEffort.SetValue(saved.Effort)
		return
	}
	for i, level := range ch.efforts(h, toolName) {
		if level == saved.Effort {
			ch.effort = i + 1
		}
	}
}

// Refit places a saved choice the CLI's new answer can now take, and drops
// what the answer no longer offers.
func (ch *Choice) Refit(h ChoiceHost, toolName string) {
	ch.restore(h, toolName)
	ch.fit(h, toolName)
}

// RefreshPrefs takes a newer read of the CLI's kept choice while the open
// choice still waits to place it.
func (ch *Choice) RefreshPrefs(h ChoiceHost, toolName string) {
	ch.recent = recentModels(h, toolName)
	if ch.saved != nil {
		ch.saved = savedChoice(h, toolName)
		ch.restore(h, toolName)
	}
}

// choiceAnswer is the CLI's latest answer, and whether it can be asked.
func choiceAnswer(h ChoiceHost, toolName string) (*Answer, bool) {
	if !h.ChoiceCaps(toolName).Catalog {
		return nil, false
	}
	return h.ChoiceAnswer(toolName), true
}

func choiceProfiles(h ChoiceHost, toolName string) []catalog.Profile {
	if state, _ := choiceAnswer(h, toolName); state != nil && h.ChoiceCaps(toolName).Profiles {
		return state.Cat.Profiles
	}
	return nil
}

// ProfileName is the picked profile, "" for the CLI's default.
func (ch *Choice) ProfileName(h ChoiceHost, toolName string) string {
	profiles := choiceProfiles(h, toolName)
	if ch.profile == 0 || ch.profile > len(profiles) {
		return ""
	}
	return profiles[ch.profile-1].Name
}

func (ch *Choice) models(h ChoiceHost, toolName string) []catalog.Model {
	state, _ := choiceAnswer(h, toolName)
	if state == nil || !h.ChoiceCaps(toolName).Models {
		return nil
	}
	return state.Cat.ModelsFor(ch.ProfileName(h, toolName))
}

func (ch *Choice) pickedModel(h ChoiceHost, toolName string) (catalog.Model, bool) {
	if ch.model == "" {
		return catalog.Model{}, false
	}
	matches := catalog.Match(ch.models(h, toolName), ch.model)
	if len(matches) != 1 {
		return catalog.Model{}, false
	}
	return matches[0], true
}

// effortModel is the model the effort applies to: the pick, else the one
// the CLI starts on.
func (ch *Choice) effortModel(h ChoiceHost, toolName string) (catalog.Model, bool) {
	switch {
	case !h.ChoiceCaps(toolName).Efforts:
		return catalog.Model{}, false
	case ch.model != "":
		return ch.pickedModel(h, toolName)
	}
	return catalog.Default(ch.models(h, toolName))
}

func (ch *Choice) efforts(h ChoiceHost, toolName string) []string {
	model, _ := ch.effortModel(h, toolName)
	return model.Efforts
}

func (ch *Choice) effortTyped(h ChoiceHost, toolName string) bool {
	model, _ := ch.effortModel(h, toolName)
	return model.EffortTyped
}

// EffortTyped says the model takes a typed level, since its CLI lists none.
func (ch *Choice) EffortTyped(h ChoiceHost, toolName string) bool { return ch.effortTyped(h, toolName) }

func (ch *Choice) fit(h ChoiceHost, toolName string) {
	if ch.profile > len(choiceProfiles(h, toolName)) {
		ch.profile = 0
	}
	if ch.model != "" {
		if _, ok := ch.pickedModel(h, toolName); !ok {
			ch.model = ""
		}
	}
	if ch.effort > len(ch.efforts(h, toolName)) {
		ch.effort = 0
	}
	if ch.sugg.index >= len(ch.suggestions(h, toolName, ch.query())) {
		ch.sugg.index, ch.sugg.chosen, ch.sugg.offset = 0, false, 0
	}
}

// pickModel keeps the effort when the new model offers the same level.
func (ch *Choice) pickModel(h ChoiceHost, toolName string, key string) {
	level := ch.EffortLevel(h, toolName)
	ch.model = key
	ch.filter.SetValue(key)
	ch.filter.CursorEnd()
	ch.filtering = false
	ch.effort = 0
	for i, offered := range ch.efforts(h, toolName) {
		if offered == level {
			ch.effort = i + 1
		}
	}
	ch.sugg = modelSuggest{}
	ch.keep(h, toolName)
}

// EffortLevel is the picked level, "" for the model's default.
func (ch *Choice) EffortLevel(h ChoiceHost, toolName string) string {
	if ch.effortTyped(h, toolName) {
		return strings.TrimSpace(ch.typedEffort.Value())
	}
	efforts := ch.efforts(h, toolName)
	if ch.effort == 0 || ch.effort > len(efforts) {
		return ""
	}
	return efforts[ch.effort-1]
}

// CycleEffort steps the level, the model's default included.
func (ch *Choice) CycleEffort(h ChoiceHost, toolName string, delta int) {
	count := len(ch.efforts(h, toolName)) + 1
	ch.effort = (ch.effort + delta + count) % count
	ch.keep(h, toolName)
}

// CycleProfile steps the profile, the CLI's default included.
func (ch *Choice) CycleProfile(h ChoiceHost, toolName string, delta int) {
	count := len(choiceProfiles(h, toolName)) + 1
	ch.profile = (ch.profile + delta + count) % count
	ch.fit(h, toolName)
	ch.keep(h, toolName)
}

// Launch is the choice a spawn launches on; typed is a model typed into
// the filter but not picked, which must name one the CLI lists.
func (ch *Choice) Launch(h ChoiceHost, toolName string, typed string) (Pick, error) {
	typed = strings.TrimSpace(typed)
	if typed != "" && typed != ch.model {
		if matches := catalog.Match(ch.models(h, toolName), typed); len(matches) == 1 {
			ch.pickModel(h, toolName, matches[0].Key())
		} else {
			return Pick{}, fmt.Errorf("model %q is not one %s lists: pick one from the list", typed, toolName)
		}
	}
	return ch.current(h, toolName), nil
}

func choiceModelKey(c Pick) string {
	return catalog.Model{ID: c.Model, Provider: c.Provider}.Key()
}

func (ch *Choice) current(h ChoiceHost, toolName string) Pick {
	picked := Pick{Profile: ch.ProfileName(h, toolName), Effort: ch.EffortLevel(h, toolName)}
	if model, ok := ch.pickedModel(h, toolName); ok {
		picked.Model, picked.Provider = model.ID, model.Provider
	}
	return picked
}

// Model is the picked model's key, "" for the CLI's default.
func (ch *Choice) Model() string { return ch.model }

// Current is the choice as it stands, without a typed model.
func (ch *Choice) Current(h ChoiceHost, toolName string) Pick { return ch.current(h, toolName) }

// FilterValue is what the model field shows or holds typed.
func (ch *Choice) FilterValue() string { return ch.filter.Value() }

// SetFilter puts text in the model field, as a model typed but not picked.
func (ch *Choice) SetFilter(value string) { ch.filter.SetValue(value) }

// ListOpen says the model list is open.
func (ch *Choice) ListOpen() bool { return ch.sugg.open }

type suggestion struct {
	model  catalog.Model
	recent bool
}

// ModelListRows is the most model rows a list shows before it scrolls.
const ModelListRows = 9

// suggestions lists the recent picks first, then the rest.
func (ch *Choice) suggestions(h ChoiceHost, toolName string, query string) []suggestion {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := func(model catalog.Model) bool {
		return query == "" || strings.Contains(strings.ToLower(model.Key()), query) || strings.Contains(strings.ToLower(model.Label), query)
	}
	models := ch.models(h, toolName)
	var list []suggestion
	listed := map[string]bool{}
	add := func(model catalog.Model, recent bool) {
		if matches(model) && !listed[model.Key()] {
			listed[model.Key()] = true
			list = append(list, suggestion{model: model, recent: recent})
		}
	}
	for _, key := range ch.recent {
		for _, model := range catalog.Match(models, key) {
			add(model, true)
		}
	}
	for _, model := range models {
		add(model, false)
	}
	return list
}

// Suggestions are the keys of the models the list shows, recent first.
func (ch *Choice) Suggestions(h ChoiceHost, toolName string) []string {
	var keys []string
	for _, entry := range ch.suggestions(h, toolName, ch.query()) {
		keys = append(keys, entry.model.Key())
	}
	return keys
}

const recentModelLimit = 3

func recentModelsKey(toolName string) string { return "recent_models." + toolName }

func recentModels(h ChoiceHost, toolName string) []string {
	var keys []string
	raw := h.ChoiceSetting(recentModelsKey(toolName))
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		h.ReportErr("reading recent models: " + err.Error())
		return nil
	}
	return keys
}

// RememberModel puts a launched model at the head of the CLI's recent ones.
func RememberModel(h ChoiceHost, toolName string, picked Pick) {
	if picked.Model == "" {
		return
	}
	key := choiceModelKey(picked)
	keys := []string{key}
	for _, earlier := range recentModels(h, toolName) {
		if earlier != key && len(keys) < recentModelLimit {
			keys = append(keys, earlier)
		}
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		h.ReportErr("remembering the model: " + err.Error())
		return
	}
	h.SaveChoiceSetting(recentModelsKey(toolName), string(raw))
}

// SettingKeys are the per-CLI keys the form and quick prompt read from
// the settings cache.
func SettingKeys(tools []string) []string {
	keys := make([]string, 0, 2*len(tools))
	for _, name := range tools {
		keys = append(keys, savedChoiceKey(name), recentModelsKey(name))
	}
	return keys
}

// ModelListed says the model row has a list to pick from.
func ModelListed(h ChoiceHost, toolName string) bool {
	state, supported := choiceAnswer(h, toolName)
	switch {
	case !supported || !h.ChoiceCaps(toolName).Models:
		return false
	case state == nil || (!state.Loaded && state.Loading):
		return false
	case state.Err != nil && len(state.Cat.Models) == 0:
		return false
	case len(state.Cat.Models) == 0 && len(state.Cat.Profiles) == 0:
		return false
	}
	return true
}

// EffortShown says whether the effort row shows, and whether it takes keys.
func (ch *Choice) EffortShown(h ChoiceHost, toolName string) (shown, active bool) {
	state, supported := choiceAnswer(h, toolName)
	if !supported || !h.ChoiceCaps(toolName).Efforts {
		return true, false
	}
	if state == nil || !state.Loaded || len(ch.models(h, toolName)) == 0 {
		return false, false
	}
	model, known := ch.effortModel(h, toolName)
	switch {
	case !known:
		return true, false
	case model.EffortTyped:
		return true, true
	case len(model.Efforts) == 0:
		return false, false
	}
	return true, true
}

// HasProfiles says the CLI answered profiles to pick from.
func HasProfiles(h ChoiceHost, toolName string) bool {
	return len(choiceProfiles(h, toolName)) > 0
}

// OpenFilter empties the filter and opens the model list on the pick.
func (ch *Choice) OpenFilter(h ChoiceHost, toolName string) {
	ch.filter.SetValue("")
	ch.filter.Focus()
	ch.openModelList(h, toolName)
}

// FocusEffort puts the caret in the typed level.
func (ch *Choice) FocusEffort() { ch.typedEffort.Focus() }

// Blur takes the caret out of the choice and closes its list.
func (ch *Choice) Blur() {
	ch.filter.Blur()
	ch.typedEffort.Blur()
	ch.sugg = modelSuggest{}
}

// TypeEffort types into the level of a CLI that lists none, and keeps it.
func (ch *Choice) TypeEffort(h ChoiceHost, toolName string, msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	before := ch.typedEffort.Value()
	ch.typedEffort, cmd = ch.typedEffort.Update(msg)
	if ch.typedEffort.Value() != before {
		ch.keep(h, toolName)
	}
	return cmd
}

// TypeFilter narrows the open model list.
func (ch *Choice) TypeFilter(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	ch.filter, cmd = ch.filter.Update(msg)
	ch.filtering = true
	ch.sugg = modelSuggest{open: true}
	return cmd
}

// PickHighlighted picks the model the list highlights, if it lists any.
func (ch *Choice) PickHighlighted(h ChoiceHost, toolName string) {
	if list := ch.suggestions(h, toolName, ch.query()); len(list) > 0 {
		ch.pickModel(h, toolName, list[ch.sugg.index].model.Key())
	}
}

// PickEntry picks the model a list row shows.
func (ch *Choice) PickEntry(h ChoiceHost, toolName string, entry int) {
	if list := ch.suggestions(h, toolName, ch.query()); entry < len(list) {
		ch.pickModel(h, toolName, list[entry].model.Key())
	}
}

// StepList moves the list highlight; the row is already highlighted and
// enter picks it, so the first arrow moves too.
func (ch *Choice) StepList(h ChoiceHost, toolName string, delta int) {
	ch.sugg.chosen = true
	ch.sugg.move(len(ch.suggestions(h, toolName, ch.query())), delta)
}

// move reports false at an edge, so the form moves on instead of trapping
// the keys in the list.
func (s *modelSuggest) move(count, delta int) bool {
	if count == 0 {
		return false
	}
	if !s.chosen {
		if delta < 0 {
			return false
		}
		s.chosen = true
		return true
	}
	next := s.index + delta
	if next < 0 || next >= count {
		return false
	}
	s.index = next
	return true
}
