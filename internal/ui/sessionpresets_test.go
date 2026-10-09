package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSessionPresetsSettingsEntry(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !strings.Contains(m.viewSettings(), "session presets") {
		t.Fatal("Settings must expose the named instruction library")
	}
}

func TestSessionPresetsFormLoadsCatalog(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SaveSessionPreset("", store.SessionPreset{Name: "Coordinator", Instructions: "  literal instructions\n\n"}); err != nil {
		t.Fatal(err)
	}
	cmd := m.openForm()
	// A batch may contain the independent model catalog command.
	runPresetCommands(m, cmd)
	if !strings.Contains(m.viewForm(), "preset") {
		t.Fatal("new session must show catalog selection")
	}
}

func runPresetCommands(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			runPresetCommands(m, sub)
		}
		return
	}
	m.Update(msg)
}

func TestSessionPresetsSaveCancelRenameDelete(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m.presets.name.SetValue("  Role  ")
	m.presets.instructions.SetValue(" \n literal\n\n")
	_, cmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil || !m.presets.busy {
		t.Fatal("save must dispatch asynchronously")
	}
	if rows, _ := m.store.SessionPresets(); len(rows) != 0 {
		t.Fatal("Update saved synchronously")
	}
	_, again := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if again != nil {
		t.Fatal("double save dispatched")
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.presets.editing {
		t.Fatal("in-flight save was cancelled")
	}
	runPresetCommands(m, cmd)
	rows, err := m.store.SessionPresets()
	if err != nil || len(rows) != 1 || rows[0].Name != "Role" || rows[0].Instructions != " \n literal\n\n" {
		t.Fatalf("saved %v: %v", rows, err)
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.presets.name.SetValue("Renamed")
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	runPresetCommands(m, cmd)
	rows, _ = m.store.SessionPresets()
	if len(rows) != 1 || rows[0].Name != "Renamed" {
		t.Fatal(rows)
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.presets.instructions.SetValue("discard")
	_, cancelCmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEsc})
	runPresetCommands(m, cancelCmd)
	rows, _ = m.store.SessionPresets()
	if rows[0].Instructions == "discard" {
		t.Fatal("cancel saved")
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !m.presets.confirm {
		t.Fatal("deletion requires confirmation")
	}
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEnter})
	runPresetCommands(m, cmd)
	rows, _ = m.store.SessionPresets()
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestSessionPresetsLateLoadsCannotReplaceReopenedFormOrEditor(t *testing.T) {
	m := buildModel(t)
	p := store.SessionPreset{Name: "Before", Instructions: "before"}
	m.store.SaveSessionPreset("", p)
	m.openForm()
	old := m.loadSessionPresets(m.form.prompt.gen, true)()
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.store.SaveSessionPreset("Before", store.SessionPreset{Name: "After", Instructions: "after"})
	runPresetCommands(m, m.openForm())
	m.Update(old)
	if len(m.form.presets) != 1 || m.form.presets[0].Name != "After" {
		t.Fatal(m.form.presets)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.openSettings()
	late := m.openSessionPresets()
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m.presets.name.SetValue("unsaved")
	runPresetCommands(m, late)
	if m.presets.name.Value() != "unsaved" || !m.presets.editing {
		t.Fatal("late library read replaced editor")
	}
}

func TestSessionPresetsSelectionPreservesTaskAndSnapshot(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "A", Instructions: " \n a\n"})
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "B", Instructions: "b"})
	runPresetCommands(m, m.openForm())
	m.form.prompt.input.SetValue("  task  ")
	beforeTool, beforeChoice, beforeDir, beforeGroup, beforeWorktree := m.form.toolIndex, m.form.choice, m.form.dir.Value(), m.form.groupIndex, m.form.worktree
	m.form.focus = fieldPreset
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formInitialPrompt() != " \n a\n\n\ntask" {
		t.Fatalf("prompt %q", m.formInitialPrompt())
	}
	m.store.DeleteSessionPreset("A")
	if m.formInitialPrompt() != " \n a\n\n\ntask" {
		t.Fatal("external catalog edit changed snapshot")
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formInitialPrompt() != "b\n\ntask" {
		t.Fatal(m.formInitialPrompt())
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formInitialPrompt() != "task" {
		t.Fatal(m.formInitialPrompt())
	}
	if m.form.prompt.input.Value() != "  task  " || m.form.toolIndex != beforeTool || m.form.dir.Value() != beforeDir || m.form.groupIndex != beforeGroup || m.form.worktree != beforeWorktree || m.form.choice.model != beforeChoice.model {
		t.Fatal("preset selection changed composer or CLI choices")
	}
}

func TestSessionPresetsDuplicateRetainsEditorAndInput(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Taken", Instructions: "existing"})
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.name.SetValue("Taken")
	m.presets.instructions.SetValue("  my text\n")
	_, cmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	runPresetCommands(m, cmd)
	if !m.presets.editing || m.presets.busy || m.errBar.text == "" || m.presets.name.Value() != "Taken" || m.presets.instructions.Value() != "  my text\n" {
		t.Fatal("duplicate lost input or error")
	}
	m.presets.name.SetValue("Fresh")
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	runPresetCommands(m, cmd)
	if m.presets.editing || len(m.presets.rows) != 2 {
		t.Fatal("retry did not save")
	}
}

func TestSessionPresetsMouseActionsAndScrolling(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 40, 22
	for i := 0; i < 30; i++ {
		m.store.SaveSessionPreset("", store.SessionPreset{Name: fmt.Sprintf("Role %02d", i), Instructions: "text"})
	}
	m.openSettings()
	m.viewSettings()
	_, cmd := m.handleSettingsClick(m.cardLeft+4, m.cardTop+2+settingsFieldPresets)
	runPresetCommands(m, cmd)
	if !m.presets.open {
		t.Fatal("Settings row click did not open library")
	}
	for i := 0; i < 29; i++ {
		m.handleSessionPresetAction("j")
	}
	frame := m.viewSessionPresets()
	if !strings.Contains(frame, "Role 29") {
		t.Fatal("selected preset scrolled out of view")
	}
	for _, line := range strings.Split(frame, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("narrow frame exceeds width: %q", line)
		}
	}
	clickPresetAction(t, m, "edit")
	if !m.presets.editing || m.presets.previous != "Role 29" {
		t.Fatal("mouse edit failed")
	}
	m.presets.name.SetValue("Mouse rename")
	clickPresetAction(t, m, "save")
	if m.presets.editing {
		t.Fatal("mouse save failed")
	}
	if m.presets.rows[m.presets.cursor].Name != "Mouse rename" {
		t.Fatal("renamed preset selection moved to a different entry")
	}
	clickPresetAction(t, m, "delete")
	if !m.presets.confirm {
		t.Fatal("mouse delete skipped confirmation")
	}
	clickPresetAction(t, m, "cancel")
	if m.presets.confirm {
		t.Fatal("mouse confirmation cancel failed")
	}
	clickPresetAction(t, m, "delete")
	clickPresetAction(t, m, "confirm")
	if rows, _ := m.store.SessionPresets(); len(rows) != 29 {
		t.Fatal("mouse confirmation failed")
	}
	clickPresetAction(t, m, "new")
	clickPresetAction(t, m, "instructions")
	if m.presets.focus != 1 {
		t.Fatal("instruction mouse focus failed")
	}
	clickPresetAction(t, m, "cancel")
	clickPresetAction(t, m, "cancel")
	if m.presets.open {
		t.Fatal("mouse back failed")
	}
}
func clickPresetAction(t *testing.T, m *Model, action string) {
	t.Helper()
	m.viewSessionPresets()
	for i, hit := range m.presets.hits {
		if hit.action == action {
			_, cmd := m.handleSessionPresetsClick(m.cardLeft+4, m.cardTop+2+i)
			runPresetCommands(m, cmd)
			return
		}
	}
	t.Fatalf("no mouse action %s", action)
}

