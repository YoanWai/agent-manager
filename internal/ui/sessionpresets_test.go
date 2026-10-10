package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"unicode"
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
	m.runBatch(t, cmd)
	if !strings.Contains(m.viewForm(), "preset") {
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
	m.viewSettings()
	_, cmd := m.handleSettingsClick(m.cardLeft+4, m.cardTop+2+settingsFieldPresets)
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
			m.runBatch(t, cmd)
			return
		}
	}
	t.Fatalf("no mouse action %s", action)
}

func TestSessionPresetsPickerMousePreservesImagesAndChoices(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Role", Instructions: "instruct"})
	m.runBatch(t, m.openForm())
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
	m.runBatch(t, cmd)
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
	m.runBatch(t, m.openSessionPresets())
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
	m.runBatch(t, m.openSessionPresets())
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
		if strings.Contains(line, "session presets") {
			_, cmd := m.handleSettingsClick(m.cardLeft+4, y)
			m.runBatch(t, cmd)
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
			m.runBatch(t, m.openSessionPresets())
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
			m.runBatch(t, cmd)
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
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.name.SetValue("Literal")
	m.presets.focus = 1
	raw := "\t\r\n⇥␍␛ raw\t"
	m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(raw), Paste: true})
	if m.presets.instructions.Value() != raw {
		t.Fatalf("paste changed bytes %q", m.presets.instructions.Value())
	}
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
	rows, _ := m.store.SessionPresets()
	if len(rows) != 1 || rows[0].Instructions != raw {
		t.Fatal("save changed pasted bytes")
	}
	m.handleSessionPresetAction("e")
	_, cmd = m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
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
			m.runBatch(t, m.openSessionPresets())
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
	m.runBatch(t, m.openSessionPresets())
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
	m.runBatch(t, cancel)
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
	m.runBatch(t, m.openSessionPresets())
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
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("e")
	frame := m.viewSessionPresets()
	if strings.Contains(frame, "\x1b]52") || strings.Contains(frame, "\a end") || !strings.Contains(frame, "Read-only") {
		t.Fatal("read-only preview emits instruction control bytes")
	}
	m.handleSessionPresetAction("esc")
	m.presets.open = false
	m.runBatch(t, m.openForm())
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
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("e")
	if !strings.Contains(m.viewSessionPresets(), "Read-only") {
		t.Fatal("textarea cannot edit replacement characters without stripping them")
	}
	m.presets.name.SetValue("Renamed")
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
	rows, _ := m.store.SessionPresets()
	if len(rows) != 1 || rows[0].Instructions != raw {
		t.Fatal("valid replacement rune lost")
	}
}

func TestSessionPresetsEditorLiteralMarkersDeleteAsCharacters(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
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

func TestSessionPresetsOversizedInsertionRejectedWithoutTextLoss(t *testing.T) {
	cases := []struct {
		name, before, input string
		accept              bool
	}{
		{"ascii_overflow", "before", strings.Repeat("a", 64*1024), false},
		{"ascii_boundary", "before", strings.Repeat("a", 64*1024-len("before")), true},
		{"ascii_boundary_plus_one", "before", strings.Repeat("a", 64*1024-len("before")+1), false},
		{"multibyte_overflow", "before", strings.Repeat("界", (64*1024-len("before"))/3+1), false},
		{"multibyte_boundary", "before", strings.Repeat("界", (64*1024-len("before"))/3) + "a", true},
		{"marker_overflow", "before", strings.Repeat("⇥", (64*1024-len("before"))/3+1), false},
		{"marker_boundary", "before", strings.Repeat("⇥", (64*1024-len("before"))/3) + "a", true},
		{"tab_boundary", "before", strings.Repeat("\t", 64*1024-len("before")), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			m.openSettings()
			m.runBatch(t, m.openSessionPresets())
			m.handleSessionPresetAction("n")
			m.presets.name.SetValue("Bounded")
			m.presets.instructions.SetValue(tc.before)
			m.presets.focus = 1
			beforeDisplay := m.presets.instructions.Model.Value()
			m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.input), Paste: true})
			want := tc.before
			if tc.accept {
				want += tc.input
				if m.errBar.text != "" {
					t.Fatalf("valid insertion refused: %s", m.errBar.text)
				}
			} else {
				if m.errBar.text == "" || m.presets.instructions.Model.Value() != beforeDisplay {
					t.Fatal("oversize input silently truncated or changed display buffer")
				}
			}
			if got := m.presets.instructions.Value(); got != want {
				t.Fatalf("instruction bytes changed: got length %d want %d", len(got), len(want))
			}
			_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
			m.runBatch(t, cmd)
			rows, err := m.store.SessionPresets()
			if err != nil || len(rows) != 1 || rows[0].Instructions != want {
				t.Fatal("saved a changed/truncated instruction payload")
			}
		})
	}
}

