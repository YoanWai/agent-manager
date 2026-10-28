// Package onboarding is the first-run setup: a few choices, each shown
// live, then a short guide to the keys that matter. It receives copied facts
// and returns what to save. The root writes the settings, applies the
// palette and draws the live preview.
package onboarding

import (
	"maps"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// Step is one screen of the setup.
type Step uint8

const (
	Welcome Step = iota
	Agents
	Layout
	Density
	Theme
	QuickPrompt
	Sessions
	Review
	Orchestrate
	Places
)

// Steps is the walk, in order. A step a later release adds, such as
// importing existing sessions, takes its place in this list.
var Steps = []Step{Welcome, Agents, Layout, Density, Theme, QuickPrompt, Sessions, Review, Orchestrate, Places}

// choosing reports a step that sets a preference. The others teach.
func (s Step) choosing() bool {
	switch s {
	case Agents, Layout, Density, Theme:
		return true
	}
	return false
}

// Choices are the preferences the setup sets, in the terms of the
// Settings keys they are saved under.
type Choices struct {
	Hidden      map[string]bool
	FullLayout  bool
	Comfortable bool
	Theme       string
	ThemeAuto   bool
}

func (c Choices) clone() Choices {
	c.Hidden = maps.Clone(c.Hidden)
	return c
}

// Agent is one agent CLI the first step lists.
type Agent struct {
	Name      string
	Installed bool
}

type Action uint8

const (
	Stay Action = iota
	// Save writes Outcome.Choices: every step confirmed so far.
	Save
	// Close ends the setup. Whatever was saved stays.
	Close
)

type Outcome struct {
	Action  Action
	Choices Choices
}

type Model struct {
	steps  []Step
	at     int
	agents []Agent
	probed bool
	// preselect checks the installed agents when the probe lands, unless an
	// agent was toggled first.
	preselect, touched bool
	// chosen is what the screen shows, and saved is what the confirmed
	// steps wrote and what skipping keeps.
	chosen, saved           Choices
	agentCursor, agentFirst int
	// themeCursor is the theme step's palette row, from 1: row 0 is the
	// follow-the-OS toggle, which space and a click reach without moving
	// through the palettes.
	themeCursor, themeFirst int
	notice                  string
	done                    bool
	lines                   presentation.Lines[int]
	// beat counts the animation ticks since the open step came on screen.
	beat int
	// skipping is a first esc waiting for the second that skips the setup.
	skipping bool
}

// New opens the setup on the current choices. A first run checks the
// agents found on PATH once the root reports them.
func New(agents []string, current Choices, firstRun bool) Model {
	m := Model{steps: Steps, preselect: firstRun, chosen: current.clone(), saved: current.clone()}
	for _, name := range agents {
		m.agents = append(m.agents, Agent{Name: name})
	}
	if len(m.Enabled()) == 0 {
		m.chosen.Hidden = nil
	}
	m.themeCursor = uitheme.Index(current.Theme) + 1
	return m
}

func (m Model) Step() Step { return m.steps[m.at] }

// Shown is the choice the frame should be painted in: the theme under the
// cursor while the theme step is open, otherwise what was saved.
func (m Model) Shown() Choices {
	if !m.done && m.Step() == Theme {
		return m.chosen.clone()
	}
	return m.saved.clone()
}

// Enabled is the agents left checked, in picker order.
func (m Model) Enabled() []string {
	var names []string
	for _, agent := range m.agents {
		if !m.chosen.Hidden[agent.Name] {
			names = append(names, agent.Name)
		}
	}
	return names
}

// SetInstalled records which agents are on PATH. On a first run it checks
// those and unchecks the rest, when at least one was found.
func (m *Model) SetInstalled(installed map[string]bool) {
	m.probed = true
	found := false
	for i := range m.agents {
		m.agents[i].Installed = installed[m.agents[i].Name]
		found = found || m.agents[i].Installed
	}
	if !m.preselect || m.touched || !found {
		return
	}
	hidden := make(map[string]bool)
	for _, agent := range m.agents {
		if !agent.Installed {
			hidden[agent.Name] = true
		}
	}
	m.chosen.Hidden = hidden
}

func (m *Model) Key(msg tea.KeyMsg) Outcome {
	m.notice = ""
	if m.skipping {
		m.skipping = false
		if msg.String() == "esc" {
			return m.close()
		}
		return Outcome{}
	}
	switch msg.String() {
	case "esc":
		m.skipping = !m.Step().choosing()
	case "enter", "right", "tab":
		return m.next()
	case "backspace", "left", "shift+tab":
		if m.at > 0 {
			m.at--
			m.beat = 0
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case " ", "space":
		m.toggle()
	}
	return Outcome{}
}

// Click acts on the option painted on line of the last frame.
func (m *Model) Click(line int) Outcome {
	row, ok := m.lines.At(line)
	if !ok {
		return Outcome{}
	}
	m.notice, m.skipping = "", false
	switch m.Step() {
	case Agents:
		m.agentCursor = row
		m.toggle()
	case Layout:
		m.chosen.FullLayout = row == 1
	case Density:
		m.chosen.Comfortable = row == 1
	case Theme:
		if row == 0 {
			m.toggle()
		} else {
			m.pickTheme(row)
		}
	}
	return Outcome{}
}

// Wheel moves through the options the way ↑↓ do.
func (m *Model) Wheel(step int) Outcome {
	m.move(step)
	return Outcome{}
}

func (m *Model) close() Outcome {
	m.done = true
	return Outcome{Action: Close, Choices: m.saved.clone()}
}

func (m *Model) next() Outcome {
	if m.at == len(m.steps)-1 {
		return m.close()
	}
	out := Outcome{}
	if m.Step().choosing() {
		m.confirm()
		out = Outcome{Action: Save, Choices: m.saved.clone()}
	}
	m.at++
	m.beat = 0
	return out
}

func (m *Model) confirm() {
	switch m.Step() {
	case Agents:
		m.saved.Hidden = maps.Clone(m.chosen.Hidden)
		m.preselect = false
	case Layout:
		m.saved.FullLayout = m.chosen.FullLayout
	case Density:
		m.saved.Comfortable = m.chosen.Comfortable
	case Theme:
		m.saved.Theme, m.saved.ThemeAuto = m.chosen.Theme, m.chosen.ThemeAuto
	}
}

func (m *Model) move(delta int) {
	switch m.Step() {
	case Agents:
		if count := len(m.agents); count > 0 {
			m.agentCursor = (m.agentCursor + delta + count) % count
		}
	case Layout:
		m.chosen.FullLayout = delta > 0
	case Density:
		m.chosen.Comfortable = delta > 0
	case Theme:
		count := len(uitheme.Palettes)
		m.pickTheme((m.themeCursor-1+delta+count)%count + 1)
	}
}

// pickTheme chooses a palette by hand, which turns following the OS off,
// as stepping the theme in Settings does.
func (m *Model) pickTheme(row int) {
	m.themeCursor = row
	m.chosen.Theme = uitheme.Palettes[row-1].Name
	m.chosen.ThemeAuto = false
}

func (m *Model) toggle() {
	switch m.Step() {
	case Agents:
		if len(m.agents) > 0 {
			m.toggleAgent(m.agents[m.agentCursor].Name)
		}
	case Theme:
		m.chosen.ThemeAuto = !m.chosen.ThemeAuto
	}
}

// toggleAgent checks or unchecks one agent. One stays checked, so a new
// session always has something to launch.
func (m *Model) toggleAgent(name string) {
	m.touched = true
	if m.chosen.Hidden[name] {
		delete(m.chosen.Hidden, name)
		return
	}
	if len(m.Enabled()) <= 1 {
		m.notice = "Keep at least one agent."
		return
	}
	if m.chosen.Hidden == nil {
		m.chosen.Hidden = make(map[string]bool)
	}
	m.chosen.Hidden[name] = true
}

// Hints are the footer's keys for the open step. Only the welcome offers
// a skip. The choices are required, and the tour takes an esc it does not
// name.
func (m Model) Hints() [][2]string {
	if m.skipping {
		return [][2]string{{"esc", "skip setup"}, {"↵", "keep going"}}
	}
	step := m.Step()
	var hints [][2]string
	switch {
	case step == Agents:
		hints = [][2]string{{"↑↓", "move"}, {"space", "check"}}
	case step == Theme:
		hints = [][2]string{{"↑↓", "theme"}, {"space", "follow OS"}}
	case step.choosing():
		hints = [][2]string{{"↑↓", "choose"}}
	}
	switch {
	case step == Welcome:
		hints = append(hints, [2]string{"↵", "set it up"})
	case m.at == len(m.steps)-1:
		hints = append(hints, [2]string{"↵", "start"})
	default:
		hints = append(hints, [2]string{"↵", "next"})
	}
	if m.at > 0 {
		hints = append(hints, [2]string{"⌫", "back"})
	}
	if step == Welcome {
		hints = append(hints, [2]string{"esc", "skip setup"})
	}
	return hints
}