func TestSessionPresetsPickerMousePreservesImagesAndChoices(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Role", Instructions: "instruct"})
	runPresetCommands(m, m.openForm())
	m.form.focus = fieldPreset
	m.form.prompt.input.SetValue("task " + imageToken(1))
	m.form.prompt.attachments = []imageAttachment{{id: 1, path: tempImage(t, "image.png")}}
	before := m.form.prompt.message()
	m.form.choice.model = "chosen-model"
	m.form.choice.effort = 2
	m.form.choice.profile = 1
	m.viewForm()
	for i, hit := range m.form.hits {
		if hit.field == fieldPreset && hit.entry == 1 {
			m.handleFormClick(m.cardLeft+4, m.cardTop+2+i)
			break
		}
	}
	if m.form.presetIndex != 1 || m.form.prompt.message() != before || len(m.form.prompt.attachments) != 1 || m.form.choice.model != "chosen-model" || m.form.choice.effort != 2 || m.form.choice.profile != 1 {
		t.Fatal("mouse selection mutated task/CLI choices")
	}
	if m.formInitialPrompt() != "instruct\n\n"+before {
		t.Fatal(m.formInitialPrompt())
	}
}

func TestSessionPresetsLoadFailureIsVisibleAndNoPresetStillLaunchable(t *testing.T) {
	m := buildModel(t)
	cmd := m.openForm()
	m.store.Close()
	runPresetCommands(m, cmd)
	if m.form.presetsLoading || m.form.presetsError == "" || !strings.Contains(m.viewForm(), "Reading presets") {
		t.Fatal("catalog error hidden")
	}
	m.form.prompt.input.SetValue("task")
	if m.formInitialPrompt() != "task" {
		t.Fatal("no preset unavailable during read failure")
	}
}

