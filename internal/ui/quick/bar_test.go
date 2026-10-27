package quick

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakePrompt is a one-line prompt box holding typed text.
type fakePrompt struct {
	text     string
	released bool
}

func (p *fakePrompt) Focus()                             {}
func (p *fakePrompt) Blur()                              {}
func (p *fakePrompt) Fit(int, int)                       {}
func (p *fakePrompt) View() string                       { return p.text }
func (p *fakePrompt) StepRow(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (p *fakePrompt) ChipKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (p *fakePrompt) Type(msg tea.KeyMsg) tea.Cmd        { p.text += string(msg.Runes); return nil }
func (p *fakePrompt) Release()                           { p.released = true }
func (p *fakePrompt) Pasting() bool                      { return false }
func (p *fakePrompt) Message() string                    { return strings.TrimSpace(p.text) }

// fakeHost drives the quick bar without a root model, on a CLI with no
// choices to pick.
type fakeHost struct {
	prompt   *fakePrompt
	target   Target
	selected bool
	settings map[string]string
	errs     []string
}

func (h *fakeHost) ChoiceCaps(string) spawn.Caps        { return spawn.Caps{} }
func (h *fakeHost) ChoiceRemoteHost() string            { return "" }
func (h *fakeHost) ChoiceAnswer(string) *spawn.Answer   { return nil }
func (h *fakeHost) ChoiceSetting(key string) string     { return h.settings[key] }
func (h *fakeHost) SaveChoiceSetting(key, value string) { h.settings[key] = value }
func (h *fakeHost) ReportErr(text string)               { h.errs = append(h.errs, text) }
func (h *fakeHost) ClearErr()                           {}
func (h *fakeHost) Prompt() spawn.Prompt                { return h.prompt }
func (h *fakeHost) Target() (Target, bool)              { return h.target, h.selected }

// The bar types into its prompt and hands the root a request for the
// rest: a cursor step, a worktree toggle, an answer, or a spawn.
func TestQuickBarThroughAFakeHost(t *testing.T) {
	h := &fakeHost{prompt: &fakePrompt{}, settings: map[string]string{}}
	q := NewBar(h, []string{"claude"}, 0, false)
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); request != (Request{}) || len(h.errs) != 0 {
		t.Fatalf("enter with nothing selected asked for %+v: %v", request, h.errs)
	}
	q.HandleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ship it"), Paste: true})
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyUp}); request.Move != -1 {
		t.Fatalf("up on a one-row prompt asked for %+v", request)
	}
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlT}); !request.Toggle {
		t.Fatalf("ctrl+t asked for %+v", request)
	}
	h.target, h.selected = Target{IsGroup: true, Group: "backend"}, true
	_, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if got := request.Spawn; got == nil || *got != (spawn.Request{Tool: "claude", Group: "backend", Prompt: "ship it"}) {
		t.Fatalf("spawn = %+v", request.Spawn)
	}
	q.SetWorktree(true)
	if _, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); !request.Spawn.WorktreeSet || !request.Spawn.Worktree {
		t.Fatalf("a toggled worktree was not asked for: %+v", request.Spawn)
	}
	if q.Legend(h)[0] != [2]string{"↵", "send"} {
		t.Fatalf("legend = %v", q.Legend(h))
	}
	h.target = Target{Group: "backend"}
	_, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request != (Request{Send: true, Text: "ship it"}) {
		t.Fatalf("send = %+v", request)
	}
	if _, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEsc}); request != (Request{Close: true}) || !h.prompt.released {
		t.Fatalf("esc asked for %+v", request)
	}
}

func TestEmptyEnterClosesAfterDebounce(t *testing.T) {
	for _, target := range []struct {
		name     string
		target   Target
		selected bool
	}{
		{name: "session", selected: true},
		{name: "group", target: Target{IsGroup: true}, selected: true},
		{name: "no selection"},
	} {
		t.Run(target.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := &fakeHost{prompt: &fakePrompt{text: " \n\t"}, target: target.target, selected: target.selected}
				q := NewBar(h, []string{"claude"}, 0, false)
				enter := tea.KeyMsg{Type: tea.KeyEnter}
				for range 3 {
					time.Sleep(emptyEnterDebounce - time.Millisecond)
					if _, request := q.HandleKey(h, enter); request != (Request{}) || h.prompt.released {
						t.Fatalf("repeated Enter closed the prompt: %+v", request)
					}
				}
				time.Sleep(emptyEnterDebounce)
				if _, request := q.HandleKey(h, enter); request != (Request{Close: true}) || !h.prompt.released {
					t.Fatalf("empty Enter did not close and release the prompt: %+v", request)
				}
				if len(h.errs) != 0 {
					t.Fatalf("empty Enter reported errors: %v", h.errs)
				}
			})
		})
	}
}

