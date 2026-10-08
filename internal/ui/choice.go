package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type catalogState struct {
	cat       catalog.Catalog
	err       error
	loaded    bool
	loading   bool
	checkedAt time.Time
}

// A running manager rereads the kept answer to catch a CLI upgrade or a
// spawn's refresh, and retries a failure sooner, since a login may be all
// it lacked.
const (
	catalogRecheck = 10 * time.Minute
	catalogRetry   = 30 * time.Second
)

type catalogMsg struct {
	tool string
	cat  catalog.Catalog
	err  error
	// stale is shown while a fresh answer is asked.
	stale bool
}

// ensureCatalog reads the kept answer or asks the CLI off the update path,
// since an answer takes up to seconds.
func (m *Model) ensureCatalog(toolName string) tea.Cmd {
	tool := m.services.cfg.Tools[toolName]
	if tool.Catalog == "" {
		return nil
	}
	if m.ledger.catalogs == nil {
		m.ledger.catalogs = map[string]*catalogState{}
	}
	state := m.ledger.catalogs[toolName]
	if state == nil {
		state = &catalogState{}
		m.ledger.catalogs[toolName] = state
	}
	recheck := catalogRecheck
	if state.err != nil {
		recheck = catalogRetry
	}
	if state.loading || (state.loaded && time.Since(state.checkedAt) < recheck) {
		return nil
	}
	state.loading = true
	configDir := m.services.configDir
	return func() tea.Msg {
		if cat, fresh, ok := catalog.Cached(configDir, toolName, tool); ok {
			return catalogMsg{tool: toolName, cat: cat, stale: !fresh}
		}
		return refreshCatalog(configDir, toolName, tool)
	}
}

func refreshCatalog(configDir, toolName string, tool config.Tool) tea.Msg {
	cat, err := catalog.Refresh(context.Background(), configDir, toolName, tool)
	return catalogMsg{tool: toolName, cat: cat, err: err}
}

func (m *Model) handleCatalog(msg catalogMsg) tea.Cmd {
	state := m.ledger.catalogs[msg.tool]
	if msg.err == nil {
		state.cat = msg.cat
	}
	state.err = msg.err
	state.loaded = true
	state.loading = msg.stale
	state.checkedAt = time.Now()
	m.fitChoices()
	if !msg.stale {
		return nil
	}
	tool, configDir := m.services.cfg.Tools[msg.tool], m.services.configDir
	return func() tea.Msg { return refreshCatalog(configDir, msg.tool, tool) }
}

func (m *Model) fitChoices() {
	if m.mode == modeForm {
		m.form.choice.restore(m, m.form.tool())
		m.form.choice.fit(m, m.form.tool())
	}
	if m.quick.active {
		m.quick.choice.restore(m, m.quick.tool())
		m.quick.choice.fit(m, m.quick.tool())
	}
}

// choiceHost is what a choice reads from the root: the CLI's flags, its
// latest catalog answer, and the cached choice settings, with the status
// bar for an unreadable one and the effect lane that keeps a new one.
type choiceHost interface {
	choiceTool(toolName string) config.Tool
	choiceCatalog(toolName string) *catalogState
	choiceSetting(key string) string
	saveChoiceSetting(key, value string)
	reportErr(text string)
}

func (m *Model) choiceTool(toolName string) config.Tool { return m.services.cfg.Tools[toolName] }

func (m *Model) choiceCatalog(toolName string) *catalogState { return m.ledger.catalogs[toolName] }

func (m *Model) choiceSetting(key string) string { return m.settings.cache.value(key) }

// choice is what a new session launches its CLI with. Index 0 of profile
// and effort, and an empty model, leave the CLI's own default.
type choice struct {
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
	saved *config.Choice
}

func (ch *choice) query() string {
	if !ch.filtering {
		return ""
	}
	return ch.filter.Value()
}