func TestSessionPresetsSmallTerminalKeepsActionsVisible(t *testing.T) {
	for _, size := range [][2]int{{28, 18}, {40, 18}, {40, 22}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := buildModel(t)
			m.width, m.height = size[0], size[1]
			for i := 0; i < 30; i++ {
				m.store.SaveSessionPreset("", store.SessionPreset{Name: fmt.Sprintf("Role %02d", i), Instructions: "text"})
			}
			m.openSettings()
			runPresetCommands(m, m.openSessionPresets())
			m.presets.cursor = 15
			frame := m.viewSessionPresets()
			if len(strings.Split(frame, "\n")) > m.height {
				t.Fatal("library actions overflow terminal")
			}
			for _, line := range strings.Split(frame, "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Fatal("card exceeds terminal width")
				}
			}
			m.handleSessionPresetAction("e")
			frame = m.viewSessionPresets()
			if len(strings.Split(frame, "\n")) > m.height || !strings.Contains(frame, "Save") || !strings.Contains(frame, "Cancel") {
				t.Fatal("editor actions overflow terminal")
			}
		})
	}
}

func TestSessionPresetsLateLibraryReadCannotReplaceReopenedLibrary(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	old := m.openSessionPresets()()
	m.handleSessionPresetAction("esc")
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "New", Instructions: "text"})
	runPresetCommands(m, m.openSessionPresets())
	m.Update(old)
	if len(m.presets.rows) != 1 {
		t.Fatal("closed library response replaced latest catalog")
	}
}

func TestSessionPresetsHelpNamesControls(t *testing.T) {
	sections := helpSections(keybind.DefaultSession(), keybind.DefaultList(), true)
	found := false
	for _, section := range sections {
		if section.title == "session presets" {
			found = true
			if !strings.Contains(fmt.Sprint(section.rows), "ctrl+s") {
				t.Fatal("help lacks save binding")
			}
		}
	}
	if !found {
		t.Fatal("? help lacks preset controls")
	}
}

