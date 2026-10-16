package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	savedClosed = iota
	savedInForm
	savedInQuick
	savedInFocus
)

type savedMatch struct {
	name         string
	instructions string
	formIndex    int
}

// savedList is the filterable catalog, the same one the new-session row,
// the quick prompt and a focused session open.
type savedList struct {
	where            int
	filter           textinput.Model
	cursor           int
	rows             []store.SessionPreset
	loading          bool
	err              string
	gen              int
	originX, originY int
	hits             []savedHit
}

type savedHit struct {
	line  int
	match int
}

type sessionPresetPanel struct {
	open, editing, confirm, loading, busy bool
	gen, cursor, focus                    int
	refreshFailed                         bool
	rows                                  []store.SessionPreset
	previous                              string
	name                                  textinput.Model
	instructions                          textarea.Model
}

type sessionPresetsLoadedMsg struct {
	gen                  int
	form, mutation, list bool
	committed            bool
	rows                 []store.SessionPreset
	err                  error
	selected             string
}

func (m *Model) loadSessionPresets(gen int, form bool) tea.Cmd {
	st := m.store
	return func() tea.Msg {
		rows, err := st.SessionPresets()
		return sessionPresetsLoadedMsg{gen: gen, form: form, rows: rows, err: err}
	}
}

func (m *Model) openSessionPresets() tea.Cmd {
	m.presets = sessionPresetPanel{open: true, loading: true, gen: m.nextComposerGen()}
	m.errBar.text = ""
	return m.loadSessionPresets(m.presets.gen, false)
}

func (m *Model) recordSessionPresets(msg sessionPresetsLoadedMsg) tea.Cmd {
	if msg.list {
		if m.savedList.where == savedClosed || m.savedList.gen != msg.gen {
			return nil
		}
		m.savedList.loading = false
		if msg.err != nil {
			m.savedList.err = msg.err.Error()
			return nil
		}
		m.savedList.rows = msg.rows
		m.savedList.err = ""
		return nil
	}
	if msg.form {
		if m.mode != modeForm || m.form.prompt.gen != msg.gen {
			return nil
		}
		m.form.presetsLoading = false
		if msg.err != nil {
			m.form.presetsError = "Reading saved prompts: " + msg.err.Error()
			return nil
		}
		m.form.presets = msg.rows
		if m.form.presetIndex > len(m.form.presets) {
			m.form.presetIndex = 0
		}
		return nil
	}
	p := &m.presets
	if m.mode != modeSettings || !p.open || p.gen != msg.gen {
		return nil
	}
	p.loading, p.busy = false, false
	if msg.err != nil {
		m.errBar.text = msg.err.Error()
		if msg.committed {
			p.editing, p.confirm = false, false
			p.previous = msg.selected
			p.rows = nil
			p.refreshFailed = true
			m.errBar.text = "Changes saved. Refresh failed. " + msg.err.Error()
		} else if !msg.mutation {
			p.rows = nil
			p.refreshFailed = true
			m.errBar.text = "Reading saved prompts: " + msg.err.Error()
		}
		return nil
	}
	p.refreshFailed = false
	p.rows = msg.rows
	p.cursor = min(p.cursor, max(0, len(p.rows)-1))
	for i, preset := range p.rows {
		if preset.Name == msg.selected {
			p.cursor = i
			break
		}
	}
	if msg.mutation {
		p.editing, p.confirm = false, false
	}
	m.errBar.text = ""
	return nil
}

func (m *Model) editSessionPreset(new bool) tea.Cmd {
	p := &m.presets
	p.gen = m.nextComposerGen()
	p.loading = false
	p.editing, p.confirm = true, false
	p.previous = ""
	p.name = textField("name", 60)
	p.instructions = textarea.New()
	p.instructions.CharLimit = 64 * 1024
	p.instructions.ShowLineNumbers = false
	p.instructions.Placeholder = "instructions"
	holdOpen(&p.instructions)
	if !new && p.cursor < len(p.rows) {
		preset := p.rows[p.cursor]
		p.previous = preset.Name
		p.name.SetValue(preset.Name)
		p.instructions.SetValue(preset.Instructions)
	}
	p.focus = 0
	m.errBar.text = ""
	return p.name.Focus()
}