func TestEnterAfterSendKeepsQuickPromptOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &fakeHost{prompt: &fakePrompt{text: "ship it"}, selected: true}
		q := NewBar(h, []string{"claude"}, 0, false)
		if request := q.Submit(h); !request.Send || request.Text != "ship it" {
			t.Fatalf("nonempty submit was debounced: %+v", request)
		}
		// Delivery may take longer than the debounce interval.
		time.Sleep(time.Second)
		h.prompt.text = ""
		q.ResetEnterDebounce()
		if request := q.Submit(h); request != (Request{}) || h.prompt.released {
			t.Fatalf("Enter just after delivery closed the prompt: %+v", request)
		}
		time.Sleep(emptyEnterDebounce)
		if request := q.Submit(h); !request.Close {
			t.Fatalf("Enter after the debounce did not close: %+v", request)
		}
	})
}

// A remote spawn that leaves the worktree to the host names it in the
// glyph set the user picked, as every other worktree label does.
func TestHostDefaultWorktreeFollowsTheGlyphMode(t *testing.T) {
	h := &fakeHost{prompt: &fakePrompt{}, settings: map[string]string{}}
	q := NewBar(h, []string{"claude"}, 0, false)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		facts := Facts{Remote: "box", RemoteSpawn: true, WorktreeInherit: true, Styles: spawn.Styles{GlyphMode: mode}}
		want := presentation.GlyphsFor(mode).WorktreeGlyph + " host default"
		if got := ansi.Strip(q.View(h, facts, 120, 3)); !strings.Contains(got, want) {
			t.Fatalf("%s mode: bar %q lacks %q", mode, got, want)
		}
	}
}

// ctrl+g and a click step the spawn's environment from the target group's
// default; an untouched pick leaves the default to the root.
func TestQuickBarStepsTheEnvironmentFromTheTargetsDefault(t *testing.T) {
	h := &fakeHost{prompt: &fakePrompt{text: "ship it"}, settings: map[string]string{}}
	q := NewBar(h, []string{"claude"}, 0, false)
	h.target, h.selected = Target{IsGroup: true, Group: "backend"}, true
	q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlG})
	if len(h.errs) != 1 || !strings.Contains(h.errs[0], "none is defined") {
		t.Fatalf("ctrl+g with nothing to pick reported %v", h.errs)
	}
	h.target.Env = "work"
	q.Env().SetProfiles([]string{"team", "work"})
	_, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request.Spawn == nil || request.Spawn.EnvSet {
		t.Fatalf("an untouched env was decided for the root: %+v", request.Spawn)
	}
	if legend := q.Legend(h); !strings.Contains(strings.Join(legendKeys(legend), " "), EnvKey) {
		t.Fatalf("legend %v does not name %s", legend, EnvKey)
	}
	q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlG})
	if got := q.Env().For("work"); got != "" {
		t.Fatalf("ctrl+g from the default work = %q, want none", got)
	}
	q.Click(h, Hit{Action: ClickEnv})
	_, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request.Spawn == nil || !request.Spawn.EnvSet || request.Spawn.Env != "team" {
		t.Fatalf("spawn after stepping = %+v, want env team", request.Spawn)
	}
}

func legendKeys(pairs [][2]string) []string {
	keys := make([]string, len(pairs))
	for i, pair := range pairs {
		keys[i] = pair[0]
	}
	return keys
}

// A connection's spawn takes no profile of this machine, so the bar neither
// shows nor steps one there.
func TestQuickBarOffersNoEnvironmentOnAConnection(t *testing.T) {
	h := &fakeHost{prompt: &fakePrompt{text: "ship it"}, settings: map[string]string{}}
	q := NewBar(h, []string{"claude"}, 0, false)
	q.Env().SetProfiles([]string{"work"})
	h.target, h.selected = Target{IsGroup: true, Group: "backend", Remote: true}, true
	q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlG})
	if len(h.errs) != 1 || !strings.Contains(h.errs[0], "this machine") {
		t.Fatalf("ctrl+g on a connection reported %v", h.errs)
	}
	if q.Env().Touched() || slices.Contains(legendKeys(q.Legend(h)), EnvKey) {
		t.Fatal("a connection's bar stepped or offered the environment")
	}
}