func TestSessionPresetsEmptyLibraryMouseNew(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	if !strings.Contains(m.viewSessionPresets(), "No presets") {
		t.Fatal("missing empty state")
	}
	clickPresetAction(t, m, "new")
	if !m.presets.editing {
		t.Fatal("empty catalog New inaccessible")
	}
}

func TestSessionPresetsSubmitUsesDisplayedInstructions(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Role", Instructions: " \n instruction\n\n"})
	runPresetCommands(m, m.openForm())
	pickFormTool(t, m, "ready-tool")
	m.form.presetIndex = 1
	m.form.prompt.input.SetValue("  task  ")
	m.form.name.SetValue("preset-launch")
	m.form.dir.SetValue(t.TempDir())
	m.store.SaveSessionPreset("Role", store.SessionPreset{Name: "Role", Instructions: "external replacement"})
	_, cmd := m.submitForm()
	if m.mode != modeList {
		t.Fatalf("submit failed: %s", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	rows, err := m.store.ListSessions(true)
	if err != nil || len(rows) != 1 {
		t.Fatalf("sessions %v %v", rows, err)
	}
	if !strings.HasSuffix(rows[0].LaunchPrompt, " \n instruction\n\n\n\ntask") {
		t.Fatalf("launch prompt lost snapshot or whitespace: %q", rows[0].LaunchPrompt)
	}
}

func TestSessionPresetsDashInstructionsLaunchWithOrdinaryPrefix(t *testing.T) {
	for _, tool := range []string{"ready-tool", "send-tool"} {
		t.Run(tool, func(t *testing.T) {
			m := buildModel(t)
			m.store.SaveSessionPreset("", store.SessionPreset{Name: "Bullets", Instructions: "- literal bullet\n"})
			runPresetCommands(m, m.openForm())
			pickFormTool(t, m, tool)
			m.form.presetIndex = 1
			m.form.name.SetValue("dash-preset")
			m.form.dir.SetValue(t.TempDir())
			m.submitForm()
			if m.mode != modeList {
				t.Fatalf("literal instruction refused: %s", m.errBar.text)
			}
			rows, err := m.store.ListSessions(true)
			if err != nil || len(rows) != 1 {
				t.Fatalf("sessions %v %v", rows, err)
			}
			prompt := rows[0].LaunchPrompt
			if tool == "send-tool" {
				if len(rows[0].PendingInputs) == 0 {
					t.Fatal("no pending input")
				}
				prompt = rows[0].PendingInputs[0]
			}
			if strings.HasPrefix(prompt, "-") || !strings.Contains(prompt, launch.RenameAvailableNote) || !strings.HasSuffix(prompt, "- literal bullet\n") {
				t.Fatalf("not prefixed or literal: %q", prompt)
			}
		})
	}
	m := buildModel(t)
	m.openForm()
	m.form.prompt.input.SetValue("- legacy task")
	m.form.dir.SetValue(t.TempDir())
	m.submitForm()
	if m.mode != modeForm || !strings.Contains(m.errBar.text, "prompt cannot start") {
		t.Fatal("legacy flag guard changed")
	}
}

func TestSessionPresetsEditorPreservesLiteralWhitespace(t *testing.T) {
	for _, instructions := range []string{"\t literal\r\nend\t", strings.Repeat("line\n", 120), strings.Repeat("\n", 10001) + "last"} {
		t.Run(fmt.Sprint(len(instructions)), func(t *testing.T) {
			m := buildModel(t)
			m.store.SaveSessionPreset("", store.SessionPreset{Name: "Literal", Instructions: instructions})
			m.openSettings()
			runPresetCommands(m, m.openSessionPresets())
			m.handleSessionPresetAction("e")
			if m.presets.instructions.Value() != instructions {
				t.Fatal("opening editor silently changed literal instruction bytes")
			}
			m.presets.name.SetValue("Renamed")
			_, cmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
			runPresetCommands(m, cmd)
			rows, err := m.store.SessionPresets()
			if err != nil || len(rows) != 1 || rows[0].Instructions != instructions {
				t.Fatal("rename changed literal instructions")
			}
		})
	}
}

func TestSessionPresetsNoPresetLaunchDuringLoad(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	if !m.form.presetsLoading {
		t.Fatal("catalog should still be loading")
	}
	m.form.prompt.input.SetValue("task")
	m.form.name.SetValue("without-preset")
	m.form.dir.SetValue(t.TempDir())
	m.submitForm()
	if m.mode != modeList {
		t.Fatalf("no-preset launch blocked: %s", m.errBar.text)
	}
	rows, err := m.store.ListSessions(true)
	if err != nil || len(rows) != 1 || !strings.HasSuffix(rows[0].LaunchPrompt, "task") {
		t.Fatalf("no-preset launch changed: %v %v", rows, err)
	}
}

func TestSessionPresetsSettingsRowAccessibleAtShortHeight(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 40, 22
	m.openSettings()
	for i := 0; i < settingsFieldPresets; i++ {
		m.handleMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	}
	frame := m.viewSettings()
	if m.settings.field != settingsFieldPresets || len(strings.Split(frame, "\n")) > m.height {
		t.Fatal("Settings cannot scroll to preset action with mouse")
	}
	for y, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "session presets") {
			_, cmd := m.handleSettingsClick(m.cardLeft+4, y)
			runPresetCommands(m, cmd)
			break
		}
	}
	if !m.presets.open {
		t.Fatal("scrolled Settings preset row click failed")
	}
}