func TestSessionPresetsOversizedNewlineRejected(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	before := strings.Repeat("a", 64*1024)
	m.presets.instructions.SetValue(before)
	m.presets.focus = 1
	m.handleSessionPresetsKey(namedKey(tea.KeyEnter))
	if m.errBar.text == "" || m.presets.instructions.Value() != before || m.presets.instructions.Model.Value() != before {
		t.Fatal("newline exceeded byte boundary")
	}
}

func TestSessionPresetsCancelPendingClipboardSuccessorCanPasteAndSave(t *testing.T) {
	oldReader := readSessionPresetClipboard
	t.Cleanup(func() { readSessionPresetClipboard = oldReader })
	readSessionPresetClipboard = func() (string, error) { return " stale", nil }
	m := buildModel(t)
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.focus = 1
	_, late := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlV))
	lateMsg := late()
	_, cancel := m.handleSessionPresetsKey(namedKey(tea.KeyEsc))
	m.runBatch(t, cancel)
	m.handleSessionPresetAction("n")
	m.presets.name.SetValue("Successor")
	m.presets.instructions.SetValue("new editor")
	m.presets.focus = 1
	m.Update(lateMsg)
	if m.presets.instructions.Value() != "new editor" {
		t.Fatal("stale clipboard result entered successor")
	}
	readSessionPresetClipboard = func() (string, error) { return " new paste", nil }
	_, paste := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlV))
	if paste == nil {
		t.Fatal("successor cannot paste after cancelled read")
	}
	m.runBatch(t, paste)
	_, save := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	if save == nil {
		t.Fatalf("successor cannot save: %s", m.errBar.text)
	}
	m.runBatch(t, save)
	rows, err := m.store.SessionPresets()
	if err != nil || len(rows) != 1 || rows[0].Instructions != "new editor new paste" {
		t.Fatal("successor clipboard/save did not complete")
	}
}

func TestSessionPresetsCommittedMutationRefreshFailureRecovers(t *testing.T) {
	m := buildModel(t)
	m.store.SaveSessionPreset("", store.SessionPreset{Name: "Before", Instructions: "literal"})
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("e")
	m.presets.name.SetValue("After")
	_, save := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	msg := save().(sessionPresetsLoadedMsg)
	// The real write succeeded. Simulate only the catalog-read response failure.
	msg.rows = nil
	msg.err = errors.New("catalog read unavailable")
	m.Update(msg)
	rows, err := m.store.SessionPresets()
	if err != nil || len(rows) != 1 || rows[0].Name != "After" {
		t.Fatal("real rename did not commit")
	}
	if m.presets.editing || m.presets.busy || m.errBar.text == "" || !strings.Contains(m.viewSessionPresets(), "[ Refresh ]") {
		t.Fatal("committed rename remained in write-error editor with no recovery")
	}
	clickPresetAction(t, m, "refresh")
	if len(m.presets.rows) != 1 || m.presets.rows[0].Name != "After" || m.errBar.text != "" {
		t.Fatal("refresh did not recover committed rename")
	}
	m.handleSessionPresetAction("d")
	_, del := m.handleSessionPresetsKey(namedKey(tea.KeyEnter))
	msg = del().(sessionPresetsLoadedMsg)
	msg.rows = nil
	msg.err = errors.New("catalog read unavailable")
	m.Update(msg)
	if m.presets.confirm || !strings.Contains(m.viewSessionPresets(), "[ Refresh ]") {
		t.Fatal("committed deletion remained in confirmation")
	}
	clickPresetAction(t, m, "refresh")
	if len(m.presets.rows) != 0 {
		t.Fatal("refresh retained deleted entry")
	}
}

