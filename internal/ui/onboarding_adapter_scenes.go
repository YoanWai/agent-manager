package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// setupBeat is one tick of a guide step acting itself out.
const setupBeat = 90 * time.Millisecond

type setupTickMsg struct{}

// keepSetupAnimating starts the ticks a guide step's animation runs on,
// once, for as long as such a step is on screen.
func (m *Model) keepSetupAnimating() tea.Cmd {
	setup := &m.startup.setup
	if m.mode != modeOnboarding || !setup.Animating() || setup.ticking {
		return nil
	}
	setup.ticking = true
	return tea.Tick(setupBeat, func(time.Time) tea.Msg { return setupTickMsg{} })
}

func (m *Model) applySetupTick() tea.Cmd {
	setup := &m.startup.setup
	setup.ticking = false
	if m.mode != modeOnboarding || !setup.Animating() {
		return nil
	}
	setup.Tick()
	return m.keepSetupAnimating()
}

const (
	sampleAnswer  = "yes, go ahead"
	sampleFocus   = "also cover the logout path"
	sampleComment = "keep the #hash on the way back"
)

// setupScene is the window's frame for the open step: the list as the
// choices draw it, and on a guide step the beat of the scene it acts out.
func (m *Model) setupScene(p uionboarding.Preview, width, height int) previewScene {
	scene := previewScene{
		sessions: previewSessions(p.Agents),
		groups:   []string{"web", "api"},
		selected: sessionSelection(previewLogin, "web"),
	}
	scene.content = m.loginTranscript()
	switch p.Step {
	case uionboarding.Welcome:
		welcomeScene(&scene, p)
	case uionboarding.QuickPrompt:
		m.quickPromptScene(&scene, p, width)
	case uionboarding.Sessions:
		m.sessionsScene(&scene, p, width, height)
	case uionboarding.Review:
		m.reviewScene(&scene, p, width, height)
	case uionboarding.Orchestrate:
		m.orchestrateScene(&scene, p, width)
	case uionboarding.Places:
		scene.foot = []string{"", m.previewLegend()}
	}
	return scene
}

// welcomeScene: the agents arrive in the list one after another, each
// group showing up with its first agent.
func welcomeScene(scene *previewScene, p uionboarding.Preview) {
	const arrival = 8
	shown := min(1+p.Frame/arrival, len(scene.sessions))
	scene.sessions, scene.groups = scene.sessions[:shown], nil
}

func sessionSelection(id, group string) uirail.Selection {
	return uirail.Selection{Kind: uirail.SessionRow, SessionID: id, Group: group}
}

// quickTasks are the agents the quick prompt starts in the web group, one
// after another.
var quickTasks = []struct{ name, task, line string }{
	{"flaky checkout", "fix the flaky checkout test", "Reading tests/checkout.spec.ts"},
	{"coupon field", "add a coupon field", "Adding the coupon input"},
	{"cart query", "speed up the cart query", "Profiling the cart query"},
}

// quickTyping is the characters typed per beat, quick enough that three
// agents start inside one loop.
const quickTyping = 3

// quickPromptScene: space on a group opens the bar, then each task is
// typed and enter starts its agent in the group, three times over.
func (m *Model) quickPromptScene(scene *previewScene, p uionboarding.Preview, width int) {
	scene.selected = uirail.Selection{Kind: uirail.GroupRow, Group: "web"}
	target := lipgloss.NewStyle().Foreground(colorAccent2()).Render("web")
	launch := valueStyle().Render(p.Agents[0]) + subtleStyle().Render(" · "+m.glyphs().WorktreeGlyph+" off ")
	// Scene 1, 3 and 5 type a task; 2, 4 and 6 start it.
	typing, started := (p.Scene-1)/2, p.Scene/2
	typed := ""
	if p.Scene%2 == 1 {
		typed = typedOut(quickTasks[typing].task, p.Frame*quickTyping)
	}
	if p.Scene > 0 || p.Frame >= 3 {
		scene.foot = m.previewBar(width, "new", target, launch, typed)
	}
	now := time.Now()
	for i := range started {
		quick := quickTasks[i]
		agent := uirail.Session{ID: "preview-quick-" + quick.name, Name: quick.name, Group: "web", Tool: p.Agents[i%len(p.Agents)],
			Status: status.Working, Prompt: quick.task, PaneLine: quick.line, CreatedAt: now, LastStatusAt: now}
		if i == started-1 && p.Scene%2 == 0 && p.Frame < 4 {
			agent.Status, agent.PaneLine = status.Starting, ""
		}
		scene.sessions = append(scene.sessions, agent)
	}
	scene.content = m.groupSummary(2 + started)
}