func (m *Model) mutateSessionPreset(deleting bool) tea.Cmd {
	p := &m.presets
	if p.busy {
		return nil
	}
	p.busy = true
	m.errBar.text = ""
	gen, st, previous := p.gen, m.store, p.previous
	preset := store.SessionPreset{Name: p.name.Value(), Instructions: p.instructions.Value()}
	name := ""
	if deleting {
		if p.cursor >= len(p.rows) {
			p.busy = false
			return nil
		}
		name = p.rows[p.cursor].Name
	}
	return func() tea.Msg {
		var err error
		if deleting {
			_, err = st.DeleteSessionPreset(name)
		} else {
			err = st.SaveSessionPreset(previous, preset)
		}
		committed := err == nil
		var rows []store.SessionPreset
		if committed {
			rows, err = st.SessionPresets()
		}
		selected := ""
		if !deleting {
			selected = strings.TrimSpace(preset.Name)
		}
		return sessionPresetsLoadedMsg{gen: gen, mutation: true, committed: committed, rows: rows, err: err, selected: selected}
	}
}

func (m *Model) handleSessionPresetsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.presets
	if p.busy {
		return m, nil
	}
	msg = typedText(msg)
	key := msg.String()
	if p.confirm {
		switch key {
		case "esc":
			p.confirm = false
		case "enter":
			return m, m.mutateSessionPreset(true)
		}
		return m, nil
	}
	if p.editing {
		switch key {
		case "esc":
			p.editing = false
			return m, m.reloadSessionPresets()
		case "ctrl+s":
			return m, m.mutateSessionPreset(false)
		case "tab", "shift+tab":
			p.focus = 1 - p.focus
			return m, m.focusPresetEditor()
		}
		return m, m.typePresetEditor(msg)
	}
	switch key {
	case "esc":
		p.open = false
		m.errBar.text = ""
	case "n":
		return m, m.editSessionPreset(true)
	case "r":
		if p.refreshFailed && !p.loading {
			return m, m.reloadSessionPresets()
		}
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
	case "down", "j":
		p.cursor = min(max(0, len(p.rows)-1), p.cursor+1)
	case "enter", "e":
		if !p.loading && len(p.rows) > 0 {
			return m, m.editSessionPreset(false)
		}
	case "d":
		if !p.loading && len(p.rows) > 0 {
			p.confirm = true
			m.errBar.text = ""
		}
	}
	return m, nil
}

func (m *Model) focusPresetEditor() tea.Cmd {
	p := &m.presets
	p.name.Blur()
	p.instructions.Blur()
	if p.focus == 0 {
		return p.name.Focus()
	}
	return p.instructions.Focus()
}

func (m *Model) typePresetEditor(msg tea.KeyMsg) tea.Cmd {
	p := &m.presets
	var cmd tea.Cmd
	if p.focus == 0 {
		p.name, cmd = p.name.Update(msg)
		return cmd
	}
	p.instructions, cmd = p.instructions.Update(msg)
	return cmd
}

func (m *Model) reloadSessionPresets() tea.Cmd {
	p := &m.presets
	p.gen = m.nextComposerGen()
	p.loading = true
	m.errBar.text = ""
	return m.loadSessionPresets(p.gen, false)
}

func (m *Model) handleSessionPresetsClick(x, y int) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *Model) handleSessionPresetAction(key string) tea.Cmd {
	typ := tea.KeyRunes
	if key == "esc" {
		typ = tea.KeyEsc
	}
	_, cmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: typ, Runes: []rune(key)})
	return cmd
}

func (m *Model) viewSessionPresets() string {
	p := &m.presets
	width := m.cardWidth()
	if m.width > 0 {
		width = min(width, max(12, m.width-2))
	}
	inner := max(1, cardInnerWidth(width))
	var body []string
	add := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			body = append(body, ansi.Truncate(line, inner, "…"))
		}
	}
	hint := [][2]string{{"↑↓", "select"}, {"n", "new"}, {"↵", "edit"}, {"d", "delete"}, {"esc", "back"}}
	switch {
	case p.editing:
		p.name.Width = max(1, inner-3)
		p.instructions.SetWidth(inner)
		p.instructions.SetHeight(max(1, min(6, m.height-14)))
		add("name")
		add(textInputView(p.name))
		add("text")
		add(textAreaView(p.instructions))
		add("agents add these with agent-manager prompts add")
		if p.busy {
			hint = [][2]string{{"wait", "save in progress"}}
		} else {
			hint = [][2]string{{"tab", "field"}, {"ctrl+s", "save"}, {"esc", "discard"}}
		}
	case p.confirm:
		add("Delete " + p.rows[p.cursor].Name + "?")
		if p.busy {
			hint = [][2]string{{"wait", "delete in progress"}}
		} else {
			hint = [][2]string{{"↵", "delete"}, {"esc", "cancel"}}
		}
	default:
		room := max(1, m.height-12)
		first, last := sessionPresetWindow(len(p.rows), p.cursor, room)
		if p.loading {
			add("Loading saved prompts…")
		} else if p.refreshFailed {
			add("Catalog unavailable. Press r to load again.")
		} else if len(p.rows) == 0 {
			add("No saved prompts.")
		}
		if first > 0 {
			add(fmt.Sprintf("↑ %d more", first))
		}
		for i := first; i < last; i++ {
			add(presetAction(p.rows[i].Name, i == p.cursor))
		}
		if last < len(p.rows) {
			add(fmt.Sprintf("↓ %d more", len(p.rows)-last))
		}
		add("agents add these with agent-manager prompts add")
		if p.refreshFailed && !p.loading {
			hint = [][2]string{{"n", "new"}, {"r", "refresh"}, {"esc", "back"}}
		}
	}
	return m.cardSized(width, "Saved prompts", strings.Join(body, "\n"), hint)
}

