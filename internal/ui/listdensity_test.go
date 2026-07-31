package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func railText(t *testing.T, m *Model) []string {
	t.Helper()
	var out []string
	for _, line := range m.entryLines(60, 20) {
		out = append(out, strings.TrimRight(ansi.Strip(line.text), " "))
	}
	return out
}

func lineWith(t *testing.T, lines []string, want string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("no rail line carrying %q:\n%s", want, strings.Join(lines, "\n"))
	return -1
}

// Compact keeps a session on one line; comfortable moves its meta to a
// second line under the name, and the choice persists.
func TestSettingsTogglesListDensity(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	lines := railText(t, m)
	head := lineWith(t, lines, "alpha")
	if !strings.Contains(lines[head], "claude") {
		t.Fatalf("compact row should carry its meta inline: %q", lines[head])
	}
	if got := m.entryHeight(m.rows[0]); got != 1 {
		t.Fatalf("compact entry height = %d want 1", got)
	}

	m.openSettings()
	if m.settings.comfortableRows {
		t.Fatal("settings should open on compact by default")
	}
	for i := 0; i < settingsFieldDensity; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldDensity {
		t.Fatalf("stepping down should reach the density field, got %d", m.settings.field)
	}
	if card := ansi.Strip(m.viewSettings()); !strings.Contains(card, "list density") {
		t.Fatalf("settings card has no density row:\n%s", card)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if card := ansi.Strip(m.viewSettings()); !strings.Contains(card, "comfortable") {
		t.Fatalf("toggled card does not read comfortable:\n%s", card)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.comfortableRows {
		t.Fatal("model did not pick up the comfortable density")
	}
	if !storedComfortableRows(m.store) {
		t.Fatal("comfortable density did not persist")
	}
	if got := m.entryHeight(m.rows[0]); got != 2 {
		t.Fatalf("comfortable entry height = %d want 2", got)
	}

	lines = railText(t, m)
	head = lineWith(t, lines, "alpha")
	if strings.Contains(lines[head], "claude") {
		t.Fatalf("comfortable name line should not carry meta: %q", lines[head])
	}
	if head+1 >= len(lines) {
		t.Fatalf("comfortable row has no meta line:\n%s", strings.Join(lines, "\n"))
	}
	meta := lines[head+1]
	if !strings.Contains(meta, "claude") || !strings.Contains(meta, statusLabel(m.rows[0].sess.Status)) {
		t.Fatalf("meta line = %q", meta)
	}
	if indent := len(meta) - len(strings.TrimLeft(meta, " ")); indent < railInset+2 {
		t.Fatalf("meta line should sit under the name, indent = %d: %q", indent, meta)
	}
}

// Groups follow the same density so the list keeps one rhythm.
func TestComfortableGroupRowStacks(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	m.openGroupForm()
	m.groupForm.name.SetValue("fleet")
	if _, _ = m.submitGroupForm(); m.err != "" {
		t.Fatalf("create group: %q", m.err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "beta", t.TempDir(), "fleet")

	lines := railText(t, m)
	head := lineWith(t, lines, "fleet")
	if head+1 >= len(lines) || strings.TrimSpace(lines[head+1]) == "" {
		t.Fatalf("group row has no meta line:\n%s", strings.Join(lines, "\n"))
	}
}
