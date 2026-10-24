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
	PromptGlyph, ReplyGlyph, SearchGlyph, SelectionGlyph, NoticeSelectionGlyph                    string
	SettingsGlyph, NewSessionGlyph, NewGlyph, ArchiveGlyph, RestoreGlyph                          string
	RestartGlyph, DeleteGlyph, MoveGlyph, ResetGlyph, ReviewGlyph, SetupGlyph                     string
	FeedGlyph, WelcomeGlyph, BetaGlyph, UpdateGlyph, DownloadGlyph, RefreshGlyph, LinkGlyph       string
	WarningGlyph, CardErrorGlyph, DiffErrorGlyph, DiffCleanGlyph, DiffDoneGlyph, DiffPendingGlyph string
	WorktreeGlyph, AfterTurnArchiveGlyph, AfterTurnKillGlyph                                      string
	WorkingGlyph, StartingGlyph, WaitingGlyph, FinishedGlyph, ErroredGlyph, IdleGlyph             string
	ReorderGrip, InboxGlyph, BranchGlyph, GroupOpenGlyph, GroupClosedGlyph                        string
	ShellGlyph, LoaderHead, LoaderTail, LoaderRest                                                string
	StartupFrames                                                                                 []string
}

func GlyphsFor(mode GlyphMode) GlyphSet {
	if mode.Effective() == GlyphModeAdvanced {
		return GlyphSet{
			WorkingGlyph: "◐", StartingGlyph: "◌", WaitingGlyph: "◆", FinishedGlyph: "●", ErroredGlyph: "✕", IdleGlyph: "○",
			ReorderGrip: "⠿", InboxGlyph: "✉", BranchGlyph: "⑂", GroupOpenGlyph: "▾", GroupClosedGlyph: "▸",
			ShellGlyph: "❯", LoaderHead: "●", LoaderTail: "•", LoaderRest: "·",
			PromptGlyph: "❯", ReplyGlyph: "↳", SearchGlyph: "⌕", SelectionGlyph: "❯", NoticeSelectionGlyph: "▸",
			SettingsGlyph: "⚙", NewSessionGlyph: "◆", NewGlyph: "✦", ArchiveGlyph: "◇", RestoreGlyph: "◆",
			RestartGlyph: "↻", DeleteGlyph: "⚠", MoveGlyph: "⇄", ResetGlyph: "↺", ReviewGlyph: "⌥", SetupGlyph: "◈",
			FeedGlyph: "◆", WelcomeGlyph: "✳", BetaGlyph: "↔", UpdateGlyph: "↑", DownloadGlyph: "↓", RefreshGlyph: "↻", LinkGlyph: "↗",
			WarningGlyph: "▲", CardErrorGlyph: "⚠", DiffErrorGlyph: "✖", DiffCleanGlyph: "✓", DiffDoneGlyph: "✔", DiffPendingGlyph: "○",
			WorktreeGlyph: "⎇", AfterTurnArchiveGlyph: "↓", AfterTurnKillGlyph: "■",
			StartupFrames: advancedStartupFrames[:],
		}
	}
	return GlyphSet{
		WorkingGlyph: workingGlyph, StartingGlyph: startingGlyph, WaitingGlyph: waitingGlyph,
		FinishedGlyph: finishedGlyph, ErroredGlyph: erroredGlyph, IdleGlyph: idleGlyph,
		ReorderGrip: reorderGrip, InboxGlyph: inboxGlyph, BranchGlyph: branchGlyph,
		GroupOpenGlyph: groupOpenGlyph, GroupClosedGlyph: groupClosedGlyph,
		PromptGlyph: ">", ReplyGlyph: "<", SearchGlyph: "/", SelectionGlyph: ">", NoticeSelectionGlyph: ">",
		SettingsGlyph: "#", NewSessionGlyph: "+", NewGlyph: "+", ArchiveGlyph: "A", RestoreGlyph: "R",
		RestartGlyph: "R", DeleteGlyph: "!", MoveGlyph: "M", ResetGlyph: "R", ReviewGlyph: "D", SetupGlyph: "!",
		FeedGlyph: "i", WelcomeGlyph: "i", BetaGlyph: "B", UpdateGlyph: "^", DownloadGlyph: "v", RefreshGlyph: "R", LinkGlyph: "@",
		WarningGlyph: "!", CardErrorGlyph: "x", DiffErrorGlyph: "x", DiffCleanGlyph: "+", DiffDoneGlyph: "+", DiffPendingGlyph: ".",
		WorktreeGlyph: branchGlyph, AfterTurnArchiveGlyph: "A", AfterTurnKillGlyph: "K",
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