func presetAction(text string, selected bool) string {
	if selected {
		return valueStyle.Render("› " + text)
	}
	return subtleStyle.Render("  " + text)
}

func (m *Model) stepSessionPreset(delta int) {
	count := len(m.form.presets) + 1
	m.form.presetIndex = (m.form.presetIndex + delta + count) % count
}

func (m *Model) formInitialPrompt() string {
	instructions := ""
	if m.form.presetIndex > 0 && m.form.presetIndex-1 < len(m.form.presets) {
		instructions = m.form.presets[m.form.presetIndex-1].Instructions
	}
	task, err := m.expandSaved(&m.form.prompt)
	if err != nil {
		task = m.form.prompt.message()
	}
	return launch.WithInstructions(instructions, task)
}

func (m *Model) viewFormSessionPresets(field func(string, string, int)) {
	f := &m.form
	if f.presetsLoading {
		field("saved prompts", "loading… (optional)", fieldPreset)
		return
	}
	if f.presetsError != "" {
		field("saved prompts", ansi.Wrap(f.presetsError, max(1, m.formValueWidth()), ""), fieldPreset)
		return
	}
	if len(f.presets) == 0 {
		return
	}
	name := "none"
	if f.presetIndex > 0 && f.presetIndex-1 < len(f.presets) {
		name = f.presets[f.presetIndex-1].Name
	}
	field("saved prompts", subtleStyle.Render("◂ ")+valueStyle.Render(name)+subtleStyle.Render(" ▸"), fieldPreset)
}

func (m *Model) viewFormSessionPresetDetails(add func(string, formHit), room int) {
	if room <= 0 {
		return
	}
	if m.savedList.where == savedInForm {
		lines := m.savedListLines(m.formValueWidth(), min(6, room))
		for i, line := range lines {
			entry := -1
			if i < len(m.savedList.hits) {
				entry = m.savedList.hits[i].match
			}
			add(strings.Repeat(" ", formLabelColumn)+line+"\n", formHit{field: fieldPreset, entry: entry})
		}
		return
	}
	if m.form.presetIndex <= 0 || m.form.presetIndex-1 >= len(m.form.presets) {
		return
	}
	label := "Instructions: "
	preview := savedPreview(m.form.presets[m.form.presetIndex-1].Instructions, max(0, m.formValueWidth()-ansi.StringWidth(label)))
	add(strings.Repeat(" ", formLabelColumn)+subtleStyle.Render(ansi.Truncate(label+preview, max(1, m.formValueWidth()), "…"))+"\n", formHit{field: fieldPreset, entry: -1})
}

func savedPreview(raw string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(strings.Join(strings.Fields(raw), " "), width, "…")
}

func sessionPresetWindow(count, cursor, room int) (int, int) {
	if count == 0 {
		return 0, 0
	}
	visible := min(count, max(1, room))
	first := max(0, min(cursor-visible/2, count-visible))
	return first, first + visible
}

func (m *Model) openSavedList(where int) tea.Cmd {
	filter := textinput.New()
	filter.Placeholder = "filter"
	filter.CharLimit = 60
	filter.Focus()
	m.savedList = savedList{where: where, filter: filter, gen: m.nextComposerGen()}
	if where == savedInQuick {
		m.quick.picking = pickNone
	}
	if where == savedInForm {
		m.savedList.rows = m.form.presets
		return nil
	}
	m.savedList.loading = true
	st := m.store
	gen := m.savedList.gen
	return func() tea.Msg {
		rows, err := st.SessionPresets()
		return sessionPresetsLoadedMsg{gen: gen, list: true, rows: rows, err: err}
	}
}

func (m *Model) closeSavedList() {
	m.savedList = savedList{}
}

