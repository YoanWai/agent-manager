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

func storedNotifyCommand(st settingReader) string {
	command, err := st.Setting(notifyCommandSetting)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(command)
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
		return m, nil
	case "esc":
		row.typing = false
		return m, nil
	}
	var cmd tea.Cmd
	row.input, cmd = row.input.Update(msg)
	return m, cmd
}
