package focus

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Facts caps the columns a deep path takes and the least space kept between
// the session's name and the readings beside it.
const (
	FactsDirCap = 40
	FactsGap    = 2
)

type FactsSession struct {
	Glyph    string
	Name     string
	Tool     string
	Status   string
	Activity string
	Dir      string
	Branch   string
	Started  string
	Queued   int
	// CPU and RAM are the session's process readings, empty when the poller
	// has none for it.
	CPU, RAM string
}

// FactsHost is what the line reads before it formats anything: the state word
// and its tone, which the root owns.
type FactsHost interface {
	StatusLabel(status string) string
	StatusColor(status string) string
}

// FactsLine says which session a full screen frame is showing, where nothing
// else on it does: the state dot and the name on the left, then where it runs
// and what it costs against the right edge, with the whole width to itself.
func FactsLine(width int, sess FactsSession, h FactsHost) string {
	sep := uitheme.SubtleStyle().Render(" · ")
	left := " " + sess.Glyph + " " + uitheme.ValueStyle().Render(sess.Name) +
		sep + uitheme.ValueStyle().Render(sess.Tool) +
		sep + lipgloss.NewStyle().Foreground(lipgloss.Color(h.StatusColor(sess.Status))).Render(h.StatusLabel(sess.Status)) +
		sep + uitheme.SubtleStyle().Render(sess.Activity)
	facts := []Fact{{Text: uitheme.ValueStyle().Render(presentation.TruncateTail(sess.Dir, FactsDirCap)), Spare: 3}}
	if sess.Branch != "" {
		facts = append(facts, Fact{Text: uitheme.SubtleStyle().Render(sess.Branch), Spare: 2})
	}
	if sess.CPU != "" {
		facts = append(facts, Fact{Text: uitheme.LabelStyle().Render("cpu ") + uitheme.ValueStyle().Render(sess.CPU), Spare: 1})
	}
	if sess.RAM != "" {
		facts = append(facts, Fact{Text: uitheme.LabelStyle().Render("ram ") + uitheme.ValueStyle().Render(sess.RAM), Spare: 1})
	}
	facts = append(facts, Fact{Text: uitheme.LabelStyle().Render("started ") + uitheme.ValueStyle().Render(sess.Started), Spare: 4})
	if sess.Queued > 0 {
		facts = append(facts, Fact{Text: uitheme.ValueStyle().Render(fmt.Sprintf("%d queued", sess.Queued)), Spare: 0})
	}
	right := FitFacts(facts, sep, width-ansi.StringWidth(left)-FactsGap)
	if ansi.StringWidth(left) > width {
		return ansi.Truncate(left, max(width, 0), "…")
	}
	return uiframe.RowColumns(left, right, width)
}

// Edge is the hairline holding the full screen pane off what sits above and
// below it, in the pane's own tone once it has a box to trace.
func Edge(width int, framed bool) string {
	if framed {
		return uitheme.FocusEdgeStyle().Render(strings.Repeat("─", max(width, 0)))
	}
	return uitheme.Hrule(width)
}

// RuleKeysFor is the focused split's hairline title: the manager's own keys
// as the live table names them, read off a bound table that may be nil.
func RuleKeysFor(keys keybind.Table) RuleKeys {
	return RuleKeys{
		Back:   keys.Binding(keybind.Detach).Label(),
		Review: keys.Binding(keybind.Review).Label(),
		Editor: keys.Binding(keybind.Editor).Label(),
	}
}

// TopRuleFor is the hairline that caps the focused pane in the split.
func TopRuleFor(width int, keys keybind.Table) string {
	return TopRule(width, RuleKeysFor(keys), uitheme.AnnotationStyle(), uitheme.FocusEdgeStyle())
}
