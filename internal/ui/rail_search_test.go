package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestSearchFieldFillsOnlyWhileSearching(t *testing.T) {
	m := shotModel()
	m.rail.search = "note"
	fieldLine := func() contentLine {
		for _, line := range m.railLines(40, 14) {
			if strings.Contains(ansi.Strip(line.text), "⌕") {
				return line
			}
		}
		t.Fatal("rail painted no search field")
		return contentLine{}
	}

	m.rail.searching = true
	open := fieldLine()
	if open.tone != searchFieldHex() {
		t.Fatalf("open field tone = %q, want %q", open.tone, searchFieldHex())
	}
	if !strings.Contains(open.text, bgSeq(searchFieldHex())) {
		t.Fatalf("open field line carries no fill:\n%q", open.text)
	}

	m.rail.searching = false
	closed := fieldLine()
	if closed.tone != "" {
		t.Fatalf("closed field with a query applied should keep the panel tone, got %q", closed.tone)
	}
	if strings.Contains(closed.text, bgSeq(searchFieldHex())) {
		t.Fatalf("closed field line should not be filled:\n%q", closed.text)
	}
}

func TestSearchLightsTheQueryInsideASessionName(t *testing.T) {
	forceANSI256(t)
	entry := treeRow{sess: store.Session{
		ID: "s1", Name: "alpha-build", Tool: "grok", Status: status.Idle, CreatedAt: time.Now(),
	}}
	lit := searchMatchStyle.Render("bui")

	m := &Model{rail: railState{search: "BUI"}}
	row := m.renderTreeRow(entry, false, 80, 0, panelHex())
	if !strings.Contains(row, lit) {
		t.Fatalf("query should light its span in the name's own case:\n%q", row)
	}
	if !strings.Contains(ansi.Strip(row), "alpha-build") {
		t.Fatalf("name lost text around the lit span:\n%q", ansi.Strip(row))
	}

	selected := m.renderTreeRow(entry, true, 80, 0, selectedHex())
	bright := lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	if !strings.Contains(selected, bright.Render("alpha-")) || !strings.Contains(selected, lit) {
		t.Fatalf("selected row should keep its bright name around the lit span:\n%q", selected)
	}

	m.rail.search = "grok"
	row = m.renderTreeRow(entry, false, 80, 0, panelHex())
	if strings.Contains(row, sgrOf(lit)) {
		t.Fatalf("a match on the tool alone should leave the name plain:\n%q", row)
	}

	m.rail.search = ""
	row = m.renderTreeRow(entry, false, 80, 0, panelHex())
	if !strings.Contains(row, valueStyle.Render("alpha-build")) {
		t.Fatalf("no query should render the plain name:\n%q", row)
	}
}

func TestSearchLightsTheQueryInsideAGroupName(t *testing.T) {
	forceANSI256(t)
	m := &Model{rail: railState{search: "END"}}
	row := m.renderTreeRow(treeRow{isGroup: true, group: "work/backend"}, false, 80, 0, panelHex())
	if !strings.Contains(row, searchMatchStyle.Render("end")) {
		t.Fatalf("group name should light the query:\n%q", row)
	}
	if !strings.Contains(ansi.Strip(row), "backend") {
		t.Fatalf("group name lost text around the lit span:\n%q", ansi.Strip(row))
	}
}

func searchModel() *Model {
	m := &Model{
		width:  120,
		height: 30,
		rail: railState{
			collapsed: map[string]bool{},
		},
	}
	m.workspace.sessions = []store.Session{
		{ID: "1", Name: "api-server", Status: status.Idle},
		{ID: "2", Name: "web-ui", Status: status.Idle},
		{ID: "3", Name: "docs", Status: status.Idle},
	}
	m.rebuildRows()
	return m
}

func railHead(m *Model) string {
	var b strings.Builder
	for _, line := range m.railLines(40, 24) {
		b.WriteString(ansi.Strip(line.text) + "\n")
	}
	return b.String()
}

// A query that outlives its field has to keep saying so: without the row the
// filtered-away sessions read as sessions that are gone.
func TestSearchFieldOutlivesTheOpenField(t *testing.T) {
	m := searchModel()
	m.rail.searching, m.rail.search = true, "api"
	m.rebuildRows()
	if !strings.Contains(railHead(m), "⌕") {
		t.Fatal("an open field should be in the rail")
	}
	m.rail.searching = false
	rail := railHead(m)
	if !strings.Contains(rail, "⌕") {
		t.Fatalf("a query still filtering should stay in the rail:\n%s", rail)
	}
	if !strings.Contains(rail, "clear") {
		t.Fatalf("a closed field should offer the way out:\n%s", rail)
	}
}

func TestSearchRailIsCleanWithNoQuery(t *testing.T) {
	if rail := railHead(searchModel()); strings.Contains(rail, "⌕") {
		t.Fatalf("no query should mean no field:\n%s", rail)
	}
}

func TestEnterKeepsTheQueryAndEscClearsIt(t *testing.T) {
	m := searchModel()
	m.rail.searching, m.rail.search = true, "api"
	m.rebuildRows()
	filtered := len(m.rail.rows)

	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.rail.searching || m.rail.search != "api" || len(m.rail.rows) != filtered {
		t.Fatalf("enter should close the field and keep the filter, got searching=%v query=%q rows=%d",
			m.rail.searching, m.rail.search, len(m.rail.rows))
	}

	// Through handleKey rather than clearSearch: the binding is half of what
	// this covers, so a test that skips it would pass with esc unbound.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.search != "" {
		t.Fatalf("esc should clear the query, got %q", m.rail.search)
	}
	if len(m.rail.rows) <= filtered {
		t.Fatalf("clearing should bring the sessions back, still %d rows", len(m.rail.rows))
	}
}

func TestEscInTheFieldClearsIt(t *testing.T) {
	m := searchModel()
	m.rail.searching, m.rail.search = true, "api"
	m.rebuildRows()
	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.searching || m.rail.search != "" {
		t.Fatalf("esc should close and clear, got searching=%v query=%q", m.rail.searching, m.rail.search)
	}
	if strings.Contains(railHead(m), "⌕") {
		t.Fatal("a cleared search should leave no field behind")
	}
}