func TestSessionPresetsLargeStoredEscapesOpenAndSaveWithoutTruncation(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"tabs", strings.Repeat("\t", 64*1024-4) + "abcd"},
		{"returns", strings.Repeat("\r", 64*1024-4) + "abcd"},
		{"markers", strings.Repeat("⇥␍␛", 7281) + "abcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			if err := m.store.SaveSessionPreset("", store.SessionPreset{Name: "Existing", Instructions: tc.raw}); err != nil {
				t.Fatal(err)
			}
			m.openSettings()
			m.runBatch(t, m.openSessionPresets())
			m.handleSessionPresetAction("e")
			decoded, err := decodePresetInstructions(m.presets.instructions.Model.Value())
			if err != nil || decoded != tc.raw {
				t.Fatal("opening stored literal text truncated the editable buffer")
			}
			_, noop := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
			m.runBatch(t, noop)
			rows, err := m.store.SessionPresets()
			if err != nil || len(rows) != 1 || rows[0].Instructions != tc.raw {
				t.Fatal("no-op save changed existing escape bytes")
			}
			m.handleSessionPresetAction("e")
			m.presets.name.SetValue("Renamed")
			_, rename := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
			m.runBatch(t, rename)
			rows, err = m.store.SessionPresets()
			if err != nil || len(rows) != 1 || rows[0].Name != "Renamed" || rows[0].Instructions != tc.raw {
				t.Fatal("name-only save changed escape bytes")
			}
			m.handleSessionPresetAction("e")
			m.presets.focus = 1
			m.handleSessionPresetsKey(namedKey(tea.KeyBackspace))
			if m.presets.instructions.Value() != strings.TrimSuffix(tc.raw, "d") {
				t.Fatal("first edit decoded a truncated stored buffer")
			}
		})
	}
}

func TestSessionPresetsSelectedFormFitsAndVisibleHitsAlign(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 22}, {40, 18}, {28, 18}} {
		for _, task := range []string{"", "one\ntwo\nthree\nfour"} {
			for _, name := range []string{"Role", strings.Repeat("界", 60)} {
				m := &Model{width: size[0], height: size[1], cfg: config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}}
				m.form = form{name: textField("name", 60), dir: textField("dir", 400), prompt: promptField(), toolNames: []string{"claude"}, groups: []groupOption{{}}, focus: fieldPreset}
				m.syncFormFieldWidths()
				m.form.prompt.input.SetValue(task)
				before := m.viewForm()
				m.form.presets = []store.SessionPreset{{Name: name, Instructions: "instruction"}, {Name: "B", Instructions: "b"}, {Name: "C", Instructions: "c"}}
				m.form.presetIndex = 1
				frame := m.viewForm()
				// Preserve inherited tiny/multiline limits; the optional details must fit whenever the base form fits.
				if len(strings.Split(before, "\n")) <= m.height && len(strings.Split(frame, "\n")) > m.height {
					t.Fatalf("selected form overflows %v: before %d after %d\n%s", size, len(strings.Split(before, "\n")), len(strings.Split(frame, "\n")), ansi.Strip(frame))
				}
				if len(strings.Split(frame, "\n")) > m.height {
					continue
				}
				for y, line := range strings.Split(frame, "\n") {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("form exceeds width %v", size)
					}
					if strings.Contains(ansi.Strip(line), "prompt") {
						m.handleFormClick(m.cardLeft+4, y)
						if m.form.focus != fieldPrompt {
							t.Fatalf("visible prompt hit selects %d at %v", m.form.focus, size)
						}
					}
				}
			}
		}
	}
}

func TestSessionPresetsAcceptedNameSaveIsLiteralUntilEdited(t *testing.T) {
	m := buildModel(t)
	for _, preset := range []store.SessionPreset{{Name: "Role", Instructions: "other"}, {Name: "Role�", Instructions: "original"}} {
		if err := m.store.SaveSessionPreset("", preset); err != nil {
			t.Fatal(err)
		}
	}
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.presets.cursor = 1
	m.handleSessionPresetAction("e")
	if m.presets.name.Value() != "Role�" {
		t.Fatalf("open changed accepted name to %q", m.presets.name.Value())
	}
	// Cursor movement and an instructions-only edit must not rename into the existing Role row.
	m.handleSessionPresetsKey(namedKey(tea.KeyLeft))
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
	if m.presets.editing || m.errBar.text != "" {
		t.Fatalf("no-op save failed: %s", m.errBar.text)
	}
	m.handleSessionPresetAction("e")
	m.presets.instructions.SetValue("changed")
	_, cmd = m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
	rows, err := m.store.SessionPresets()
	if err != nil || len(rows) != 2 || rows[1].Name != "Role�" || rows[1].Instructions != "changed" || rows[0].Instructions != "other" {
		t.Fatalf("instructions-only save: %#v %v", rows, err)
	}
	m.handleSessionPresetAction("e")
	// A real edit to the visible sanitized name is an explicit rename.
	m.handleSessionPresetsKey(namedKey(tea.KeyEnd))
	m.handleSessionPresetsKey(runeKey(" renamed"))
	_, cmd = m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
	rows, err = m.store.SessionPresets()
	if err != nil || len(rows) != 2 || rows[1].Name != "Role renamed" || rows[1].Instructions != "changed" {
		t.Fatalf("explicit rename: %#v %v", rows, err)
	}
}