// sessionsScene: answer a waiting agent from the bar, focus one and type
// into it, and come back to the list.
func (m *Model) sessionsScene(scene *previewScene, p uionboarding.Preview, width, height int) {
	switch p.Scene {
	case 0:
		scene.selected = sessionSelection(previewDark, "web")
		answered := p.Frame >= 22
		scene.content = m.darkModeTranscript(answered)
		target := lipgloss.NewStyle().Foreground(colorBright()).Render("dark mode")
		typed := typedOut(sampleAnswer, p.Frame-4)
		if answered {
			typed = ""
			setStatus(scene.sessions, previewDark, status.Working, "Editing src/theme.css")
		}
		scene.foot = m.previewBar(width, "answer", target, "", typed)
	case 1:
		scene.focused = true
		scene.content = append(m.loginTranscript(), contentLine{}, m.promptLine(typedOut(sampleFocus, p.Frame-4), true))
		if p.Choices.FullLayout {
			scene.body = m.paneBody(scene.content, width, height)
		}
	}
}

// reviewScene: Review opens on the agent's diff and a comment is written on
// a line. Sending it puts the agent to work on it in the background while
// the review goes on to the next file.
func (m *Model) reviewScene(scene *previewScene, p uionboarding.Preview, width, height int) {
	switch p.Scene {
	case 0:
		scene.body = m.previewReview(width, height, "", false)
	case 1:
		scene.body = m.previewReview(width, height, typedOut(sampleComment, p.Frame-4), false)
	default:
		setStatus(scene.sessions, previewLogin, status.Working, "Keeping the #hash in the redirect")
		scene.body = m.previewReview(width, height, sampleComment, p.Frame >= 8)
	}
}

func setStatus(sessions []uirail.Session, id, state, line string) {
	for i := range sessions {
		if sessions[i].ID == id {
			sessions[i].Status, sessions[i].PaneLine = state, line
		}
	}
}

// paneBody is an agent's pane over the whole window, the way focus takes
// the body in the full layout.
func (m *Model) paneBody(lines []contentLine, width, height int) []string {
	out := make([]string, 0, height)
	for _, line := range m.fitTranscript(lines, width, height) {
		out = append(out, paint(line.text, width, backdropHex()))
	}
	for len(out) < height {
		out = append(out, paint("", width, backdropHex()))
	}
	return out
}

func (m *Model) groupSummary(agents int) []contentLine {
	pad := strings.Repeat(" ", contentGutter)
	return []contentLine{
		{},
		{text: pad + lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true).Render("web")},
		{text: pad + mutedStyle().Render(fmt.Sprintf("%d agents · %d working · 1 waiting", agents, agents-1))},
	}
}

// The transcript lines, in the shapes an agent's pane shows them.
func (m *Model) promptLine(text string, typing bool) contentLine {
	line := lipgloss.NewStyle().Foreground(colorAccent()).Render(m.glyphs().PromptGlyph) + " " + lipgloss.NewStyle().Foreground(colorBright()).Render(text)
	if typing {
		line += lipgloss.NewStyle().Foreground(colorAccent()).Render(m.chrome().CursorGlyph)
	}
	return contentLine{text: strings.Repeat(" ", contentGutter) + line}
}

func (m *Model) stepLine(text string) contentLine {
	return contentLine{text: strings.Repeat(" ", contentGutter) + lipgloss.NewStyle().Foreground(colorFinished()).Render(m.glyphs().FinishedGlyph) + " " + valueStyle().Render(text)}
}

func (m *Model) noteLine(text string) contentLine {
	return contentLine{text: strings.Repeat(" ", contentGutter+2) + valueStyle().Render(text)}
}

func (m *Model) stateLine(state, text string) contentLine {
	return contentLine{text: strings.Repeat(" ", contentGutter) + lipgloss.NewStyle().Foreground(statusColor(state)).Render(m.glyphs().StatusGlyph(state)+" "+text)}
}

func (m *Model) loginTranscript() []contentLine {
	return []contentLine{
		{},
		m.promptLine("fix the login redirect loop", false),
		{},
		m.stepLine("Reading src/auth/redirect.ts"),
		m.stepLine("The callback adds returnTo twice."),
		m.noteLine("Patching it, then the tests."),
		{},
		m.stateLine(status.Working, "Running tests…"),
	}
}

func (m *Model) darkModeTranscript(answered bool) []contentLine {
	lines := []contentLine{
		{},
		m.promptLine("add a dark mode toggle", false),
		{},
		m.stepLine("The toggle needs new tokens in theme.css."),
		{},
		m.stateLine(status.Waiting, "Allow edits to theme.css?"),
	}
	if answered {
		lines = append(lines, contentLine{}, m.promptLine(sampleAnswer, false), contentLine{}, m.stateLine(status.Working, "Editing src/theme.css…"))
	}
	return lines
}
