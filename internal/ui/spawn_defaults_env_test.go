package ui

import (
	"strings"
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

func defineEnvs(t *testing.T, m *Model, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := m.services.store.SetEnvVars(name, map[string]string{"AM_TEST_ENV": name}); err != nil {
			t.Fatalf("define %s: %v", name, err)
		}
	}
}

func TestGroupFormStoresTheEnvironmentAndNamesTheInheritedOne(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openGroupForm()
	if view := m.viewGroupForm(); strings.Contains(view, "◂ none ▸") {
		t.Fatalf("with no profile the form should have no env row, got %q", view)
	}
	m.mode = modeList
	defineEnvs(t, m, "team", "work")
	if err := m.services.store.CreateGroup("parent", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetGroupEnvironment("parent", "work"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "parent")
	m.openGroupForm()
	if view := m.viewGroupForm(); !strings.Contains(view, "◂ none ▸  work from parent") {
		t.Fatalf("none under a parent with an env should name it, got %q", view)
	}
	m.groupForm.SetName("child")
	focusGroupField(t, m, uidialogs.GroupFormEnvField)
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group.Name == "parent/child" && group.Environment != "team" {
			t.Fatalf("child environment = %q, want team", group.Environment)
		}
	}
	if m.workspace.groupEnvs["parent/child"] != "team" {
		t.Fatalf("the new group's env is not mirrored locally: %v", m.workspace.groupEnvs)
	}
}

func TestFormStartsOnTheGroupEnvironmentAndSpawnsUnderIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	defineEnvs(t, m, "team", "work")
	dir := t.TempDir()
	for _, group := range []string{"office", "home"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetGroupEnvironment("office", "work"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "office")
	m.applyTestMsg(t, m.openForm()())
	if got := m.form.Env().Name(); got != "work" {
		t.Fatalf("form env = %q, want the group's work", got)
	}
	if view := m.viewForm(); !strings.Contains(view, "◂ work ▸  (group)") {
		t.Fatalf("the env row should show the group's pick, got %q", view)
	}
	pickGroup(t, m, "home")
	m.moveGroupCursor(0)
	if got := m.form.Env().Name(); got != "" {
		t.Fatalf("form env after moving to a group without one = %q", got)
	}
	pickGroup(t, m, "office")
	m.moveGroupCursor(0)
	m.form.SetName("worker")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	sessions, err := m.services.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %+v, %v", sessions, err)
	}
	if sessions[0].Environment != "work" {
		t.Fatalf("spawned environment = %q, want work", sessions[0].Environment)
	}
}

func TestQuickSpawnTakesTheTargetGroupsEnvironment(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	defineEnvs(t, m, "work")
	if err := m.services.store.CreateGroup("office", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetGroupEnvironment("office", "work"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "office")
	m.applyTestMsg(t, m.openQuickMode()())
	request, ok := m.quick.SpawnRequest(m.spawnHost(), "office", "ship it")
	if !ok {
		t.Fatalf("quick spawn refused: %q", m.errBar.text)
	}
	if got := m.quickSpawnRequest(request).environment; got != "work" {
		t.Fatalf("quick spawn environment = %q, want the group's work", got)
	}
}
