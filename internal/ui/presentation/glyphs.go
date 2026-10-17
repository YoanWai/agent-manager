package presentation

type GlyphMode string

const (
	GlyphModeBasic    GlyphMode = "basic"
	GlyphModeAdvanced GlyphMode = "advanced"
	// An unset preference follows this default; explicit choices survive a change.
	DefaultGlyphMode = GlyphModeAdvanced
	GlyphModeSetting = "glyph_mode"
)

// Row marks share a one-cell column. ASCII keeps that geometry without
// relying on font fallback.
const (
	workingGlyph  = "*"
	startingGlyph = "~"
	waitingGlyph  = "?"
	finishedGlyph = "+"
	erroredGlyph  = "x"
	idleGlyph     = "."

	reorderGrip      = "="
	inboxGlyph       = "@"
	branchGlyph      = "Y"
	groupOpenGlyph   = "v"
	groupClosedGlyph = ">"
)

var startupFrames = [...]string{"|", "/", "-", "\\"}

var advancedStartupFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (g GlyphSet) StatusGlyph(s string) string {
	switch s {
	case "working":
		return g.WorkingGlyph
	case "starting":
		return g.StartingGlyph
	case "waiting":
		return g.WaitingGlyph
	case "finished":
		return g.FinishedGlyph
	case "errored", "dead":
		return g.ErroredGlyph
	default:
		return g.IdleGlyph
	}
}

type GlyphSet struct {
	WorkingGlyph, StartingGlyph, WaitingGlyph, FinishedGlyph, ErroredGlyph, IdleGlyph string
	ReorderGrip, InboxGlyph, BranchGlyph, GroupOpenGlyph, GroupClosedGlyph            string
	ShellGlyph, LoaderHead, LoaderTail, LoaderRest                                    string
	StartupFrames                                                                     []string
}

func GlyphsFor(mode GlyphMode) GlyphSet {
	if mode.Effective() == GlyphModeAdvanced {
		return GlyphSet{
			WorkingGlyph: "◐", StartingGlyph: "◌", WaitingGlyph: "◆", FinishedGlyph: "●", ErroredGlyph: "✕", IdleGlyph: "○",
			ReorderGrip: "⠿", InboxGlyph: "✉", BranchGlyph: "⑂", GroupOpenGlyph: "▾", GroupClosedGlyph: "▸",
			ShellGlyph: "❯", LoaderHead: "●", LoaderTail: "•", LoaderRest: "·",
			StartupFrames: advancedStartupFrames[:],
		}
	}
	return GlyphSet{
		WorkingGlyph: workingGlyph, StartingGlyph: startingGlyph, WaitingGlyph: waitingGlyph,
		FinishedGlyph: finishedGlyph, ErroredGlyph: erroredGlyph, IdleGlyph: idleGlyph,
		ReorderGrip: reorderGrip, InboxGlyph: inboxGlyph, BranchGlyph: branchGlyph,
		GroupOpenGlyph: groupOpenGlyph, GroupClosedGlyph: groupClosedGlyph,
		ShellGlyph: ">", LoaderHead: "*", LoaderTail: ":", LoaderRest: ".", StartupFrames: startupFrames[:],
	}
}

func (mode GlyphMode) Effective() GlyphMode {
	if mode == "" {
		return DefaultGlyphMode
	}
	return mode
}

func (g GlyphSet) StartupFrame(phase int) string {
	return g.StartupFrames[phase%len(g.StartupFrames)]
}
