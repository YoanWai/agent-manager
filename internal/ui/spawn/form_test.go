package spawn

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// fakePrompt is a one-line prompt box holding typed text.
type fakePrompt struct {
	text     string
	focused  bool
	released bool
	pasting  bool
}

func (p *fakePrompt) Focus()                             { p.focused = true }
func (p *fakePrompt) Blur()                              { p.focused = false }
func (p *fakePrompt) Fit(int, int)                       {}
func (p *fakePrompt) View() string                       { return p.text }
func (p *fakePrompt) StepRow(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (p *fakePrompt) ChipKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (p *fakePrompt) Type(msg tea.KeyMsg) tea.Cmd        { p.text += string(msg.Runes); return nil }
func (p *fakePrompt) Release()                           { p.released = true }
func (p *fakePrompt) Pasting() bool                      { return p.pasting }
func (p *fakePrompt) Message() string                    { return strings.TrimSpace(p.text) }

// fakePaths is a completer that records what it was asked to complete.
type fakePaths struct {
	suggested []string
	resets    int
}

func (p *fakePaths) Active() bool      { return false }
func (p *fakePaths) Chosen() bool      { return false }
func (p *fakePaths) Move(int) bool     { return false }
func (p *fakePaths) Reset()            { p.resets++ }
func (p *fakePaths) View() string      { return "" }
func (p *fakePaths) Hint() [][2]string { return nil }
func (p *fakePaths) Suggest(typed string) tea.Cmd {
	p.suggested = append(p.suggested, typed)
	return func() tea.Msg { return nil }
}

// fakeFormHost drives the New Session form without a root model.
type fakeFormHost struct {
	*fakeChoiceHost
	prompt *fakePrompt
	paths  *fakePaths
}

func (h *fakeFormHost) Prompt() Prompt { return h.prompt }

func (h *fakeFormHost) Paths() Paths { return h.paths }

func newFakeFormHost() *fakeFormHost {
	h := &fakeFormHost{fakeChoiceHost: newFakeChoiceHost(), prompt: &fakePrompt{}, paths: &fakePaths{}}
	h.caps["plain"] = Caps{}
	return h
}

// The form's keys update its own fields and hand the root a request for
// the rest: a new CLI's catalog, a worktree probe, closing, or the spawn.
func TestFormDialogThroughAFakeHost(t *testing.T) {
	h := newFakeFormHost()
	d := NewForm(h, []string{"claude", "plain"}, 0)
	for _, r := range "fix" {
		d.HandleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.FocusField(h, FieldTool)
	if _, request := d.HandleKey(h, tea.KeyMsg{Type: tea.KeyRight}); request.Catalog != "plain" || d.Tool() != "plain" {
		t.Fatalf("right on the tool row asked for %+v on %q", request, d.Tool())
	}
	d.FocusField(h, FieldGroup)
	if _, request := d.HandleKey(h, tea.KeyMsg{Type: tea.KeyRight}); request.Group != 1 {
		t.Fatalf("right on the group row asked for %+v", request)
	}
	d.FocusField(h, FieldDir)
	cmd, request := d.HandleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !request.Probe || cmd == nil || d.DirAuto() || len(h.paths.suggested) != 1 {
		t.Fatalf("typing a path should read suggestions and probe: %+v", request)
	}
	d.SetWorktree(true)
	_, request = d.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request.Spawn == nil {
		t.Fatalf("enter built no spawn: %v", h.errs)
	}
	if got, want := *request.Spawn, (Request{Tool: "plain", Name: "fix", Dir: "/", Worktree: true, WorktreeSet: true}); got != want {
		t.Fatalf("spawn = %+v, want %+v", got, want)
	}
	h.prompt.text = "-rf"
	if request = d.Submit(h); request.Spawn == nil || request.Spawn.Prompt != "-rf" {
		t.Fatalf("a leading dash is text after the rename note: %+v %v", request, h.errs)
	}
	if _, request = d.HandleKey(h, tea.KeyMsg{Type: tea.KeyEsc}); !request.Close || !h.prompt.released {
		t.Fatalf("esc asked for %+v", request)
	}
}

// A click on a group row asks the root to move its picker there, and a
// click on another field's row moves focus to it.
func TestFormClicksThroughAFakeHost(t *testing.T) {
	h := newFakeFormHost()
	d := NewForm(h, []string{"claude"}, 0)
	d.FocusField(h, FieldGroup)
	body, _ := d.View(h, FormFacts{Width: 40, Group: "root", GroupPicker: []string{"root", "backend"}, Styles: Styles{Input: func(in textinput.Model) string { return in.View() }}, Height: 40, Chrome: func([][2]string) int { return 6 }})
	lines := strings.Split(body, "\n")
	if request := d.Click(h, len(lines)-1); !request.PickGroup || request.GroupEntry != 1 {
		t.Fatalf("a click on the second picker row asked for %+v", request)
	}
	if request := d.Click(h, 0); request != (FormRequest{}) || d.Focused() != FieldName {
		t.Fatalf("a click on the name row asked for %+v, focus %v", request, d.Focused())
	}
	if request := d.Click(h, len(lines)); request != (FormRequest{}) {
		t.Fatalf("a click under the body asked for %+v", request)
	}
}

// fakeSuggestions is a completer with suggestions open.
type fakeSuggestions struct{ fakePaths }

func (p *fakeSuggestions) Active() bool { return true }
func (p *fakeSuggestions) View() string { return "      /a\n      /b" }

func TestFormClickCompletesAPathAndTheWheelScrollsTheGroups(t *testing.T) {
	h := newFakeFormHost()
	suggestions := &fakeSuggestions{}
	host := &suggestingHost{fakeFormHost: h, paths: suggestions}
	d := NewForm(host, []string{"claude"}, 0)
	d.FocusField(host, FieldDir)
	body, _ := d.View(host, FormFacts{Width: 40, Group: "root", Styles: Styles{Input: func(in textinput.Model) string { return in.View() }}, Height: 40, Chrome: func([][2]string) int { return 6 }})
	lines := strings.Split(body, "\n")
	line := -1
	for i, text := range lines {
		if strings.TrimSpace(text) == "/b" {
			line = i
		}
	}
	if line < 0 {
		t.Fatalf("test setup: no suggestion painted:\n%s", body)
	}
	if request := d.Click(host, line); !request.PickPath || request.PathEntry != 1 {
		t.Fatalf("a click on the second suggestion asked for %+v", request)
	}
	if request := d.Wheel(host, 1); request.Group != 0 {
		t.Fatalf("the wheel off the group field asked for %+v", request)
	}
	d.FocusField(host, FieldGroup)
	if request := d.Wheel(host, -1); request.Group != -1 {
		t.Fatalf("the wheel over the group picker asked for %+v", request)
	}
}

type suggestingHost struct {
	*fakeFormHost
	paths *fakeSuggestions
}

func (h *suggestingHost) Paths() Paths { return h.paths }
