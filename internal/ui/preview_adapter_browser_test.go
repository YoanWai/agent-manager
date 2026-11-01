package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	"github.com/charmbracelet/x/ansi"
)

const placeholderCell = "\U0010EEEE"

const outerTmuxLine = "run agent-manager outside tmux to see browser pages"

// placeholderPane is how a browser pane captures: rows of image placeholder
// cells carrying their diacritics and colours, which must never be painted.
var placeholderPane = strings.Repeat("\x1b[38;5;1m"+strings.Repeat(placeholderCell+"\u0305\u0305", 20)+"\x1b[0m\n", 12)

// defaultCardKeys are the attach and revive keys the shipped list table binds.
var defaultCardKeys = browserCardFacts{attachKey: "A", reviveKey: "v"}

func withFacts(facts browserCardFacts, running, starting, insideTmux bool) browserCardFacts {
	facts.running, facts.starting, facts.insideTmux = running, starting, insideTmux
	return facts
}

func TestBrowserCardStates(t *testing.T) {
	sess := store.Session{ID: "docs", Tool: "browser", BrowserURL: "https://stored.example/"}
	for _, test := range []struct {
		name     string
		state    browserState
		running  bool
		starting bool
		want     []string
	}{
		{
			name:    "a running engine names its page",
			state:   browserState{URL: "https://example.org/docs", Title: "Example Docs", Engine: true},
			running: true,
			want:    []string{"Example Docs", "https://example.org/docs", "press A to see and use the page"},
		},
		{
			name:    "an untitled page an agent drives",
			state:   browserState{URL: "https://example.org/", Engine: true, AgentDriven: true},
			running: true,
			want:    []string{"https://example.org/", "press A to see and use the page", "agent is driving it"},
		},
		{
			name:    "an engine with no url yet falls back to the stored one",
			state:   browserState{Engine: true},
			running: true,
			want:    []string{"https://stored.example/", "press A to see and use the page"},
		},
		{
			name:     "a pane still starting its engine",
			running:  true,
			starting: true,
			want:     []string{"starting the browser"},
		},
		{
			name:    "a pane whose engine never came up",
			running: true,
			want:    []string{"browser engine did not start, press A for details"},
		},
		{
			name:  "a dead pane",
			state: browserState{URL: "https://example.org/", Engine: true},
			want:  []string{"browser closed, v revives it"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := browserCard(test.state, sess, withFacts(defaultCardKeys, test.running, test.starting, false), 80, 10)
			if !slices.Equal(got, test.want) {
				t.Fatalf("card = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBrowserCardNamesTheBoundKeys(t *testing.T) {
	page := browserState{URL: "https://example.org/", Title: "Example", Engine: true}
	rebound := browserCardFacts{attachKey: "ctrl+o", reviveKey: "R"}
	if got := browserCard(page, store.Session{}, withFacts(rebound, true, false, false), 80, 10); !slices.Contains(got, "press ctrl+o to see and use the page") {
		t.Fatalf("a rebound attach key should be named: %q", got)
	}
	if got := browserCard(browserState{}, store.Session{}, withFacts(rebound, true, false, false), 80, 10); !slices.Equal(got, []string{"browser engine did not start, press ctrl+o for details"}) {
		t.Fatalf("no engine card = %q", got)
	}
	if got := browserCard(page, store.Session{}, withFacts(rebound, false, false, false), 80, 10); !slices.Equal(got, []string{"browser closed, R revives it"}) {
		t.Fatalf("closed card = %q", got)
	}
	unbound := browserCardFacts{}
	if got := browserCard(page, store.Session{}, withFacts(unbound, true, false, false), 80, 10); !slices.Equal(got, []string{"Example", "https://example.org/"}) {
		t.Fatalf("with no attach key the card should offer none: %q", got)
	}
	if got := browserCard(browserState{}, store.Session{}, withFacts(unbound, true, false, false), 80, 10); !slices.Equal(got, []string{"browser engine did not start"}) {
		t.Fatalf("no engine card with no attach key = %q", got)
	}
	if got := browserCard(page, store.Session{}, withFacts(unbound, false, false, false), 80, 10); !slices.Equal(got, []string{"browser closed"}) {
		t.Fatalf("closed card with no revive key = %q", got)
	}
}

func TestBrowserCardFitsItsBox(t *testing.T) {
	state := browserState{URL: "https://example.org/a/" + placeholderCell + "/path/that/runs/on", Title: "\x1b[31mA page title\x07 that goes on\u0085" + placeholderCell + "\ue000", Engine: true, AgentDriven: true}
	got := browserCard(state, store.Session{}, withFacts(defaultCardKeys, true, false, true), 12, 3)
	if len(got) != 3 {
		t.Fatalf("a three row box got %d lines: %q", len(got), got)
	}
	for _, line := range got {
		if width := ansi.StringWidth(line); width > 12 {
			t.Fatalf("line %q is %d cells wide, past 12", line, width)
		}
		if strings.ContainsAny(line, "\x1b\x07\u0085\ue000"+placeholderCell) {
			t.Fatalf("line %q carries a control or private use character from the page", line)
		}
	}
}

func TestBrowserCardNamesOuterTmux(t *testing.T) {
	sess := store.Session{ID: "docs", Tool: "browser"}
	for _, state := range []struct {
		state    browserState
		running  bool
		starting bool
	}{
		{browserState{URL: "https://example.org/", Title: "Example", Engine: true}, true, false},
		{browserState{}, true, false},
		{browserState{}, true, true},
		{browserState{}, false, false},
	} {
		got := browserCard(state.state, sess, withFacts(defaultCardKeys, state.running, state.starting, true), 80, 10)
		if len(got) == 0 || got[len(got)-1] != outerTmuxLine {
			t.Fatalf("inside tmux the card should end with %q, got %q", outerTmuxLine, got)
		}
		if outside := browserCard(state.state, sess, withFacts(defaultCardKeys, state.running, state.starting, false), 80, 10); slices.Contains(outside, outerTmuxLine) {
			t.Fatalf("outside tmux the card should not name it: %q", outside)
		}
	}
}

// A browser opened a moment ago has no engine listed yet, which is the engine
// booting rather than failing.
func TestBrowserCardSaysStartingForAYoungPane(t *testing.T) {
	m := browserRowModel(t)
	m.workspace.browsers = nil
	young := store.Session{ID: "docs", Tool: "browser", Status: status.Idle, CreatedAt: time.Now()}
	old := young
	old.CreatedAt = time.Now().Add(-time.Minute)
	booting := old
	booting.Status = status.Starting
	for _, test := range []struct {
		sess store.Session
		want string
	}{
		{young, "starting the browser"},
		{booting, "starting the browser"},
		{old, "browser engine did not start, press A for details"},
	} {
		lines := m.browserCardLines(test.sess, 80, 10, "  ")
		if len(lines) == 0 || !strings.Contains(ansi.Strip(lines[0].Text), test.want) {
			t.Fatalf("created %s ago with status %s: card %+v, want %q", time.Since(test.sess.CreatedAt).Round(time.Second), test.sess.Status, lines, test.want)
		}
	}
}

// browserRowModel is a model built without tmux whose cursor sits on a live
// browser row beside an agent, with the browser pane's capture as preview.
func browserRowModel(t *testing.T) *Model {
	t.Helper()
	now := time.Now()
	sessions := []store.Session{
		{ID: "coder", Name: "coder", Tool: "claude", Status: status.Idle, CreatedAt: now, LastStatusAt: now},
		{ID: "docs", Name: "docs", Tool: "browser", Status: status.Idle, CreatedAt: now, LastStatusAt: now, BrowserURL: "https://example.org/docs"},
	}
	m := &Model{
		effects: effectState{Queue: uieffects.Queue{Lifetime: &uieffects.Lifetime{}}},
		layout: layoutState{
			width:  100,
			height: 34,
			split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
		},
		services: services{
			cfg:      config.Config{CLIs: map[string]config.CLI{"claude": {}}, Tools: map[string]config.Tool{"browser": {}}},
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		workspace: workspace{
			sessions: sessions,
			preview:  placeholderPane,
			procFor:  "docs",
			browsers: map[string]browserState{"docs": {URL: "https://example.org/docs", Title: "Example Docs", Engine: true}},
		},
		rail: railModelFromRows([]treeRow{{sess: sessions[0]}, {sess: sessions[1]}}, 1),
	}
	if sess, ok := m.selected(); !ok || sess.ID != "docs" {
		t.Fatalf("test setup: selected %+v", sess)
	}
	return m
}

func TestPreviewPaintsTheCardForBrowserRows(t *testing.T) {
	m := browserRowModel(t)
	assertCard := func(where, painted string) {
		t.Helper()
		if strings.Contains(painted, placeholderCell) {
			t.Fatalf("%s painted placeholder cells", where)
		}
		plain := ansi.Strip(painted)
		for _, want := range []string{"Example Docs", "https://example.org/docs", "press A to see and use the page"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%s is missing %q:\n%s", where, want, plain)
			}
		}
	}
	assertCard("the list preview", contentText(m))
	assertCard("the list frame", preparedView(m))

	m.push(newFocusSurface(m))
	assertCard("the focused preview", contentText(m))
	assertCard("the focused frame", preparedView(m))
	m.prefs.fullLayout = true
	assertCard("the full screen focus frame", m.viewFullFocusFrame())
}
