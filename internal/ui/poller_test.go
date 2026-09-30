package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	tea "github.com/charmbracelet/bubbletea"
)

// A detached session must boot at the preview panel's width×height so its
// pane preview fills 1:1, and follow later terminal resizes, rather than
// staying at tmux's 80×24 default until attach.
func TestSessionSizesToPreviewPane(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	// Create sizes from pre-selection geometry; re-pin to the live preview box.
	m.resizeSessions()

	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("new session window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}

	m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	wantW, wantH = m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("after resize, window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}

// writeName queues a rename the way the subcommand does, and returns the
// request its answer comes back under.
func writeName(t *testing.T, m *Model, id, name string) string {
	t.Helper()
	path := m.services.hooks.NameFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	request, err := hooks.NewRequestID()
	if err != nil {
		t.Fatalf("request id: %v", err)
	}
	if err := os.WriteFile(path, []byte(hooks.NameRequest(request, name)), 0o644); err != nil {
		t.Fatalf("write name file: %v", err)
	}
	return request
}

func TestRefreshWithStaleSelectionFetchesPreview(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "fresh-one", t.TempDir(), "")
	m.selectSessionRow(t, "fresh-one")
	sess := m.sessionRows()[0]

	_, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: ""})
	if cmd == nil {
		t.Fatal("stale refresh should schedule an immediate preview fetch")
	}
	m.applyCmd(t, cmd)
	if m.workspace.preview == "" {
		t.Fatal("scheduled selection refresh produced no preview")
	}

	m.workspace.preview = "existing"
	if _, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: sess.ID, preview: "pane text"}); cmd != nil {
		t.Fatal("matching refresh should not schedule extra work")
	}
	if m.workspace.preview != "pane text" {
		t.Fatalf("preview = %q want %q", m.workspace.preview, "pane text")
	}
}

func TestSweepPastesReportsSweepError(t *testing.T) {
	orig := sweepStalePastes
	defer func() { sweepStalePastes = orig }()
	sweepStalePastes = func() error { return errors.New("permission denied") }

	m := &Model{}
	msg, ok := m.sweepPastes().(pasteSweepMsg)
	if !ok {
		t.Fatalf("want pasteSweepMsg, got %T", m.sweepPastes())
	}
	if msg.err == nil || msg.err.Error() != "permission denied" {
		t.Fatalf("got %v", msg.err)
	}
}

func TestPasteSweepMsgSurfacesErrorOnce(t *testing.T) {
	m := buildModel(t)
	m.Update(pasteSweepMsg{err: errors.New("permission denied")})
	if m.errBar.text == "" {
		t.Fatal("a failed sweep must reach the user")
	}
	m.errBar.text = ""
	m.Update(pasteSweepMsg{})
	if m.errBar.text != "" {
		t.Fatalf("a clean sweep must stay silent, got %q", m.errBar.text)
	}
}

func TestPasteSweepTickSweepsAgainAndRearms(t *testing.T) {
	m := buildModel(t)
	_, cmd := m.Update(pasteSweepTickMsg{})
	if cmd == nil {
		t.Fatal("tick must return work")
	}
	// A manager left open for weeks only keeps sweeping if the tick both
	// sweeps and re-arms, so the batch must carry two commands. Running the
	// timer itself here would wait out the real interval.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("want a batch, got %T", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("want sweep plus re-arm, got %d commands", len(batch))
	}
}