func TestSessionPresetsMouseLiteralPasteSharesAsyncGuards(t *testing.T) {
	old := readSessionPresetClipboard
	t.Cleanup(func() { readSessionPresetClipboard = old })
	reads := 0
	raw := "\t literal\r\n⇥␍␛"
	readSessionPresetClipboard = func() (string, error) { reads++; return raw, nil }
	m := buildModel(t)
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	click := func() tea.Cmd {
		t.Helper()
		frame := m.viewSessionPresets()
		if (!m.presets.busy && !strings.Contains(frame, "ctrl+v")) || !strings.Contains(frame, "Paste") {
			t.Fatal("editor lacks paste action/key hint")
		}
		for i, hit := range m.presets.hits {
			if hit.action == "paste" {
				_, cmd := m.handleSessionPresetsClick(m.cardLeft+4, m.cardTop+2+i)
				return cmd
			}
		}
		t.Fatal("literal paste lacks mouse target")
		return nil
	}
	cmd := click()
	if cmd == nil || reads != 0 || !m.presets.pasting {
		t.Fatal("mouse paste did not dispatch asynchronously")
	}
	if again := click(); again != nil || reads != 0 {
		t.Fatal("duplicate mouse paste read dispatched")
	}
	_, save := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	if save != nil || !strings.Contains(m.errBar.text, "clipboard") {
		t.Fatal("save raced mouse clipboard read")
	}
	m.Update(cmd())
	if reads != 1 || m.presets.instructions.Value() != raw {
		t.Fatal("mouse paste changed literal bytes")
	}
	late := click()()
	_, cancel := m.handleSessionPresetsKey(namedKey(tea.KeyEsc))
	m.runBatch(t, cancel)
	m.handleSessionPresetAction("n")
	m.presets.instructions.SetValue("successor")
	m.Update(late)
	if m.presets.instructions.Value() != "successor" {
		t.Fatal("late mouse paste reached successor")
	}
	m.presets.instructions.SetValue("unsafe\x1b")
	if cmd := click(); cmd != nil || !strings.Contains(m.errBar.text, "Unsupported") {
		t.Fatal("read-only mouse paste not visibly refused")
	}
	m.presets.instructions.SetValue("before")
	m.presets.busy = true
	if cmd := click(); cmd != nil || reads != 2 || !strings.Contains(m.viewSessionPresets(), "Saving") {
		t.Fatal("busy mouse paste dispatched or state hidden")
	}
}

func TestSessionPresetsNameReplacementInputRefusedWithoutLoss(t *testing.T) {
	for _, paste := range []bool{false, true} {
		m := buildModel(t)
		m.openSettings()
		m.runBatch(t, m.openSessionPresets())
		m.handleSessionPresetAction("n")
		m.presets.name.SetValue("before")
		m.handleSessionPresetsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Role�"), Paste: paste})
		if m.presets.name.Value() != "before" || !strings.Contains(m.errBar.text, "unchanged") {
			t.Fatal("replacement input silently stripped or changed name")
		}
	}
}
func TestSessionPresetsNameExplicitReplacementCanEqualSanitizedDisplay(t *testing.T) {
	m := buildModel(t)
	for _, p := range []store.SessionPreset{{Name: "Role", Instructions: "other"}, {Name: "Role�", Instructions: "literal"}} {
		if err := m.store.SaveSessionPreset("", p); err != nil {
			t.Fatal(err)
		}
	}
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.presets.cursor = 1
	m.handleSessionPresetAction("e")
	if !strings.Contains(m.viewSessionPresets(), "Role�") {
		t.Fatal("original accepted name is not visible")
	}
	m.handleSessionPresetsKey(namedKey(tea.KeyCtrlA))
	m.handleSessionPresetsKey(namedKey(tea.KeyCtrlK))
	m.handleSessionPresetsKey(runeKey("Role"))
	_, cmd := m.handleSessionPresetsKey(namedKey(tea.KeyCtrlS))
	m.runBatch(t, cmd)
	if !m.presets.editing || m.errBar.text == "" || m.presets.name.Value() != "Role" {
		t.Fatal("explicit replacement equal to sanitized display was silently restored")
	}
	rows, err := m.store.SessionPresets()
	if err != nil || len(rows) != 2 || rows[1].Name != "Role�" {
		t.Fatal("collision changed accepted identity")
	}
}

