package ui

import (
	"github.com/YoanWai/agent-manager/internal/termseq"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Theme and the palettes live in ui/theme so feature packages read the
// same tokens; the root keeps the live styles derived from them.
type Theme = uitheme.Theme

var themes = uitheme.Palettes

func themeIndex(name string) int { return uitheme.Index(name) }

func mix(a, b string, ratio float64) string { return uitheme.Mix(a, b, ratio) }

// applyTheme publishes the styles for a token set; a theme switch repaints
// from the next frame on.
func applyTheme(t Theme) { uitheme.Apply(t) }

// SyncTerminalColors sets the terminal's own text and background colors to
// the theme's (OSC 10 and 11). The frame can only paint its cell grid; any
// window padding the terminal draws around that grid keeps the terminal's
// background, so the two must be the same color for the frame's edges to
// look exact, and text an agent prints in the default color must read on
// that background. Terminals without OSC 10 and 11 ignore them.
func SyncTerminalColors() {
	emitToTerminal("\x1b]10;" + current().Text + "\x07\x1b]11;" + current().Bg + "\x07")
}

// ResetTerminalColors restores the terminal's own text and background colors
// (OSC 110 and 111) when the manager exits.
func ResetTerminalColors() {
	emitToTerminal("\x1b]110\x07\x1b]111\x07")
}

// emitToTerminal sends a control sequence to whatever is actually drawing
// the window. Run under tmux, only the passthrough envelope goes out: a
// plain OSC would make tmux recolor our pane's default background, and a
// recolored pane paints as explicit color while the terminal's padding
// keeps blending its own — the exact ring the backdrop scheme exists to
// avoid. EnableTerminalPassthrough must have opened the envelope first.
func emitToTerminal(seq string) {
	_ = termseq.Emit(seq)
}

// EnableTerminalPassthrough opens the tmux passthrough envelope the backdrop
// sync and the clipboard fallback both write through.
func EnableTerminalPassthrough() {
	termseq.EnablePassthrough()
}

// The style, color and paint vocabulary now lives in ui/theme; these keep
// the root's call sites on their short lowercase names.

func current() Theme { return uitheme.Current() }

func colorBg() lipgloss.Color       { return uitheme.ColorBg() }
func colorSurface() lipgloss.Color  { return uitheme.ColorSurface() }
func colorOverlay() lipgloss.Color  { return uitheme.ColorOverlay() }
func colorBorder() lipgloss.Color   { return uitheme.ColorBorder() }
func colorBright() lipgloss.Color   { return uitheme.ColorBright() }
func colorText() lipgloss.Color     { return uitheme.ColorText() }
func colorDim() lipgloss.Color      { return uitheme.ColorDim() }
func colorSubtle() lipgloss.Color   { return uitheme.ColorSubtle() }
func colorAccent() lipgloss.Color   { return uitheme.ColorAccent() }
func colorAccent2() lipgloss.Color  { return uitheme.ColorAccent2() }
func colorRemote() lipgloss.Color   { return uitheme.ColorRemote() }
func colorSelBg() lipgloss.Color    { return uitheme.ColorSelBg() }
func colorWorking() lipgloss.Color  { return uitheme.ColorWorking() }
func colorWaiting() lipgloss.Color  { return uitheme.ColorWaiting() }
func colorFinished() lipgloss.Color { return uitheme.ColorFinished() }
func colorErrored() lipgloss.Color  { return uitheme.ColorErrored() }
func colorIdle() lipgloss.Color     { return uitheme.ColorIdle() }

func sectionStyle() lipgloss.Style          { return uitheme.SectionStyle() }
func selectedRowStyle() lipgloss.Style      { return uitheme.SelectedRowStyle() }
func mutedStyle() lipgloss.Style            { return uitheme.MutedStyle() }
func subtleStyle() lipgloss.Style           { return uitheme.SubtleStyle() }
func valueStyle() lipgloss.Style            { return uitheme.ValueStyle() }
func labelStyle() lipgloss.Style            { return uitheme.LabelStyle() }
func errStyle() lipgloss.Style              { return uitheme.ErrStyle() }
func doneStyle() lipgloss.Style             { return uitheme.DoneStyle() }
func warnStyle() lipgloss.Style             { return uitheme.WarnStyle() }
func keyStyle() lipgloss.Style              { return uitheme.KeyStyle() }
func annotationStyle() lipgloss.Style       { return uitheme.AnnotationStyle() }
func scopeBadgeStyle() lipgloss.Style       { return uitheme.ScopeBadgeStyle() }
func focusBadgeStyle() lipgloss.Style       { return uitheme.FocusBadgeStyle() }
func inboxBadgeStyle() lipgloss.Style       { return uitheme.InboxBadgeStyle() }
func searchMatchStyle() lipgloss.Style      { return uitheme.SearchMatchStyle() }
func focusEdgeStyle() lipgloss.Style        { return uitheme.FocusEdgeStyle() }
func chipStyle() lipgloss.Style             { return uitheme.ChipStyle() }
func imageChipStyle() lipgloss.Style        { return uitheme.ImageChipStyle() }
func imageChipPastingStyle() lipgloss.Style { return uitheme.ImageChipPastingStyle() }
func legendTitleStyle() lipgloss.Style      { return uitheme.LegendTitleStyle() }
func legendBadgeStyle() lipgloss.Style      { return uitheme.LegendBadgeStyle() }
func legendAlertStyle() lipgloss.Style      { return uitheme.LegendAlertStyle() }
func legendLabelStyle() lipgloss.Style      { return uitheme.LegendLabelStyle() }
func legendLeadKey() lipgloss.Style         { return uitheme.LegendLeadKey() }
func legendLeadLabel() lipgloss.Style       { return uitheme.LegendLeadLabel() }

func renderSelectedRow(s string) string           { return uitheme.RenderSelectedRow(s) }
func annotationBg() string                        { return uitheme.AnnotationBg() }
func handledAnnotationBg() string                 { return uitheme.HandledAnnotationBg() }
func backdropHex() string                         { return uitheme.BackdropHex() }
func panelHex() string                            { return uitheme.PanelHex() }
func blockHex() string                            { return uitheme.BlockHex() }
func selectedHex() string                         { return uitheme.SelectedHex() }
func quickModeHex() string                        { return uitheme.QuickModeHex() }
func searchFieldHex() string                      { return uitheme.SearchFieldHex() }
func liftedHex() string                           { return uitheme.LiftedHex() }
func dropHex() string                             { return uitheme.DropHex() }
func restingMarkHex() string                      { return uitheme.RestingMarkHex() }
func ruleHex() string                             { return uitheme.RuleHex() }
func hrule(width int) string                      { return uitheme.Hrule(width) }
func paint(s string, width int, bg string) string { return uitheme.Paint(s, width, bg) }
func plain(s string, width int) string            { return uitheme.Plain(s, width) }

func fillBackdrop(frame string, width int, backgroundHex, foregroundHex string) string {
	return uitheme.FillBackdrop(frame, width, backgroundHex, foregroundHex)
}

func paintRows(lines []string, width, height int, bg string) []string {
	return uitheme.PaintRows(lines, width, height, bg)
}

func joinColumns(columns ...[]string) []string { return uitheme.JoinColumns(columns...) }

func indentLines(lines []string, n int) []string { return uitheme.IndentLines(lines, n) }

func splitLines(s string) []string { return uitheme.SplitLines(s) }

func bgSeq(hex string) string { return uitheme.BgSeq(hex) }

func fgSeq(hex string) string { return uitheme.FgSeq(hex) }

func gauge(percent float64, width int, invert bool) string {
	return uitheme.Gauge(percent, width, invert)
}

func gaugeRamp(percent float64) string { return uitheme.GaugeRamp(percent) }

func pill(text string, fg lipgloss.Color) string { return uitheme.Pill(text, fg) }

func keyPill(key, text string, fg lipgloss.Color) string {
	return uitheme.KeyPill(key, text, fg)
}

func keyCap(key, label string) string { return uitheme.KeyCap(key, label) }

func hintCap(key, label string) string { return uitheme.HintCap(key, label) }

func keyCapLead(key, label string) string { return uitheme.KeyCapLead(key, label) }

func keyCapAlert(key, label string) string { return uitheme.KeyCapAlert(key, label) }

func keyCapQuiet(key, label string) string { return uitheme.KeyCapQuiet(key, label) }

func imageChip(token string) string { return uitheme.ImageChip(token) }

func imageChipPasting(token string) string { return uitheme.ImageChipPasting(token) }
