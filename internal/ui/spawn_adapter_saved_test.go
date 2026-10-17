package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func focusSavedRow(t *testing.T, m *Model) {
	t.Helper()
	m.form.FocusField(m.spawnHost(), spawn.FieldSaved)
	if m.form.Focused() != spawn.FieldSaved {
		t.Fatal("the saved prompts row took no focus")
	}
}

// The form reads the catalog as it opens and offers it on its own row.
func TestFormOffersSavedPrompts(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Coordinator", "  literal instructions\n\n")
	m.runBatch(t, m.openForm())
	if body := formBody(m); !strings.Contains(body, "saved prompts ◂ none ▸") {
		t.Fatalf("form:\n%s", body)
	}
}

// Stepping the row changes the text in front of the task and nothing else,
// and the form keeps the text it showed when the catalog changes under it.
func TestFormSavedPromptStepsLeaveTheRestAlone(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "A", " \n a\n")
	savePrompt(t, m, "B", "b")
	m.runBatch(t, m.openForm())
	m.form.prompt.input.SetValue("  task  ")
	tools, toolIndex := m.form.Tools()
	dir, group, worktree, model := m.form.Dir(), m.form.groupIndex, m.form.Worktree(), m.form.Choice().Model()
	focusSavedRow(t, m)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.form.SavedText(); got != " \n a\n" {
		t.Fatalf("saved text = %q", got)
	}
	if _, err := m.services.store.DeleteSessionPreset("A"); err != nil {
		t.Fatal(err)
	}
	if got := m.form.SavedText(); got != " \n a\n" {
		t.Fatal("a catalog edit changed the text the form shows")
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.form.SavedText(); got != "" {
		t.Fatalf("stepping past the last kept %q", got)
	}
	newTools, newIndex := m.form.Tools()
	if m.form.prompt.input.Value() != "  task  " || newTools[newIndex] != tools[toolIndex] || m.form.Dir() != dir ||
		m.form.groupIndex != group || m.form.Worktree() != worktree || m.form.Choice().Model() != model {
		t.Fatal("stepping saved prompts changed another field")
	}
}

// A click in the open list picks without touching the task, its images or
// the CLI choices.
func TestFormSavedPromptClickKeepsTheTask(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Role", "instruct")
	m.runBatch(t, m.openForm())
	focusSavedRow(t, m)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	m.form.prompt.input.SetValue("task " + imageToken(1))
	m.form.prompt.attachments = []imageAttachment{{id: 1, path: tempImage(t, "image.png")}}
	before := m.form.prompt.message()
	model := m.form.Choice().Model()
	clicked := false
	for y, line := range strings.Split(ansi.Strip(m.viewForm()), "\n") {
		if strings.Contains(line, "Role") {
			m.handleFormClick(m.layout.cardLeft+4, y)
			clicked = true
			break
		}
	}
	if !clicked || m.form.SavedText() != "instruct" {
		t.Fatalf("a click on Role picked %q", m.form.SavedText())
	}
	if m.form.prompt.message() != before || len(m.form.prompt.attachments) != 1 || m.form.Choice().Model() != model {
		t.Fatal("the click changed the task or the CLI choice")
	}
}

// A catalog that cannot be read says so, and the form still launches
// without a saved prompt.
func TestFormSavedPromptsReadFailureShows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	cmd := m.openForm()
	m.services.store.Close()
	m.runBatch(t, cmd)
	if body := formBody(m); !strings.Contains(body, "Reading saved prompts") {
		t.Fatalf("form:\n%s", body)
	}
	if m.form.SavedText() != "" {
		t.Fatal("a failed read picked a saved prompt")
	}
}

// The spawn carries the text the form showed, as written, in front of the
// trimmed task, even when the catalog changed after the form read it.
func TestFormSpawnsWithTheSavedPromptItShowed(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Role", " \n instruction\n\n")
	m.runBatch(t, m.openForm())
	pickFormTool(t, m, "ready-tool")
	focusSavedRow(t, m)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	m.form.prompt.input.SetValue("  task  ")
	m.form.SetName("saved-launch")
	m.form.SetDir(t.TempDir())
	if err := m.services.store.SaveSessionPreset("Role", store.SessionPreset{Name: "Role", Instructions: "external replacement"}); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	rows := m.sessionRows()
	if m.mode != modeList || len(rows) != 1 {
		t.Fatalf("submit: mode %v err %q rows %d", m.mode, m.errBar.text, len(rows))
	}
	if !strings.HasSuffix(rows[0].LaunchPrompt, " \n instruction\n\n\n\ntask") {
		t.Fatalf("launch prompt = %q", rows[0].LaunchPrompt)
	}
}

// Saved text that opens with a dash launches behind the rename note, for a
// CLI that takes the prompt as an argument and one that has it typed.
func TestFormSavedPromptWithADashLaunches(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"ready-tool", "send-tool"} {
		t.Run(tool, func(t *testing.T) {
			m := buildModel(t)
			savePrompt(t, m, "Bullets", "- literal bullet\n")
			m.runBatch(t, m.openForm())
			pickFormTool(t, m, tool)
			focusSavedRow(t, m)
			m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
			m.form.SetName("dash-saved")
			m.form.SetDir(t.TempDir())
			_, cmd := m.submitForm()
			m.applyCmd(t, cmd)
			rows := m.sessionRows()
			if m.mode != modeList || len(rows) != 1 {
				t.Fatalf("submit: mode %v err %q", m.mode, m.errBar.text)
			}
			prompt := rows[0].LaunchPrompt
			if tool == "send-tool" {
				if len(rows[0].PendingInputs) == 0 {
					t.Fatal("no pending input")
				}
				prompt = rows[0].PendingInputs[0]
			}
			if strings.HasPrefix(prompt, "-") || !strings.Contains(prompt, launch.RenameAvailableNote) || !strings.HasSuffix(prompt, "- literal bullet\n") {
				t.Fatalf("prompt = %q", prompt)
			}
		})
	}
}

// A form submitted before its catalog read lands launches the plain task.
func TestFormLaunchesBeforeTheSavedPromptsRead(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	m.form.prompt.input.SetValue("task")
	m.form.SetName("without-saved")
	m.form.SetDir(t.TempDir())
	if !strings.Contains(formBody(m), "loading… (optional)") {
		t.Fatal("the row does not say the read is running")
	}
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	rows := m.sessionRows()
	if m.mode != modeList || len(rows) != 1 || !strings.HasSuffix(rows[0].LaunchPrompt, "task") {
		t.Fatalf("submit: mode %v err %q rows %+v", m.mode, m.errBar.text, rows)
	}
}