func TestSessionPresetsEditorReadOnlyPayloadStillRenames(t *testing.T) {
	for _, raw := range []string{"before\x1bafter", strings.Repeat("\n", 10001) + "last"} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			m := buildModel(t)
			m.store.SaveSessionPreset("", store.SessionPreset{Name: "Raw", Instructions: raw})
			m.openSettings()
			runPresetCommands(m, m.openSessionPresets())
			m.handleSessionPresetAction("e")
			if !strings.Contains(m.viewSessionPresets(), "Read-only") {
				t.Fatal("unsupported payload editor not explicit")
			}
			m.presets.focus = 1
			m.handleSessionPresetsKey(runeKey("typed"))
			if m.presets.instructions.Value() != raw {
				t.Fatal("read-only payload changed")
			}
			m.presets.name.SetValue("Renamed")
			_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
			runPresetCommands(m, cmd)
			rows, _ := m.store.SessionPresets()
			if len(rows) != 1 || rows[0].Instructions != raw {
				t.Fatal("rename changed read-only bytes")
			}
		})
	}
}

func TestSessionPresetsEditorLiteralPasteAndMarkers(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.name.SetValue("Literal")
	m.presets.focus = 1
	raw := "\t\r\n⇥␍␛ raw\t"
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(raw), Paste: true})
	if m.presets.instructions.Value() != raw {
		t.Fatalf("paste changed bytes %q", m.presets.instructions.Value())
	}
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	runPresetCommands(m, cmd)
	rows, _ := m.store.SessionPresets()
	if len(rows) != 1 || rows[0].Instructions != raw {
		t.Fatal("save changed pasted bytes")
	}
	m.handleSessionPresetAction("e")
	_, cmd = m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	runPresetCommands(m, cmd)
	rows, _ = m.store.SessionPresets()
	if rows[0].Instructions != raw {
		t.Fatal("no-op save changed marker bytes")
	}
}

func TestSessionPresetsEditorUnsupportedPasteRetainsBuffer(t *testing.T) {
	for _, raw := range []string{"\x1bunsafe", strings.Repeat("\n", 10001)} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			m := buildModel(t)
			m.openSettings()
			runPresetCommands(m, m.openSessionPresets())
			m.handleSessionPresetAction("n")
			m.presets.instructions.SetValue("before")
			m.presets.focus = 1
			m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(raw), Paste: true})
			if m.errBar.text == "" || m.presets.instructions.Value() != "before" {
				t.Fatal("unsupported paste changed buffer or was silently accepted")
			}
		})
	}
}

