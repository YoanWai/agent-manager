package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// notifyCommandRow is the Settings row holding the shell command every
// notification also runs. A command is free text, so the row is typed
// rather than stepped; an empty line turns it off.
type notifyCommandRow struct {
	value  string
	typing bool
	input  textinput.Model
	// loaded is false when the stored command could not be read; saving
	// then would overwrite it with an empty line nobody typed.
	loaded bool
}

// notifyCommandLabelRunes keeps a long command, a curl with a URL and
// headers, from pushing the Settings card wider than the other rows.
const notifyCommandLabelRunes = 40

func (r notifyCommandRow) label() string {
	if r.value == "" {
		return "off"
	}
	if runes := []rune(r.value); len(runes) > notifyCommandLabelRunes {
		return string(runes[:notifyCommandLabelRunes-1]) + "…"
	}
	return r.value
}

func storedNotifyCommand(st settingReader) (string, error) {
	command, err := st.Setting(notifyCommandSetting)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(command), nil
}

// loadNotifyCommandRow reads the row for Settings. A store error is
// surfaced, and the row keeps the stored command out of the save.
func (m *Model) loadNotifyCommandRow() notifyCommandRow {
	command, err := storedNotifyCommand(m.store)
	if err != nil {
		m.errBar.text = "reading notify command: " + err.Error()
		return notifyCommandRow{}
	}
	return notifyCommandRow{value: command, loaded: true}
}

func (m *Model) openNotifyCommandTyping() {
	input := textField(`a shell command, such as curl -d "$AM_BODY" ntfy.sh/mytopic`, 1000)
	input.Prompt = ""
	input.SetValue(m.settings.notifyCommand.value)
	input.Focus()
	m.settings.notifyCommand.input = input
	m.settings.notifyCommand.typing = true
}

func (m *Model) handleNotifyCommandTypingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	row := &m.settings.notifyCommand
	switch msg.String() {
	case "enter":
		row.value = strings.TrimSpace(row.input.Value())
		row.typing = false
		row.loaded = true
		return m, nil
	case "esc":
		row.typing = false
		return m, nil
	}
	var cmd tea.Cmd
	row.input, cmd = row.input.Update(msg)
	return m, cmd
}
