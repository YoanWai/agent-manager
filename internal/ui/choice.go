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
	tool := m.cfg.Tools[toolName]
	if tool.Catalog == "" {
		return nil
	}
	if m.catalogs == nil {
		m.catalogs = map[string]*catalogState{}
	}
	state := m.catalogs[toolName]
	if state == nil {
		state = &catalogState{}
		m.catalogs[toolName] = state
	}
	recheck := catalogRecheck
	if state.err != nil {
		recheck = catalogRetry
	}
	if state.loading || (state.loaded && time.Since(state.checkedAt) < recheck) {
		return nil
	}
	state.loading = true
	configDir := m.configDir
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
	state := m.catalogs[msg.tool]
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
	tool, configDir := m.cfg.Tools[msg.tool], m.configDir
	return func() tea.Msg { return refreshCatalog(configDir, msg.tool, tool) }
}

func (m *Model) fitChoices() {
	if m.mode == modeForm {
		m.fitChoice(m.formTool(), &m.form.choice)
	}
	if m.quick.active {
		m.fitChoice(m.quickTool(), &m.quick.choice)
	}
}

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
}

func (ch *choice) query() string {
	if !ch.filtering {
		return ""
	}
	return ch.filter.Value()
}

func (m *Model) openModelList(toolName string, ch *choice) {
	ch.filtering = false
	ch.sugg = modelSuggest{open: true}
	if ch.model == "" {
		return
	}
	for i, entry := range m.modelSuggestions(toolName, ch, "") {
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

func (m *Model) newChoice(toolName string) choice {
	filter := textinput.New()
	filter.CharLimit = 200
	filter.Placeholder = toolName + "'s default"
	typed := textinput.New()
	typed.CharLimit = 40
	typed.Placeholder = "default"
	return choice{filter: filter, typedEffort: typed, recent: m.recentModels(toolName)}
}

// choiceAnswer is the CLI's latest answer, and whether it can be asked.
func (m *Model) choiceAnswer(toolName string) (*catalogState, bool) {
	if m.cfg.Tools[toolName].Catalog == "" {
		return nil, false
	}
	return m.catalogs[toolName], true
}

func (m *Model) choiceProfiles(toolName string) []catalog.Profile {
	if state, _ := m.choiceAnswer(toolName); state != nil && m.cfg.Tools[toolName].ProfileArgs != "" {
		return state.cat.Profiles
	}
	return nil
}

func (m *Model) choiceProfileName(toolName string, ch *choice) string {
	profiles := m.choiceProfiles(toolName)
	if ch.profile == 0 || ch.profile > len(profiles) {
		return ""
	}
	return profiles[ch.profile-1].Name
}

func (m *Model) choiceModels(toolName string, ch *choice) []catalog.Model {
	state, _ := m.choiceAnswer(toolName)
	if state == nil || m.cfg.Tools[toolName].ModelArgs == "" {
		return nil
	}
	return state.cat.ModelsFor(m.choiceProfileName(toolName, ch))
}

func (m *Model) pickedModel(toolName string, ch *choice) (catalog.Model, bool) {
	if ch.model == "" {
		return catalog.Model{}, false
	}
	matches := catalog.Match(m.choiceModels(toolName, ch), ch.model)
	if len(matches) != 1 {
		return catalog.Model{}, false
	}
	return matches[0], true
}

// effortModel is the model the effort applies to: the pick, else the one
// the CLI starts on.
func (m *Model) effortModel(toolName string, ch *choice) (catalog.Model, bool) {
	switch {
	case m.cfg.Tools[toolName].EffortArgs == "":
		return catalog.Model{}, false
	case ch.model != "":
		return m.pickedModel(toolName, ch)
	}
	return catalog.Default(m.choiceModels(toolName, ch))
}

func (m *Model) choiceEfforts(toolName string, ch *choice) []string {
	model, _ := m.effortModel(toolName, ch)
	return model.Efforts
}

func (m *Model) effortTyped(toolName string, ch *choice) bool {
	model, _ := m.effortModel(toolName, ch)
	return model.EffortTyped
}

func (m *Model) fitChoice(toolName string, ch *choice) {
	if ch.profile > len(m.choiceProfiles(toolName)) {
		ch.profile = 0
	}
	if ch.model != "" {
		if _, ok := m.pickedModel(toolName, ch); !ok {
			ch.model = ""
		}
	}
	if ch.effort > len(m.choiceEfforts(toolName, ch)) {
		ch.effort = 0
	}
	if ch.sugg.index >= len(m.modelSuggestions(toolName, ch, ch.query())) {
		ch.sugg.index, ch.sugg.chosen, ch.sugg.offset = 0, false, 0
	}
}

// pickModel keeps the effort when the new model offers the same level.
func (m *Model) pickModel(toolName string, ch *choice, key string) {
	level := m.choiceEffort(toolName, ch)
	ch.model = key
	ch.filter.SetValue(key)
	ch.filter.CursorEnd()
	ch.filtering = false
	ch.effort = 0
	for i, offered := range m.choiceEfforts(toolName, ch) {
		if offered == level {
			ch.effort = i + 1
		}
	}
	ch.sugg = modelSuggest{}
}

func (m *Model) choiceEffort(toolName string, ch *choice) string {
	if m.effortTyped(toolName, ch) {
		return strings.TrimSpace(ch.typedEffort.Value())
	}
	efforts := m.choiceEfforts(toolName, ch)
	if ch.effort == 0 || ch.effort > len(efforts) {
		return ""
	}
	return efforts[ch.effort-1]
}

func (m *Model) cycleChoiceEffort(toolName string, ch *choice, delta int) {
	count := len(m.choiceEfforts(toolName, ch)) + 1
	ch.effort = (ch.effort + delta + count) % count
}

func (m *Model) cycleChoiceProfile(toolName string, ch *choice, delta int) {
	count := len(m.choiceProfiles(toolName)) + 1
	ch.profile = (ch.profile + delta + count) % count
	m.fitChoice(toolName, ch)
}

func (m *Model) launchChoice(toolName string, ch *choice, typed string) (config.Choice, error) {
	typed = strings.TrimSpace(typed)
	if typed != "" && typed != ch.model {
		if matches := catalog.Match(m.choiceModels(toolName, ch), typed); len(matches) == 1 {
			m.pickModel(toolName, ch, matches[0].Key())
		} else {
			return config.Choice{}, fmt.Errorf("model %q is not one %s lists: pick one from the list", typed, toolName)
		}
	}
	picked := config.Choice{Profile: m.choiceProfileName(toolName, ch), Effort: m.choiceEffort(toolName, ch)}
	if model, ok := m.pickedModel(toolName, ch); ok {
		picked.Model, picked.Provider = model.ID, model.Provider
	}
	return picked, nil
}

type suggestion struct {
	model  catalog.Model
	recent bool
}

const modelListRows = 9

// modelSuggestions lists the recent picks first, then the rest.
func (m *Model) modelSuggestions(toolName string, ch *choice, query string) []suggestion {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := func(model catalog.Model) bool {
		return query == "" || strings.Contains(strings.ToLower(model.Key()), query) || strings.Contains(strings.ToLower(model.Label), query)
	}
	models := m.choiceModels(toolName, ch)
	var list []suggestion
	for _, key := range ch.recent {
		for _, model := range catalog.Match(models, key) {
			if matches(model) {
				list = append(list, suggestion{model: model, recent: true})
			}
		}
	}
	for _, model := range models {
		if matches(model) && !slices.Contains(ch.recent, model.Key()) {
			list = append(list, suggestion{model: model})
		}
	}
	return list
}

const recentModelLimit = 3

func recentModelsKey(toolName string) string { return "recent_models." + toolName }

func (m *Model) recentModels(toolName string) []string {
	var keys []string
	raw, err := m.store.Setting(recentModelsKey(toolName))
	if err == nil && raw != "" {
		err = json.Unmarshal([]byte(raw), &keys)
	}
	if err != nil {
		m.errBar.text = "reading recent models: " + err.Error()
		return nil
	}
	return keys
}

func (m *Model) rememberModel(toolName string, picked config.Choice) {
	if picked.Model == "" {
		return
	}
	key := catalog.Model{ID: picked.Model, Provider: picked.Provider}.Key()
	keys := []string{key}
	for _, earlier := range m.recentModels(toolName) {
		if earlier != key && len(keys) < recentModelLimit {
			keys = append(keys, earlier)
		}
	}
	raw, err := json.Marshal(keys)
	if err == nil {
		err = m.store.SetSetting(recentModelsKey(toolName), string(raw))
	}
	if err != nil {
		m.errBar.text = "remembering the model: " + err.Error()
	}
}

// modelRowNote says why the model row has no list, or reports it has one.
func (m *Model) modelRowNote(toolName string) (string, bool) {
	state, supported := m.choiceAnswer(toolName)
	switch {
	case !supported || m.cfg.Tools[toolName].ModelArgs == "":
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
func (m *Model) effortRow(toolName string, ch *choice) (value string, shown, active bool) {
	state, supported := m.choiceAnswer(toolName)
	if !supported || m.cfg.Tools[toolName].EffortArgs == "" {
		return subtleStyle.Render("not supported by " + toolName), true, false
	}
	if state == nil || !state.loaded || len(m.choiceModels(toolName, ch)) == 0 {
		return "", false, false
	}
	model, known := m.effortModel(toolName, ch)
	switch {
	case !known:
		return subtleStyle.Render("pick a model to see its levels"), true, false
	case model.EffortTyped:
		return textInputView(ch.typedEffort) + "  " + subtleStyle.Render("typed · "+toolName+" lists no levels"), true, true
	case len(model.Efforts) == 0:
		return "", false, false
	}
	level := m.choiceEffort(toolName, ch)
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

func (m *Model) profileRow(toolName string, ch *choice) (string, bool) {
	profiles := m.choiceProfiles(toolName)
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

// viewModelSuggestions returns each line's list index in entries, -1 for a
// heading.
func (m *Model) viewModelSuggestions(toolName string, ch *choice, query string, indent, width int) (lines []string, entries []int) {
	list := m.modelSuggestions(toolName, ch, query)
	headingStyle := lipgloss.NewStyle().Foreground(colorSubtle).Italic(true)
	var rows []string
	heading := func(text string) {
		rows = append(rows, "  "+headingStyle.Render(text))
		entries = append(entries, -1)
	}
	highlight := -1
	state, _ := m.choiceAnswer(toolName)
	for i, entry := range list {
		if i == 0 && entry.recent {
			heading("recent")
		}
		if !entry.recent && (i == 0 || list[i-1].recent) {
			source := fmt.Sprintf("from %s · %d models", toolName, len(m.choiceModels(toolName, ch)))
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
	if len(rows) <= modelListRows {
		for _, row := range rows {
			lines = append(lines, pad+ansi.Truncate(row, width, "…"))
		}
		return lines, entries
	}
	ch.sugg.offset = scrollToShow(ch.sugg.offset, highlight, len(rows), modelListRows, entries)
	bar := scrollBar(ch.sugg.offset, len(rows), modelListRows)
	for i, row := range rows[ch.sugg.offset : ch.sugg.offset+modelListRows] {
		lines = append(lines, pad+padRight(row, width-2)+" "+bar[i])
	}
	return lines, entries[ch.sugg.offset : ch.sugg.offset+modelListRows]
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