func TestSessionPresetsEditorClipboardIsAsyncAndGenerationSafe(t *testing.T) {
	oldReader := readSessionPresetClipboard
	t.Cleanup(func() { readSessionPresetClipboard = oldReader })
	reads := 0
	readSessionPresetClipboard = func() (string, error) { reads++; return "\t pasted\r\n⇥␍␛", nil }
	m := buildModel(t)
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.focus = 1
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlV))
	if cmd == nil || reads != 0 {
		t.Fatal("clipboard read must be asynchronous")
	}
	msg := cmd()
	m.Update(msg)
	if reads != 1 || m.presets.instructions.Value() != "\t pasted\r\n⇥␍␛" {
		t.Fatal("clipboard literal bytes changed")
	}
	_, late := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlV))
	lateMsg := late()
	_, cancel := m.handleSessionPresetsKey(namedKey(tea.KeyEsc))
	runPresetCommands(m, cancel)
	m.handleSessionPresetAction("n")
	m.presets.instructions.SetValue("new editor")
	m.Update(lateMsg)
	if m.presets.instructions.Value() != "new editor" {
		t.Fatal("late clipboard changed reopened editor")
	}
}

func TestSessionPresetsEditorRejectsSaveWhileClipboardPending(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.name.SetValue("Pending")
	m.presets.instructions.SetValue("before")
	m.presets.focus = 1
	m.handleSessionPresetsKey(namedKey(tea.KeyCtrlV))
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	if cmd != nil || !strings.Contains(m.errBar.text, "clipboard") {
		t.Fatal("save raced pending clipboard input")
	}
}

func TestSessionPresetsUnsafeControlPreviewsCannotEmitTerminalCommands(t *testing.T) {
	m := buildModel(t)
	raw := "literal\x1b]52;c;danger\a end"
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Controls", Instructions: raw})
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("e")
	frame := m.viewSessionPresets()
	if strings.Contains(frame, "\x1b]52") || strings.Contains(frame, "\a end") || !strings.Contains(frame, "Read-only") {
		t.Fatal("read-only preview emits instruction control bytes")
	}
	m.handleSessionPresetAction("esc")
	m.presets.open = false
	runPresetCommands(m, m.openForm())
	m.form.presetIndex = 1
	if frame = m.viewForm(); strings.Contains(frame, "\x1b]52") || strings.Contains(frame, "\a end") {
		t.Fatal("new-session preview emits instruction control bytes")
	}
	if m.formInitialPrompt() != raw {
		t.Fatal("safe display changed launch bytes")
	}
}

func TestSessionPresetsEditorReplacementCharacterPreservedReadOnly(t *testing.T) {
	m := buildModel(t)
	raw := "literal � text"
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Unicode", Instructions: raw})
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("e")
	if !strings.Contains(m.viewSessionPresets(), "Read-only") {
		t.Fatal("textarea cannot edit replacement characters without stripping them")
	}
	m.presets.name.SetValue("Renamed")
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	runPresetCommands(m, cmd)
	rows, _ := m.store.SessionPresets()
	if len(rows) != 1 || rows[0].Instructions != raw {
		t.Fatal("valid replacement rune lost")
	}
}

func TestSessionPresetsEditorLiteralMarkersDeleteAsCharacters(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	runPresetCommands(m, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.focus = 1
	m.presets.instructions.SetValue("text⇥␍␛")
	for _, want := range []string{"text⇥␍", "text⇥", "text"} {
		m.handleSessionPresetsKey(namedKey(tea.KeyBackspace))
		if got := m.presets.instructions.Value(); got != want {
			t.Fatalf("backspace=%q want %q", got, want)
		}
	}
}
