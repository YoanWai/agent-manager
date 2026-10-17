package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionPresetsSettingsEntry(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !strings.Contains(m.viewSettings(), "saved prompts") {
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
	m.runBatch(t, cmd)
	if !strings.Contains(m.viewForm(), "saved") {
		t.Fatal("new session must show catalog selection")
	}
}

func TestSessionPresetsSaveCancelRenameDelete(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
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
	m.runBatch(t, cmd)
	rows, err := m.store.SessionPresets()
	if err != nil || len(rows) != 1 || rows[0].Name != "Role" || rows[0].Instructions != " \n literal\n\n" {
		t.Fatalf("saved %v: %v", rows, err)
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.presets.name.SetValue("Renamed")
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.runBatch(t, cmd)
	rows, _ = m.store.SessionPresets()
	if len(rows) != 1 || rows[0].Name != "Renamed" {
		t.Fatal(rows)
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.presets.instructions.SetValue("discard")
	_, cancelCmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.runBatch(t, cancelCmd)
	rows, _ = m.store.SessionPresets()
	if rows[0].Instructions == "discard" {
		t.Fatal("cancel saved")
	}
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !m.presets.confirm {
		t.Fatal("deletion requires confirmation")
	}
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runBatch(t, cmd)
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
	m.runBatch(t, m.openForm())
	m.Update(old)
	if len(m.form.presets) != 1 || m.form.presets[0].Name != "After" {
		t.Fatal(m.form.presets)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.openSettings()
	late := m.openSessionPresets()
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m.presets.name.SetValue("unsaved")
	m.runBatch(t, late)
	if m.presets.name.Value() != "unsaved" || !m.presets.editing {
		t.Fatal("late library read replaced editor")
	}
}

func TestSessionPresetsSelectionPreservesTaskAndSnapshot(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "A", Instructions: " \n a\n"})
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "B", Instructions: "b"})
	m.runBatch(t, m.openForm())
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
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.name.SetValue("Taken")
	m.presets.instructions.SetValue("  my text\n")
	_, cmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.runBatch(t, cmd)
	if !m.presets.editing || m.presets.busy || m.errBar.text == "" || m.presets.name.Value() != "Taken" || m.presets.instructions.Value() != "  my text\n" {
		t.Fatal("duplicate lost input or error")
	}
	m.presets.name.SetValue("Fresh")
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.runBatch(t, cmd)
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
	m.settings.field = settingsFieldPresets
	m.viewSettings()
	_, cmd := m.handleSettingsClick(m.cardLeft+4, m.cardTop+2+m.settings.presetsRow)
	m.runBatch(t, cmd)
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
	m.handleSessionPresetAction("e")
	if !m.presets.editing || m.presets.previous != "Role 29" {
		t.Fatal("edit failed")
	}
	m.presets.name.SetValue("Renamed")
	_, cmd = m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.runBatch(t, cmd)
	if m.presets.editing {
		t.Fatal("save failed")
	}
}

