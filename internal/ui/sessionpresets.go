package ui

import (
	"errors"
	"fmt"
	"github.com/atotto/clipboard"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type sessionPresetHit struct {
	action string
	index  int
}
type sessionPresetPanel struct {
	open, editing, confirm, loading, busy, pasting bool
	gen, cursor, focus                             int
	rows                                           []store.SessionPreset
	previous                                       string
	name                                           textinput.Model
	instructions                                   sessionPresetInstructions
	hits                                           []sessionPresetHit
}
type sessionPresetsLoadedMsg struct {
	gen            int
	form, mutation bool
	rows           []store.SessionPreset
	err            error
	selected       string
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
	if msg.form {
		if m.mode != modeForm || m.form.prompt.gen != msg.gen {
			return nil
		}
		m.form.presetsLoading = false
		if msg.err != nil {
			m.form.presetsError = "Reading presets: " + msg.err.Error()
			return nil
		}
		m.form.presets = msg.rows
		return nil
	}
	p := &m.presets
	if m.mode != modeSettings || !p.open || p.gen != msg.gen {
		return nil
	}
	p.loading, p.busy = false, false
	if msg.err != nil {
		m.errBar.text = msg.err.Error()
		return nil
	}
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
	p.instructions = sessionPresetInstructions{Model: textarea.New()}
	p.instructions.MaxHeight = 0
	p.instructions.CharLimit = 64 * 1024
	p.instructions.ShowLineNumbers = false
	p.instructions.Placeholder = "instructions for the new session"
	holdOpen(&p.instructions.Model)
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
	if p.pasting {
		m.errBar.text = "Reading clipboard; try save again when it finishes"
		return nil
	}
	p.busy = true
	m.errBar.text = ""
	gen, st, previous := p.gen, m.store, p.previous
	preset := store.SessionPreset{Name: p.name.Value(), Instructions: p.instructions.Value()}
	name := ""
	if deleting {
		name = p.rows[p.cursor].Name
	}
	return func() tea.Msg {
		var err error
		if deleting {
			_, err = st.DeleteSessionPreset(name)
		} else {
			err = st.SaveSessionPreset(previous, preset)
		}
		var rows []store.SessionPreset
		if err == nil {
			rows, err = st.SessionPresets()
		}
		selected := ""
		if !deleting {
			selected = strings.TrimSpace(preset.Name)
		}
		return sessionPresetsLoadedMsg{gen: gen, mutation: true, rows: rows, err: err, selected: selected}
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
		case "ctrl+v":
			if p.focus == 1 && !p.instructions.readOnly && !p.pasting {
				gen := p.gen
				p.pasting = true
				return m, func() tea.Msg {
					value, err := readSessionPresetClipboard()
					return sessionPresetPasteMsg{gen: gen, text: value, err: err}
				}
			}
			return m, nil
		case "tab", "shift+tab":
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			p.focus = (p.focus + delta + 4) % 4
		case "enter":
			if p.focus == 2 {
				return m, m.mutateSessionPreset(false)
			}
			if p.focus == 3 {
				p.editing = false
				return m, m.reloadSessionPresets()
			}
		}
		p.name.Blur()
		p.instructions.Blur()
		var cmd tea.Cmd
		switch p.focus {
		case 0:
			p.name.Focus()
			p.name, cmd = p.name.Update(msg)
		case 1:
			p.instructions.Focus()
			var err error
			p.instructions, cmd, err = p.instructions.UpdateLiteral(msg)
			if err != nil {
				m.errBar.text = err.Error()
			}
		}
		return m, cmd
	}
	switch key {
	case "esc":
		p.open = false
		m.errBar.text = ""
	case "n":
		return m, m.editSessionPreset(true)
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
func (m *Model) reloadSessionPresets() tea.Cmd {
	p := &m.presets
	p.gen = m.nextComposerGen()
	p.loading = true
	m.errBar.text = ""
	return m.loadSessionPresets(p.gen, false)
}
func (m *Model) handleSessionPresetsClick(x, y int) (tea.Model, tea.Cmd) {
	p := &m.presets
	line := y - m.cardTop - 2
	if p.busy || x < m.cardLeft || x >= m.cardRight || line < 0 || line >= len(p.hits) {
		return m, nil
	}
	hit := p.hits[line]
	switch hit.action {
	case "row":
		p.cursor = hit.index
	case "name":
		p.focus = 0
		p.instructions.Blur()
		return m, p.name.Focus()
	case "instructions":
		p.focus = 1
		p.name.Blur()
		return m, p.instructions.Focus()
	case "save":
		return m, m.mutateSessionPreset(false)
	case "cancel":
		return m, m.handleSessionPresetAction("esc")
	case "new":
		return m, m.handleSessionPresetAction("n")
	case "edit":
		return m, m.handleSessionPresetAction("e")
	case "delete":
		return m, m.handleSessionPresetAction("d")
	case "confirm":
		return m, m.mutateSessionPreset(true)
	}
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
	p.hits = nil
	var body []string
	add := func(text, action string, index int) {
		for _, line := range strings.Split(text, "\n") {
			body = append(body, ansi.Truncate(line, inner, "…"))
			p.hits = append(p.hits, sessionPresetHit{action, index})
		}
	}
	hint := [][2]string{{"↑↓", "select"}, {"n", "new"}, {"↵/e", "edit"}, {"d", "delete"}, {"esc", "back"}}
	if inner < 30 {
		hint = [][2]string{{"↑↓", "select"}, {"n/e/d", "actions"}, {"esc", "back"}}
	}
	switch {
	case p.editing:
		p.name.Width = max(1, inner-3)
		p.name.SetCursor(p.name.Position())
		p.instructions.SetWidth(inner)
		p.instructions.SetHeight(max(1, min(6, m.height-16)))
		add("Name", "", 0)
		add(textInputView(p.name), "name", 0)
		if p.instructions.readOnly {
			add("Read-only instructions", "", 0)
			add(p.instructions.reason, "", 0)
			add(safePresetPreview(p.instructions.Value()), "", 0)
		} else {
			add("Instructions", "", 0)
			add("⇥ tab · ␍ return · ␛ literal marker", "", 0)
			add(textAreaView(p.instructions.Model), "instructions", 0)
			if p.pasting {
				add("Reading clipboard…", "", 0)
			}
		}
		if p.busy {
			add("Saving…", "", 0)
			hint = [][2]string{{"wait", "save in progress"}}
		} else {
			add(presetAction("[ Save ]", p.focus == 2), "save", 0)
			add(presetAction("[ Cancel ]", p.focus == 3), "cancel", 0)
			hint = [][2]string{{"tab", "field"}, {"ctrl+s", "save"}, {"esc", "discard"}}
		}
	case p.confirm:
		add("Delete "+p.rows[p.cursor].Name+"?", "", 0)
		if p.busy {
			add("Deleting…", "", 0)
			hint = [][2]string{{"wait", "delete in progress"}}
		} else {
			add("[ Delete ]", "confirm", 0)
			add("[ Cancel ]", "cancel", 0)
			hint = [][2]string{{"↵", "delete"}, {"esc", "cancel"}}
		}
	default:
		room := max(1, m.height-17)
		first, last := sessionPresetWindow(len(p.rows), p.cursor, room)
		if p.loading {
			add("Loading presets…", "", 0)
		} else if len(p.rows) == 0 {
			add("No presets. Create named instructions here.", "", 0)
		}
		if first > 0 {
			add(fmt.Sprintf("↑ %d more", first), "", 0)
		}
		for i := first; i < last; i++ {
			add(presetAction(p.rows[i].Name, i == p.cursor), "row", i)
		}
		if last < len(p.rows) {
			add(fmt.Sprintf("↓ %d more", len(p.rows)-last), "", 0)
		}
		add("[ New ]", "new", 0)
		if len(p.rows) > 0 && !p.loading {
			add("[ Edit ]", "edit", 0)
			add("[ Delete ]", "delete", 0)
		}
		add("[ Back ]", "cancel", 0)
	}
	return m.cardSized(width, "Session presets", strings.Join(body, "\n"), hint)
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
	if m.form.presetIndex > 0 {
		instructions = m.form.presets[m.form.presetIndex-1].Instructions
	}
	return launch.WithInstructions(instructions, m.form.prompt.message())
}
func (m *Model) viewFormSessionPresets(add func(string, formHit), field func(string, string, int)) {
	f := &m.form
	if f.presetsLoading {
		field("preset", "loading… (optional)", fieldPreset)
		return
	}
	if f.presetsError != "" {
		field("preset", ansi.Wrap(f.presetsError, max(1, m.formValueWidth()), ""), fieldPreset)
		return
	}
	if len(f.presets) == 0 {
		return
	}
	name := "none"
	if f.presetIndex > 0 {
		name = f.presets[f.presetIndex-1].Name
	}
	field("preset", subtleStyle.Render("◂ ")+valueStyle.Render(name)+subtleStyle.Render(" ▸"), fieldPreset)
	if f.focus == fieldPreset {
		count := len(f.presets) + 1
		first, last := sessionPresetWindow(count, f.presetIndex, min(4, max(1, m.height-22)))
		for i := first; i < last; i++ {
			label := "none"
			if i > 0 {
				label = f.presets[i-1].Name
			}
			add(strings.Repeat(" ", formLabelColumn)+ansi.Truncate(presetAction(label, i == f.presetIndex), max(1, m.formValueWidth()), "…")+"\n", formHit{field: fieldPreset, entry: i})
		}
	}
	if f.presetIndex > 0 {
		preview := safePresetPreview(f.presets[f.presetIndex-1].Instructions)
		add(strings.Repeat(" ", formLabelColumn)+subtleStyle.Render(ansi.Truncate("Instructions: "+preview, max(1, m.formValueWidth()), "…"))+"\n", formHit{field: fieldPreset, entry: -1})
	}
}

func sessionPresetWindow(count, cursor, room int) (int, int) {
	visible := min(count, max(1, room))
	first := max(0, min(cursor-visible/2, count-visible))
	return first, first + visible
}

// The textarea sanitizes controls. Display escapes keep its editing behavior
// without changing the instruction bytes, including literal escape glyphs.
type sessionPresetInstructions struct {
	textarea.Model
	raw      string
	readOnly bool
	reason   string
}

func (in *sessionPresetInstructions) SetValue(raw string) {
	in.raw = raw
	in.reason = instructionEditorLimit(raw)
	in.readOnly = in.reason != ""
	if !in.readOnly {
		in.Model.SetValue(encodePresetInstructions(raw))
	}
}
func (in sessionPresetInstructions) Value() string { return in.raw }
func instructionEditorLimit(raw string) string {
	if strings.Count(raw, "\n") >= 10000 {
		return "More than 10,000 lines; rename or delete."
	}
	for _, r := range raw {
		if r == utf8.RuneError {
			return "Unsupported replacement rune; rename or delete."
		}
		if unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return "Unsupported controls; rename or delete."
		}
	}
	return ""
}
func encodePresetInstructions(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch r {
		case '\t':
			b.WriteRune('⇥')
		case '\r':
			b.WriteRune('␍')
		case '⇥', '␍', '␛':
			b.WriteRune('␛')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
func decodePresetInstructions(encoded string) (string, error) {
	var b strings.Builder
	escaped := false
	for _, r := range encoded {
		if escaped {
			if r != '⇥' && r != '␍' && r != '␛' {
				return "", errors.New("Keep literal marker pairs together when editing")
			}
			b.WriteRune(r)
			escaped = false
			continue
		}
		switch r {
		case '␛':
			escaped = true
		case '⇥':
			b.WriteRune('\t')
		case '␍':
			b.WriteRune('\r')
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		return "", errors.New("Keep literal marker pairs together when editing")
	}
	return b.String(), nil
}
func (in sessionPresetInstructions) UpdateLiteral(msg tea.KeyMsg) (sessionPresetInstructions, tea.Cmd, error) {
	if in.readOnly {
		return in, nil, errors.New(in.reason)
	}
	if msg.Type == tea.KeyRunes {
		raw := string(msg.Runes)
		if reason := instructionEditorLimit(raw); reason != "" {
			return in, nil, errors.New("Paste refused: " + reason)
		}
		if strings.Count(in.raw, "\n")+strings.Count(raw, "\n") >= 10000 {
			return in, nil, errors.New("Paste refused: at most 10,000 logical lines in the editor")
		}
		msg.Runes = []rune(encodePresetInstructions(raw))
		msg.Paste = true
	}
	if msg.Type == tea.KeyEnter && strings.Count(in.raw, "\n") >= 9999 {
		return in, nil, errors.New("At most 10,000 logical lines in the editor")
	}
	before := in.Model.Value()
	line, col := in.Line(), in.LineInfo().StartColumn+in.LineInfo().ColumnOffset
	var cmd tea.Cmd
	repeats := 1
	for _, span := range literalPresetMarkerSpans(strings.Split(before, "\n")[line]) {
		if msg.Type == tea.KeyBackspace && col > span[0] && col <= span[1] {
			in.Model.SetCursor(span[1])
			repeats = 2
			break
		}
		if msg.Type == tea.KeyDelete && col >= span[0] && col < span[1] {
			in.Model.SetCursor(span[0])
			repeats = 2
			break
		}
	}
	for range repeats {
		in.Model, cmd = in.Model.Update(msg)
	}
	info := in.Model.LineInfo()
	position := info.StartColumn + info.ColumnOffset
	for _, span := range literalPresetMarkerSpans(strings.Split(in.Model.Value(), "\n")[in.Model.Line()]) {
		if position > span[0] && position < span[1] {
			if strings.Contains(msg.String(), "left") {
				in.Model.SetCursor(span[0])
			} else {
				in.Model.SetCursor(span[1])
			}
			break
		}
	}
	raw, err := decodePresetInstructions(in.Model.Value())
	if err != nil {
		in.Model.SetValue(before)
		for in.Model.Line() > line {
			in.Model.CursorUp()
		}
		in.Model.SetCursor(col)
		return in, nil, err
	}
	in.raw = raw
	return in, cmd, nil
}
func safePresetPreview(raw string) string {
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			if unicode.IsSpace(r) {
				return ' '
			}
			return '�'
		}
		return r
	}, raw)
	return strings.Join(strings.Fields(raw), " ")
}

var readSessionPresetClipboard = clipboard.ReadAll

type sessionPresetPasteMsg struct {
	gen  int
	text string
	err  error
}

func (m *Model) recordSessionPresetPaste(msg sessionPresetPasteMsg) tea.Cmd {
	p := &m.presets
	if m.mode != modeSettings || !p.open || !p.editing || p.busy || p.gen != msg.gen || p.instructions.readOnly {
		return nil
	}
	p.pasting = false
	if msg.err != nil {
		m.errBar.text = "Reading clipboard: " + msg.err.Error()
		return nil
	}
	var err error
	focused := p.instructions.Focused()
	p.instructions.Focus()
	p.instructions, _, err = p.instructions.UpdateLiteral(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(msg.text), Paste: true})
	if !focused {
		p.instructions.Blur()
	}
	if err != nil {
		m.errBar.text = err.Error()
	}
	return nil
}

func literalPresetMarkerSpans(line string) [][2]int {
	var spans [][2]int
	runes := []rune(line)
	for i := 0; i < len(runes)-1; i++ {
		if runes[i] == '␛' {
			spans = append(spans, [2]int{i, i + 2})
			i++
		}
	}
	return spans
}