func (ch *choice) openModelList(h choiceHost, toolName string) {
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

func newChoice(h choiceHost, toolName string) choice {
	filter := textinput.New()
	filter.CharLimit = 200
	filter.Placeholder = toolName + "'s default"
	typed := textinput.New()
	typed.CharLimit = 40
	typed.Placeholder = "default"
	ch := choice{filter: filter, typedEffort: typed, recent: recentModels(h, toolName), saved: savedChoice(h, toolName)}
	ch.restore(h, toolName)
	return ch
}

func savedChoiceKey(toolName string) string { return "choice." + toolName }

func savedChoice(h choiceHost, toolName string) *config.Choice {
	raw := h.choiceSetting(savedChoiceKey(toolName))
	if raw == "" {
		return nil
	}
	var saved config.Choice
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		h.reportErr("reading the last choice: " + err.Error())
		return nil
	}
	return &saved
}

// keep saves the choice as the CLI's own, so the next form or quick
// prompt on it starts there.
func (ch *choice) keep(h choiceHost, toolName string) {
	raw, err := json.Marshal(ch.current(h, toolName))
	if err != nil {
		h.reportErr("saving the choice: " + err.Error())
		return
	}
	h.saveChoiceSetting(savedChoiceKey(toolName), string(raw))
}

// restore places the saved choice once the CLI's answer is in, keeping
// only what that answer still offers.
func (ch *choice) restore(h choiceHost, toolName string) {
	state, _ := choiceAnswer(h, toolName)
	if ch.saved == nil || state == nil || !state.loaded {
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

// choiceAnswer is the CLI's latest answer, and whether it can be asked.
func choiceAnswer(h choiceHost, toolName string) (*catalogState, bool) {
	if h.choiceTool(toolName).Catalog == "" {
		return nil, false
	}
	return h.choiceCatalog(toolName), true
}

func choiceProfiles(h choiceHost, toolName string) []catalog.Profile {
	if state, _ := choiceAnswer(h, toolName); state != nil && h.choiceTool(toolName).ProfileArgs != "" {
		return state.cat.Profiles
	}
	return nil
}

func (ch *choice) profileName(h choiceHost, toolName string) string {
	profiles := choiceProfiles(h, toolName)
	if ch.profile == 0 || ch.profile > len(profiles) {
		return ""
	}
	return profiles[ch.profile-1].Name
}

func (ch *choice) models(h choiceHost, toolName string) []catalog.Model {
	state, _ := choiceAnswer(h, toolName)
	if state == nil || h.choiceTool(toolName).ModelArgs == "" {
		return nil
	}
	return state.cat.ModelsFor(ch.profileName(h, toolName))
}

func (ch *choice) pickedModel(h choiceHost, toolName string) (catalog.Model, bool) {
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
func (ch *choice) effortModel(h choiceHost, toolName string) (catalog.Model, bool) {
	switch {
	case h.choiceTool(toolName).EffortArgs == "":
		return catalog.Model{}, false
	case ch.model != "":
		return ch.pickedModel(h, toolName)
	}
	return catalog.Default(ch.models(h, toolName))
}

func (ch *choice) efforts(h choiceHost, toolName string) []string {
	model, _ := ch.effortModel(h, toolName)
	return model.Efforts
}

func (ch *choice) effortTyped(h choiceHost, toolName string) bool {
	model, _ := ch.effortModel(h, toolName)
	return model.EffortTyped
}

func (ch *choice) fit(h choiceHost, toolName string) {
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
func (ch *choice) pickModel(h choiceHost, toolName string, key string) {
	level := ch.effortLevel(h, toolName)
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

func (ch *choice) effortLevel(h choiceHost, toolName string) string {
	if ch.effortTyped(h, toolName) {
		return strings.TrimSpace(ch.typedEffort.Value())
	}
	efforts := ch.efforts(h, toolName)
	if ch.effort == 0 || ch.effort > len(efforts) {
		return ""
	}
	return efforts[ch.effort-1]
}

func (ch *choice) cycleEffort(h choiceHost, toolName string, delta int) {
	count := len(ch.efforts(h, toolName)) + 1
	ch.effort = (ch.effort + delta + count) % count
	ch.keep(h, toolName)
}

func (ch *choice) cycleProfile(h choiceHost, toolName string, delta int) {
	count := len(choiceProfiles(h, toolName)) + 1
	ch.profile = (ch.profile + delta + count) % count
	ch.fit(h, toolName)
	ch.keep(h, toolName)
}

func (ch *choice) launch(h choiceHost, toolName string, typed string) (config.Choice, error) {
	typed = strings.TrimSpace(typed)
	if typed != "" && typed != ch.model {
		if matches := catalog.Match(ch.models(h, toolName), typed); len(matches) == 1 {
			ch.pickModel(h, toolName, matches[0].Key())
		} else {
			return config.Choice{}, fmt.Errorf("model %q is not one %s lists: pick one from the list", typed, toolName)
		}
	}
	return ch.current(h, toolName), nil
}

func choiceModelKey(c config.Choice) string {
	return catalog.Model{ID: c.Model, Provider: c.Provider}.Key()
}

func (ch *choice) current(h choiceHost, toolName string) config.Choice {
	picked := config.Choice{Profile: ch.profileName(h, toolName), Effort: ch.effortLevel(h, toolName)}
	if model, ok := ch.pickedModel(h, toolName); ok {
		picked.Model, picked.Provider = model.ID, model.Provider
	}
	return picked
}

type suggestion struct {
	model  catalog.Model
	recent bool
}

const modelListRows = 9

// suggestions lists the recent picks first, then the rest.
func (ch *choice) suggestions(h choiceHost, toolName string, query string) []suggestion {
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

const recentModelLimit = 3

func recentModelsKey(toolName string) string { return "recent_models." + toolName }

func recentModels(h choiceHost, toolName string) []string {
	var keys []string
	raw := h.choiceSetting(recentModelsKey(toolName))
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		h.reportErr("reading recent models: " + err.Error())
		return nil
	}
	return keys
}

func rememberModel(h choiceHost, toolName string, picked config.Choice) {
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
		h.reportErr("remembering the model: " + err.Error())
		return
	}
	h.saveChoiceSetting(recentModelsKey(toolName), string(raw))
}

// modelRowNote says why the model row has no list, or reports it has one.
func modelRowNote(h choiceHost, toolName string) (string, bool) {
	state, supported := choiceAnswer(h, toolName)
	switch {
	case !supported || h.choiceTool(toolName).ModelArgs == "":
		return subtleStyle.Render("not supported by " + toolName), false
	case state == nil || (!state.loaded && state.loading):
		return subtleStyle.Render("reading models from " + toolName + "…"), false
	case state.err != nil && len(state.cat.Models) == 0:
		return warnStyle.Render("couldn't read models from " + toolName + ": " + state.err.Error()), false
	case len(state.cat.Models) == 0 && len(state.cat.Profiles) == 0:
		return subtleStyle.Render(toolName + " lists no models"), false
	}
	return "", true
}

// effortRow is the row's value, whether it shows, and whether it takes keys.
func (ch *choice) effortRow(h choiceHost, toolName string) (value string, shown, active bool) {
	state, supported := choiceAnswer(h, toolName)
	if !supported || h.choiceTool(toolName).EffortArgs == "" {
		return subtleStyle.Render("not supported by " + toolName), true, false
	}
	if state == nil || !state.loaded || len(ch.models(h, toolName)) == 0 {
		return "", false, false
	}
	model, known := ch.effortModel(h, toolName)
	switch {
	case !known:
		return subtleStyle.Render("pick a model to see its levels"), true, false
	case model.EffortTyped:
		return textInputView(ch.typedEffort) + "  " + subtleStyle.Render("typed · "+toolName+" lists no levels"), true, true
	case len(model.Efforts) == 0:
		return "", false, false
	}
	level := ch.effortLevel(h, toolName)
	shownLevel := subtleStyle.Render("default")
	if level != "" {
		shownLevel = valueStyle.Render(level)
	}
	value = subtleStyle.Render("◂ ") + shownLevel + subtleStyle.Render(" ▸")
	if model.DefaultEffort != "" {
		value += "  " + subtleStyle.Render("default is "+model.DefaultEffort)
	}
	return value, true, true
}

func (ch *choice) profileRow(h choiceHost, toolName string) (string, bool) {
	profiles := choiceProfiles(h, toolName)
	if len(profiles) == 0 {
		return "", false
	}
	value := subtleStyle.Render(toolName + "'s default")
	if ch.profile > 0 {
		profile := profiles[ch.profile-1]
		value = valueStyle.Render(profile.Name)
		if profile.Detail != "" {
			value += "  " + subtleStyle.Render(profile.Detail)
		}
	}
	return subtleStyle.Render("◂ ") + value + subtleStyle.Render(" ▸"), true
}

// viewSuggestions returns each line's list index in entries, -1 for a
// heading.
func (ch *choice) viewSuggestions(h choiceHost, toolName string, query string, indent, width, visible int) (lines []string, entries []int) {
	list := ch.suggestions(h, toolName, query)
	headingStyle := lipgloss.NewStyle().Foreground(colorSubtle).Italic(true)
	var rows []string
	heading := func(text string) {
		rows = append(rows, "  "+headingStyle.Render(text))
		entries = append(entries, -1)
	}
	highlight := -1
	state, _ := choiceAnswer(h, toolName)
	for i, entry := range list {
		if i == 0 && entry.recent {
			heading("recent")
		}
		if !entry.recent && (i == 0 || list[i-1].recent) {
			source := fmt.Sprintf("from %s · %d models", toolName, len(ch.models(h, toolName)))
			if state.err != nil {
				source += " · refresh failed: " + state.err.Error()
			}
			heading(source)
		}
		marker, style := "  ", mutedStyle
		if i == ch.sugg.index {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			style = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
			highlight = len(rows)
		}
		row := marker + style.Render(entry.model.ID)
		if detail := modelDetail(entry.model); detail != "" {
			row += "  " + subtleStyle.Render(detail)
		}
		rows = append(rows, row)
		entries = append(entries, i)
	}
	if len(list) == 0 {
		heading("no model matches " + strings.TrimSpace(query))
	}
	pad := strings.Repeat(" ", indent)
	if len(rows) <= visible {
		for _, row := range rows {
			lines = append(lines, pad+ansi.Truncate(row, width, "…"))
		}
		return lines, entries
	}
	ch.sugg.offset = scrollToShow(ch.sugg.offset, highlight, len(rows), visible, entries)
	bar := scrollBar(ch.sugg.offset, len(rows), visible)
	for i, row := range rows[ch.sugg.offset : ch.sugg.offset+visible] {
		lines = append(lines, pad+padRight(row, width-2)+" "+bar[i])
	}
	return lines, entries[ch.sugg.offset : ch.sugg.offset+visible]
}

// scrollToShow returns to the top on the first entry, so the headings above
// it show.
func scrollToShow(offset, highlight, total, visible int, entries []int) int {
	switch {
	case highlight < 0:
	case highlight == slices.IndexFunc(entries, func(entry int) bool { return entry >= 0 }):
		offset = 0
	case highlight < offset:
		offset = highlight
	case highlight >= offset+visible:
		offset = highlight - visible + 1
	}
	return min(max(offset, 0), total-visible)
}

func scrollBar(offset, total, visible int) []string {
	thumb := max(visible*visible/total, 1)
	start := offset * (visible - thumb) / (total - visible)
	track, lit := subtleStyle.Render("│"), lipgloss.NewStyle().Foreground(colorAccent).Render("┃")
	bar := make([]string, visible)
	for i := range bar {
		bar[i] = track
		if i >= start && i < start+thumb {
			bar[i] = lit
		}
	}
	return bar
}

// modelDetail is the provider a model routes through, or the CLI's name for
// it when that says more than the id.
func modelDetail(model catalog.Model) string {
	if model.Provider != "" {
		return model.Provider
	}
	if model.Label != model.ID {
		return model.Label
	}
	return ""
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
