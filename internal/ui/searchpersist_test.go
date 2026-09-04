package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func searchModel() *Model {
	m := &Model{width: 120, height: 30, collapsed: map[string]bool{}}
	m.sessions = []store.Session{
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
	m.searching, m.search = true, "api"
	m.rebuildRows()
	if !strings.Contains(railHead(m), "⌕") {
		t.Fatal("an open field should be in the rail")
	}
	m.searching = false
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
	m.searching, m.search = true, "api"
	m.rebuildRows()
	filtered := len(m.rows)

	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searching || m.search != "api" || len(m.rows) != filtered {
		t.Fatalf("enter should close the field and keep the filter, got searching=%v query=%q rows=%d",
			m.searching, m.search, len(m.rows))
	}

	// Through handleKey rather than clearSearch: the binding is half of what
	// this covers, so a test that skips it would pass with esc unbound.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.search != "" {
		t.Fatalf("esc should clear the query, got %q", m.search)
	}
	if len(m.rows) <= filtered {
		t.Fatalf("clearing should bring the sessions back, still %d rows", len(m.rows))
	}
}

func TestEscInTheFieldClearsIt(t *testing.T) {
	m := searchModel()
	m.searching, m.search = true, "api"
	m.rebuildRows()
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.searching || m.search != "" {
		t.Fatalf("esc should close and clear, got searching=%v query=%q", m.searching, m.search)
	}
	if strings.Contains(railHead(m), "⌕") {
		t.Fatal("a cleared search should leave no field behind")
	}
}
