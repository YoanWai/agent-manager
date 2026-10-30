package focus

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeKeyHost is the root as one key press reaches it, so the routing runs
// with no Model.
type fakeKeyHost struct {
	id       string
	selected bool
	keys     Bindings
	writes   []Write
	copied   []CopyRequest
	reports  []string
	left     bool
	mode     bool
	editor   bool
	review   string
	saved    bool
	openURL  string
}

func (h *fakeKeyHost) Session() (string, bool) { return h.id, h.selected }

func (h *fakeKeyHost) PaneRows() int { return 10 }

func (h *fakeKeyHost) Bindings(string) Bindings { return h.keys }

func (h *fakeKeyHost) Queued(id string, write Write) tea.Cmd {
	h.writes = append(h.writes, write)
	return nil
}

func (h *fakeKeyHost) Region(*RegionRequest) tea.Cmd { return nil }

func (h *fakeKeyHost) Reported(report string, echo bool) { h.reports = append(h.reports, report) }

func (h *fakeKeyHost) Editor() (tea.Model, tea.Cmd) {
	h.editor = true
	return nil, nil
}

func (h *fakeKeyHost) Review(id string) (tea.Model, tea.Cmd) {
	h.review = id
	return nil, nil
}

func (h *fakeKeyHost) Saved() tea.Cmd {
	h.saved = true
	return nil
}

func (h *fakeKeyHost) Leave() tea.Cmd {
	h.left = true
	return nil
}

func (h *fakeKeyHost) LeaveMode() tea.Cmd {
	h.mode = true
	return nil
}

func (h *fakeKeyHost) Copied(request CopyRequest) tea.Cmd {
	h.copied = append(h.copied, request)
	return nil
}

func (h *fakeKeyHost) Opened(url string) (tea.Model, tea.Cmd) {
	h.openURL = url
	return nil, nil
}

func (h *fakeKeyHost) RuntimeReady() bool { return true }

func typed(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// A plain character is the agent's, encoded for its pane.
func TestKeyRoutesAPlainCharacterToThePane(t *testing.T) {
	host := &fakeKeyHost{id: "s1", selected: true}
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	focused.KeyPress(host, typed('q'))
	if len(host.writes) != 1 || host.writes[0].Kind != WriteKeys {
		t.Fatalf("writes = %+v", host.writes)
	}
	if host.left || host.mode {
		t.Fatal("a plain character left focus")
	}
}

// The manager's own keys never reach the agent.
func TestKeyTakesTheManagersKeys(t *testing.T) {
	cases := map[string]func(*fakeKeyHost){
		"detach": func(h *fakeKeyHost) { h.keys.Detach = true },
		"editor": func(h *fakeKeyHost) { h.keys.Editor = true },
		"review": func(h *fakeKeyHost) { h.keys.Review = true },
		"saved":  func(h *fakeKeyHost) { h.keys.Saved = true },
	}
	for name, set := range cases {
		host := &fakeKeyHost{id: "s1", selected: true}
		set(host)
		focused := &Model{}
		focused.Enter(EnterContext{SessionID: "s1"})
		focused.KeyPress(host, typed('x'))
		if len(host.writes) != 0 {
			t.Errorf("%s: the key reached the agent: %+v", name, host.writes)
		}
	}
}

// A paste rides its own transport; its newlines would otherwise land as Enter
// presses and submit the agent's prompt.
func TestKeyPastesOverItsOwnTransport(t *testing.T) {
	host := &fakeKeyHost{id: "s1", selected: true}
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("do it\n"), Paste: true}
	focused.KeyPress(host, msg)
	if len(host.writes) != 1 || host.writes[0].Kind != WritePaste || host.writes[0].Text != "do it\n" {
		t.Fatalf("writes = %+v", host.writes)
	}
}

// An empty selection leaves focus rather than dropping the key.
func TestKeyLeavesWithNoSelection(t *testing.T) {
	host := &fakeKeyHost{}
	focused := &Model{}
	focused.KeyPress(host, typed('a'))
	if !host.left || len(host.writes) != 0 {
		t.Fatalf("left=%t writes=%+v", host.left, host.writes)
	}
}

// A stale copy confirmation belongs to a selection that has gone.
func TestCopiedMessageBelongsToItsGeneration(t *testing.T) {
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	request := CopyRequest{Text: "alph", Generation: focused.copyGen}
	focused.clearSelection()
	if focused.ApplyCopiedMessage(CopiedMessage{Generation: request.Generation, Chars: 4}) {
		t.Fatal("a confirmation from the dropped selection was accepted")
	}
	if focused.ApplyCopiedMessage(CopiedMessage{Generation: focused.copyGen, Chars: 4}) != true {
		t.Fatal("the standing selection's confirmation was dropped")
	}
}

// fakeScrollHost lands one viewport.
type fakeScrollHost struct {
	id      string
	scroll  string
	regions int
}

func (h *fakeScrollHost) Session() (string, bool) { return h.id, h.id != "" }

func (h *fakeScrollHost) PaneRows() int { return 10 }

func (h *fakeScrollHost) Region(*RegionRequest) tea.Cmd {
	h.regions++
	return nil
}

func (h *fakeScrollHost) UsePreview(preview string) { h.scroll = preview }

// A viewport read for the selected session is what the view paints.
func TestApplyScrollUsesTheSelectedSessionsViewport(t *testing.T) {
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	host := &fakeScrollHost{id: "s1"}
	focused.ApplyScroll(ScrollMessage{SessionID: "s1", Rows: 10, Preview: "older output", OK: true}, host)
	if host.scroll != "older output" {
		t.Fatalf("preview = %q", host.scroll)
	}
}

// A viewport read for another session never replaces what the view paints.
func TestApplyScrollDropsAnotherSessionsViewport(t *testing.T) {
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	host := &fakeScrollHost{id: "s1"}
	focused.ApplyScroll(ScrollMessage{SessionID: "s2", Preview: "not ours", OK: true}, host)
	if host.scroll != "" {
		t.Fatalf("another session's viewport was painted: %q", host.scroll)
	}
}

// fakeFrameHost lands one watcher frame.
type fakeFrameHost struct {
	id      string
	preview string
}

func (h *fakeFrameHost) Session() (string, bool) { return h.id, h.id != "" }

func (h *fakeFrameHost) UsePreview(preview string) { h.preview = preview }

// A frame the focused session pushed is what the view paints.
func TestApplyFrameUsesTheSelectedSessionsFrame(t *testing.T) {
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	host := &fakeFrameHost{id: "s1"}
	focused.ApplyFrame(FrameMessage{SessionID: "s1", Preview: "live output"}, host)
	if host.preview != "live output" {
		t.Fatalf("preview = %q", host.preview)
	}
}

// A frame from another session is dropped, so a stale watcher never repaints
// the pane the user is on.
func TestApplyFrameDropsAnotherSessionsFrame(t *testing.T) {
	focused := &Model{}
	focused.Enter(EnterContext{SessionID: "s1"})
	host := &fakeFrameHost{id: "s1"}
	focused.ApplyFrame(FrameMessage{SessionID: "s2", Preview: "not ours"}, host)
	if host.preview != "" {
		t.Fatalf("another session's frame was painted: %q", host.preview)
	}
}