func (m *Model) savedMatches() []savedMatch {
	rows := m.savedList.rows
	if m.savedList.where == savedInForm {
		rows = m.form.presets
	}
	query := strings.ToLower(strings.TrimSpace(m.savedList.filter.Value()))
	var matches []savedMatch
	if m.savedList.where == savedInForm && (query == "" || strings.Contains("none", query)) {
		matches = append(matches, savedMatch{name: "none"})
	}
	for i, row := range rows {
		if query != "" && !strings.Contains(strings.ToLower(row.Name), query) {
			continue
		}
		matches = append(matches, savedMatch{
			name:         row.Name,
			instructions: row.Instructions,
			formIndex:    i + 1,
		})
	}
	return matches
}

func (m *Model) savedListLines(width, visible int) []string {
	m.savedList.hits = nil
	if m.savedList.loading {
		return []string{mutedStyle.Render("loading saved prompts…")}
	}
	if m.savedList.err != "" {
		return []string{mutedStyle.Render(ansi.Truncate(m.savedList.err, max(1, width), "…"))}
	}
	matches := m.savedMatches()
	if len(matches) == 0 {
		return []string{mutedStyle.Render("no saved prompts match")}
	}
	if m.savedList.cursor >= len(matches) {
		m.savedList.cursor = len(matches) - 1
	}
	if m.savedList.cursor < 0 {
		m.savedList.cursor = 0
	}
	filter := textInputView(m.savedList.filter)
	lines := []string{filter}
	m.savedList.hits = append(m.savedList.hits, savedHit{line: 0, match: -1})
	visible = max(1, visible-1)
	first := 0
	if m.savedList.cursor >= visible {
		first = m.savedList.cursor - visible + 1
	}
	last := min(len(matches), first+visible)
	for i := first; i < last; i++ {
		marker, style := "  ", mutedStyle
		if i == m.savedList.cursor {
			marker = lipglossAccent()
			style = valueStyle
		}
		row := marker + style.Render(matches[i].name)
		lines = append(lines, ansi.Truncate(row, max(1, width), "…"))
		formIndex := matches[i].formIndex
		if m.savedList.where != savedInForm {
			formIndex = i
		}
		m.savedList.hits = append(m.savedList.hits, savedHit{line: len(lines) - 1, match: formIndex})
	}
	return lines
}

func lipglossAccent() string {
	return valueStyle.Render("❯ ")
}

func (m *Model) handleSavedListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = typedText(msg)
	switch msg.String() {
	case "esc", "ctrl+o":
		m.closeSavedList()
		return m, nil
	case "up":
		m.savedList.cursor = max(0, m.savedList.cursor-1)
		return m, nil
	case "down":
		matches := m.savedMatches()
		if len(matches) > 0 {
			m.savedList.cursor = min(m.savedList.cursor+1, len(matches)-1)
		}
		return m, nil
	case "enter":
		return m.pickSaved()
	}
	var cmd tea.Cmd
	m.savedList.filter, cmd = m.savedList.filter.Update(msg)
	m.savedList.cursor = 0
	return m, cmd
}

func (m *Model) pickSaved() (tea.Model, tea.Cmd) {
	matches := m.savedMatches()
	if m.savedList.loading || m.savedList.err != "" || len(matches) == 0 {
		return m, nil
	}
	if m.savedList.cursor >= len(matches) {
		m.savedList.cursor = len(matches) - 1
	}
	match := matches[m.savedList.cursor]
	where := m.savedList.where
	m.closeSavedList()
	switch where {
	case savedInForm:
		m.form.presetIndex = match.formIndex
	case savedInQuick:
		if match.name != "" && match.name != "none" {
			m.quick.insertSaved(match.name)
		}
	case savedInFocus:
		return m.pasteSaved(match.instructions)
	}
	return m, nil
}

func (m *Model) pasteSaved(text string) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok || m.mode != modeFocus {
		return m, nil
	}
	driver, id := m.tmux, sess.ID
	m.writeFocused(id, func() tea.Msg {
		return writeFailure(driver.Paste(id, text))
	})
	return m, nil
}

func (m *Model) clickSavedList(x, y int) (tea.Model, tea.Cmd) {
	if m.savedList.where == savedClosed {
		return m, nil
	}
	line := y - m.savedList.originY
	if x < m.savedList.originX || x >= m.cardRight-1-cardPaddingX {
		return m, nil
	}
	for _, hit := range m.savedList.hits {
		if hit.line != line || hit.match < 0 {
			continue
		}
		matches := m.savedMatches()
		for i, match := range matches {
			index := match.formIndex
			if m.savedList.where != savedInForm {
				index = i
			}
			if index == hit.match {
				m.savedList.cursor = i
				return m.pickSaved()
			}
		}
	}
	return m, nil
}