func TestSessionPresetsPreviewPreservesSanitationAndUnicodeTruncation(t *testing.T) {
	for _, raw := range []string{
		"  alpha\t beta\r\n界  ",
		"a\x1b]52;c;unsafe\a\u009b end",
		"e\u0301界👨‍👩‍👧‍👦🇨🇦🛠️ final",
		" \u0301\t界\u2003🇨🇦 ",
		strings.Repeat("\t", 64*1024-4) + "last",
		"visible " + strings.Repeat("界", 21800),
	} {
		// This is the original displayed contract: controls become visible, whitespace collapses, graphemes truncate.
		sanitized := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				if unicode.IsSpace(r) {
					return ' '
				}
				return '�'
			}
			return r
		}, raw)
		sanitized = strings.Join(strings.Fields(sanitized), " ")
		for _, width := range []int{0, 1, 2, 3, 8, 12, 28, 60} {
			want := ansi.Truncate(sanitized, width, "…")
			got := safePresetPreview(raw, width)
			if got != want {
				t.Fatalf("width %d preview=%q want %q", width, got, want)
			}
			if strings.ContainsFunc(got, unicode.IsControl) {
				t.Fatal("preview emitted controls")
			}
			prefix := "Instructions: "
			got = ansi.Truncate(prefix+safePresetPreview(raw, max(0, width-ansi.StringWidth(prefix))), width, "…")
			want = ansi.Truncate(prefix+sanitized, width, "…")
			if got != want {
				t.Fatalf("prefixed width %d preview=%q want %q", width, got, want)
			}
		}
	}
}

func TestSessionPresetsLiteralMarkerCaretAndForwardDelete(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.runBatch(t, m.openSessionPresets())
	m.handleSessionPresetAction("n")
	m.presets.focus = 1
	raw := "a⇥b"
	m.presets.instructions.SetValue(raw)
	m.handleSessionPresetsKey(namedKey(tea.KeyLeft))
	m.handleSessionPresetsKey(namedKey(tea.KeyLeft))
	if m.presets.instructions.Value() != raw || m.presets.instructions.LineInfo().ColumnOffset != 1 {
		t.Fatal("left movement entered marker or changed bytes")
	}
	m.handleSessionPresetsKey(namedKey(tea.KeyRight))
	if m.presets.instructions.Value() != raw || m.presets.instructions.LineInfo().ColumnOffset != 3 {
		t.Fatal("right movement entered marker or changed bytes")
	}
	m.handleSessionPresetsKey(namedKey(tea.KeyLeft))
	m.handleSessionPresetsKey(namedKey(tea.KeyDelete))
	if m.presets.instructions.Value() != "ab" {
		t.Fatalf("forward delete broke literal marker: %q", m.presets.instructions.Value())
	}
}

func TestSessionPresetsSettingsWheelKeepsPickerEligibility(t *testing.T) {
	for _, blocked := range []string{"", "cli", "key", "editor"} {
		m := buildModel(t)
		m.openSettings()
		m.settings.cliPicker = blocked == "cli"
		m.settings.keyPicker = blocked == "key"
		m.settings.editor.typing = blocked == "editor"
		before := m.settings.field
		m.handleMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		if blocked == "" && m.settings.field != before+1 {
			t.Fatal("ordinary Settings wheel failed")
		}
		if blocked != "" && m.settings.field != before {
			t.Fatalf("wheel moved underlying Settings in %s picker", blocked)
		}
		// Presets remain the Settings subpanel's routing authority even with picker flags set.
		m.presets = sessionPresetPanel{open: true, rows: []store.SessionPreset{{Name: "A"}, {Name: "B"}}}
		m.handleMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		if m.presets.cursor != 1 {
			t.Fatalf("preset wheel not routed in %s", blocked)
		}
		m.handleMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		if m.presets.cursor != 0 {
			t.Fatalf("preset wheel up not routed in %s", blocked)
		}
	}
}
