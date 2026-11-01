package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

type browserState struct {
	URL, Title  string
	Engine      bool
	AgentDriven bool
}

// browserCardFacts is what the card needs beyond the engine read: the pane's
// liveness, whether its engine may still be booting, the outer tmux, and the
// list keys it names. An empty key leaves its offer off the card.
type browserCardFacts struct {
	running, starting, insideTmux bool
	attachKey, reviveKey          string
}

const (
	browserCardDriven    = "agent is driving it"
	browserCardStarting  = "starting the browser"
	browserCardNoEngine  = "browser engine did not start"
	browserCardClosed    = "browser closed"
	browserCardOuterTmux = "run agent-manager outside tmux to see browser pages"
)

// browserCard is what a browser row's preview shows in place of its pane,
// which captures as image placeholder cells only the outer terminal can draw.
func browserCard(state browserState, sess store.Session, facts browserCardFacts, width, height int) []string {
	var lines []string
	switch {
	case !facts.running:
		lines = []string{withKey(browserCardClosed, facts.reviveKey, ", ", " revives it")}
	case !state.Engine && facts.starting:
		lines = []string{browserCardStarting}
	case !state.Engine:
		lines = []string{withKey(browserCardNoEngine, facts.attachKey, ", press ", " for details")}
	default:
		url := termbrowser.PageText(state.URL)
		if url == "" {
			url = termbrowser.PageText(sess.BrowserURL)
		}
		if title := termbrowser.PageText(state.Title); title != "" {
			lines = append(lines, title)
		}
		lines = append(lines, url)
		if facts.attachKey != "" {
			lines = append(lines, "press "+facts.attachKey+" to see and use the page")
		}
		if state.AgentDriven {
			lines = append(lines, browserCardDriven)
		}
	}
	if facts.insideTmux {
		lines = append(lines, browserCardOuterTmux)
	}
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(width, 0), "…")
	}
	return lines
}

func withKey(text, key, before, after string) string {
	if key == "" {
		return text
	}
	return text + before + key + after
}

// browserCardLines matches the tone and gutter of the preview's "(no output yet)".
// A pane younger than one poll, or one that has not painted yet, has had no
// engine read that could list it, so a missing engine there is still booting.
func (m *Model) browserCardLines(sess store.Session, width, height int, gutter string) []contentLine {
	facts := browserCardFacts{
		running:    !sess.Archived && sess.Status != status.Dead,
		starting:   sess.Status == status.Starting || time.Since(sess.LaunchTime()) < config.PollInterval,
		insideTmux: m.env.insideTmux,
		attachKey:  m.listGlyph(keybind.Attach),
		reviveKey:  m.listGlyph(keybind.Revive),
	}
	card := browserCard(m.workspace.browsers[sess.ID], sess, facts, width-2*len(gutter), height)
	lines := make([]contentLine, len(card))
	for i, line := range card {
		lines[i] = contentLine{Text: gutter + uitheme.MutedStyle().Render(line)}
	}
	return lines
}
