package presentation

// ChromeSet keeps fixed-width decorations separate from ASCII row marks.
// Compatible retains the thin rules and full block verified in the font audit.
type ChromeSet struct {
	ChoiceLeftGlyph, ChoiceRightGlyph                                       string
	TopLeftGlyph, TopRightGlyph, BottomLeftGlyph, BottomRightGlyph          string
	RuleGlyph, DashedRuleGlyph, ScrollThumbGlyph, SectionGlyph, CursorGlyph string
	ReviewSelectionGlyph, QuickEdgeGlyph, ResizeGlyph                       string
}

func ChromeFor(mode GlyphMode) ChromeSet {
	if mode.Effective() == GlyphModeAdvanced {
		return ChromeSet{
			ChoiceLeftGlyph: "◂", ChoiceRightGlyph: "▸", TopLeftGlyph: "╭", TopRightGlyph: "╮", BottomLeftGlyph: "╰", BottomRightGlyph: "╯",
			RuleGlyph: "━", DashedRuleGlyph: "┄", ScrollThumbGlyph: "┃", SectionGlyph: "▍", CursorGlyph: "▏",
			ReviewSelectionGlyph: "▎", QuickEdgeGlyph: "▌", ResizeGlyph: "║",
		}
	}
	return ChromeSet{
		ChoiceLeftGlyph: "<", ChoiceRightGlyph: ">", TopLeftGlyph: "+", TopRightGlyph: "+", BottomLeftGlyph: "+", BottomRightGlyph: "+",
		RuleGlyph: "─", DashedRuleGlyph: "─", ScrollThumbGlyph: "█", SectionGlyph: "│", CursorGlyph: "│",
		ReviewSelectionGlyph: "│", QuickEdgeGlyph: "│", ResizeGlyph: "│",
	}
}