func TestSessionPresetsPickerMousePreservesImagesAndChoices(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Role", Instructions: "instruct"})
	m.runBatch(t, m.openForm())
	m.form.focus = fieldPreset
	m.openSavedList(savedInForm)
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
	m.runBatch(t, cmd)
	if m.form.presetsLoading || m.form.presetsError == "" || !strings.Contains(m.viewForm(), "Reading saved prompts") {
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
			m.runBatch(t, m.openSessionPresets())
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
			if len(strings.Split(frame, "\n")) > m.height || !strings.Contains(frame, "ctrl+s") {
				t.Fatal("editor overflows terminal")
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
	m.runBatch(t, m.openSessionPresets())
	m.Update(old)
	if len(m.presets.rows) != 1 {
		t.Fatal("closed library response replaced latest catalog")
	}
}

func TestSessionPresetsHelpNamesControls(t *testing.T) {
	sections := glyphsFor(glyphModeBasic).helpSections(keybind.DefaultSession(), keybind.DefaultList(), true)
	found := false
	for _, section := range sections {
		if section.title == "saved prompts" {
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
	m.runBatch(t, m.openSessionPresets())
	if !strings.Contains(m.viewSessionPresets(), "No saved prompts") {
		t.Fatal("missing empty state")
	}
	m.handleSessionPresetAction("n")
	if !m.presets.editing {
		t.Fatal("empty catalog New inaccessible")
	}
}

func TestSessionPresetsSubmitUsesDisplayedInstructions(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Role", Instructions: " \n instruction\n\n"})
	m.runBatch(t, m.openForm())
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
			m.runBatch(t, m.openForm())
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
	if m.mode != modeList {
		t.Fatalf("a leading dash is text after the rename note: %s", m.errBar.text)
	}
}

func TestSessionPresetsEditorPreservesLiteralWhitespace(t *testing.T) {
	for _, instructions := range []string{"literal\nend", strings.Repeat("line\n", 120)} {
		t.Run(fmt.Sprint(len(instructions)), func(t *testing.T) {
			m := buildModel(t)
			m.store.SaveSessionPreset("", store.SessionPreset{Name: "Literal", Instructions: instructions})
			m.openSettings()
			m.runBatch(t, m.openSessionPresets())
			m.handleSessionPresetAction("e")
			if m.presets.instructions.Value() != instructions {
				t.Fatal("opening editor silently changed literal instruction bytes")
			}
			m.presets.name.SetValue("Renamed")
			_, cmd := m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
			m.runBatch(t, cmd)
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
		if strings.Contains(line, "saved prompts") {
			_, cmd := m.handleSettingsClick(m.cardLeft+4, y)
			m.runBatch(t, cmd)
			break
		}
	}
	if !m.presets.open {
		t.Fatal("scrolled Settings preset row click failed")
	}
}

func clickSavedPromptsText(t *testing.T, m *Model, text string) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.viewSettings()), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			_, cmd := m.handleSettingsClick(ansi.StringWidth(line[:i]), y)
			m.runBatch(t, cmd)
			return
		}
	}
	t.Fatalf("%q is not on screen", text)
}

func TestSavedPromptsPanelWorksByMouse(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 100, 30
	for _, name := range []string{"Alpha", "Beta"} {
		m.store.SaveSessionPreset("", store.SessionPreset{Name: name, Instructions: "text"})
	}
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())

	clickSavedPromptsText(t, m, "Beta")
	if m.presets.cursor != 1 {
		t.Fatalf("row click selected %d, want Beta", m.presets.cursor)
	}
	clickSavedPromptsText(t, m, "[ Edit ]")
	if !m.presets.editing || m.presets.previous != "Beta" {
		t.Fatal("Edit button did not open Beta")
	}
	clickSavedPromptsText(t, m, "[ Cancel ]")
	if m.presets.editing {
		t.Fatal("Cancel button left the editor open")
	}
	clickSavedPromptsText(t, m, "[ New ]")
	clickSavedPromptsText(t, m, "instructions")
	if m.presets.focus != 1 {
		t.Fatal("clicking the text field did not focus it")
	}
	m.presets.name.SetValue("Gamma")
	m.presets.instructions.SetValue("more text")
	clickSavedPromptsText(t, m, "[ Save ]")
	if m.presets.editing || len(m.presets.rows) != 3 {
		t.Fatalf("Save button did not add Gamma: %+v", m.presets.rows)
	}
	clickSavedPromptsText(t, m, "Alpha")
	clickSavedPromptsText(t, m, "[ Delete ]")
	clickSavedPromptsText(t, m, "[ Delete ]")
	if len(m.presets.rows) != 2 || m.presets.rows[0].Name == "Alpha" {
		t.Fatalf("Delete buttons did not remove Alpha: %+v", m.presets.rows)
	}
	clickSavedPromptsText(t, m, "[ Back ]")
	if m.presets.open {
		t.Fatal("Back button left the panel open")
	}
}
