package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/charmbracelet/lipgloss"
)

// Theme is the full token set every rendered pixel of the TUI resolves
// through. Colors are hex so terminals with truecolor get the exact
// palette; lipgloss degrades them to the closest 256 index elsewhere.
//
// No token paints a full-screen background: the terminal's own backdrop
// shows through, and Bg is only ever used as the text color sitting on an
// accent fill.
type Theme struct {
	Name string

	// Surfaces and structure.
	Bg      string // deepest tone, used as ink on accent fills
	Surface string // selected row / chip fill
	Overlay string // raised fill for gauges tracks and gutters
	Border  string // idle panel border

	// Type.
	Bright string // emphasized text (selected names, values that must win)
	Text   string // body text
	Dim    string // secondary text
	Subtle string // tertiary text, rules, separators

	// Brand.
	Accent  string // primary: focus, keys, brand
	Accent2 string // secondary: groups, scopes

	// Agent states.
	Working  string
	Waiting  string
	Finished string
	Errored  string
	Idle     string
}

// themes is the built-in palette set, in picker order. Classic leads: it
// is the default, a restrained take on the sixteen colors every terminal
// has had for decades.
var themes = []Theme{
	{
		Name:    "classic",
		Bg:      "#0f1115",
		Surface: "#232830",
		Overlay: "#2d333d",
		Border:  "#2d333d",
		Bright:  "#eceff4",
		Text:    "#c6ccd6",
		Dim:     "#98a0ac",
		Subtle:  "#646c78",
		Accent:  "#6cb6a4",
		Accent2: "#6daaba",

		Working:  "#d08442",
		Waiting:  "#a78bd0",
		Finished: "#85b26f",
		Errored:  "#cc6a6a",
		Idle:     "#646c78",
	},
	{
		Name:    "solarized dark",
		Bg:      "#002b36",
		Surface: "#073642",
		Overlay: "#0e4753",
		Border:  "#0e4753",
		Bright:  "#eee8d5",
		Text:    "#93a1a1",
		Dim:     "#839496",
		Subtle:  "#586e75",
		Accent:  "#268bd2",
		Accent2: "#2aa198",

		Working:  "#268bd2",
		Waiting:  "#b58900",
		Finished: "#859900",
		Errored:  "#dc322f",
		Idle:     "#586e75",
	},
	{
		Name:    "catppuccin mocha",
		Bg:      "#11111b",
		Surface: "#313244",
		Overlay: "#45475a",
		Border:  "#45475a",
		Bright:  "#f5f5ff",
		Text:    "#cdd6f4",
		Dim:     "#a6adc8",
		Subtle:  "#6c7086",
		Accent:  "#cba6f7",
		Accent2: "#94e2d5",

		Working:  "#fab387",
		Waiting:  "#f5c2e7",
		Finished: "#a6e3a1",
		Errored:  "#f38ba8",
		Idle:     "#7f849c",
	},
	{
		Name:    "tokyo night",
		Bg:      "#1a1b26",
		Surface: "#292e42",
		Overlay: "#3b4261",
		Border:  "#3b4261",
		Bright:  "#ffffff",
		Text:    "#c0caf5",
		Dim:     "#9aa5ce",
		Subtle:  "#565f89",
		Accent:  "#7aa2f7",
		Accent2: "#7dcfff",

		Working:  "#e0af68",
		Waiting:  "#bb9af7",
		Finished: "#9ece6a",
		Errored:  "#f7768e",
		Idle:     "#565f89",
	},
	{
		Name:    "gruvbox dark",
		Bg:      "#1d2021",
		Surface: "#3c3836",
		Overlay: "#504945",
		Border:  "#504945",
		Bright:  "#fbf1c7",
		Text:    "#ebdbb2",
		Dim:     "#bdae93",
		Subtle:  "#928374",
		Accent:  "#83a598",
		Accent2: "#8ec07c",

		Working:  "#fabd2f",
		Waiting:  "#d3869b",
		Finished: "#b8bb26",
		Errored:  "#fb4934",
		Idle:     "#928374",
	},
	{
		Name:    "nord",
		Bg:      "#2e3440",
		Surface: "#3b4252",
		Overlay: "#434c5e",
		Border:  "#434c5e",
		Bright:  "#eceff4",
		Text:    "#d8dee9",
		Dim:     "#aebacf",
		Subtle:  "#616e88",
		Accent:  "#88c0d0",
		Accent2: "#8fbcbb",

		Working:  "#ebcb8b",
		Waiting:  "#b48ead",
		Finished: "#a3be8c",
		Errored:  "#bf616a",
		Idle:     "#6b7689",
	},
	{
		Name:    "dracula",
		Bg:      "#21222c",
		Surface: "#343746",
		Overlay: "#44475a",
		Border:  "#44475a",
		Bright:  "#ffffff",
		Text:    "#f8f8f2",
		Dim:     "#c3c3d0",
		Subtle:  "#6272a4",
		Accent:  "#bd93f9",
		Accent2: "#8be9fd",

		Working:  "#ffb86c",
		Waiting:  "#ff79c6",
		Finished: "#50fa7b",
		Errored:  "#ff5555",
		Idle:     "#6272a4",
	},
	{
		Name:    "rosé pine",
		Bg:      "#191724",
		Surface: "#26233a",
		Overlay: "#403d52",
		Border:  "#403d52",
		Bright:  "#e0def4",
		Text:    "#e0def4",
		Dim:     "#908caa",
		Subtle:  "#6e6a86",
		Accent:  "#c4a7e7",
		Accent2: "#9ccfd8",

		Working:  "#f6c177",
		Waiting:  "#ebbcba",
		Finished: "#31748f",
		Errored:  "#eb6f92",
		Idle:     "#6e6a86",
	},
	{
		Name:    "monochrome",
		Bg:      "#0d0d0f",
		Surface: "#26262b",
		Overlay: "#3a3a41",
		Border:  "#3a3a41",
		Bright:  "#ffffff",
		Text:    "#d0d0d8",
		Dim:     "#9a9aa4",
		Subtle:  "#63636d",
		Accent:  "#d8d8e0",
		Accent2: "#a8a8b4",

		Working:  "#e8e8ef",
		Waiting:  "#c8c8d2",
		Finished: "#a8a8b4",
		Errored:  "#f0f0f6",
		Idle:     "#63636d",
	},
	{
		Name:    "solarized light",
		Bg:      "#fdf6e3",
		Surface: "#eee8d5",
		Overlay: "#e0d8bf",
		Border:  "#e0d8bf",
		Bright:  "#073642",
		Text:    "#586e75",
		Dim:     "#657b83",
		Subtle:  "#93a1a1",
		Accent:  "#268bd2",
		Accent2: "#218a80",

		Working:  "#268bd2",
		Waiting:  "#b58900",
		Finished: "#859900",
		Errored:  "#dc322f",
		Idle:     "#93a1a1",
	},
	{
		Name:    "catppuccin latte",
		Bg:      "#eff1f5",
		Surface: "#ccd0da",
		Overlay: "#bcc0cc",
		Border:  "#bcc0cc",
		Bright:  "#2c2f44",
		Text:    "#4c4f69",
		Dim:     "#6c6f85",
		Subtle:  "#9ca0b0",
		Accent:  "#8839ef",
		Accent2: "#179299",

		Working:  "#fe640b",
		Waiting:  "#ea76cb",
		Finished: "#40a02b",
		Errored:  "#d20f39",
		Idle:     "#8c8fa1",
	},
	{
		Name:    "tokyo night day",
		Bg:      "#e1e2e7",
		Surface: "#c4c8da",
		Overlay: "#a8aecb",
		Border:  "#a8aecb",
		Bright:  "#1a1b26",
		Text:    "#3760bf",
		Dim:     "#6172b0",
		Subtle:  "#848cb5",
		Accent:  "#2e7de9",
		Accent2: "#007197",

		Working:  "#8c6c3e",
		Waiting:  "#9854f1",
		Finished: "#587539",
		Errored:  "#f52a65",
		Idle:     "#848cb5",
	},
	{
		Name:    "gruvbox light",
		Bg:      "#fbf1c7",
		Surface: "#ebdbb2",
		Overlay: "#d5c4a1",
		Border:  "#d5c4a1",
		Bright:  "#282828",
		Text:    "#3c3836",
		Dim:     "#665c54",
		Subtle:  "#928374",
		Accent:  "#076678",
		Accent2: "#427b58",

		Working:  "#b57614",
		Waiting:  "#8f3f71",
		Finished: "#79740e",
		Errored:  "#9d0006",
		Idle:     "#928374",
	},
	{
		Name:    "rosé pine dawn",
		Bg:      "#faf4ed",
		Surface: "#dfdad9",
		Overlay: "#cecacd",
		Border:  "#cecacd",
		Bright:  "#575279",
		Text:    "#575279",
		Dim:     "#797593",
		Subtle:  "#9893a5",
		Accent:  "#907aa9",
		Accent2: "#56949f",

		Working:  "#ea9d34",
		Waiting:  "#d7827e",
		Finished: "#286983",
		Errored:  "#b4637a",
		Idle:     "#9893a5",
	},
	{
		Name:    "paper",
		Bg:      "#f7f7f5",
		Surface: "#e2e2df",
		Overlay: "#d0d0cc",
		Border:  "#d0d0cc",
		Bright:  "#111114",
		Text:    "#33333a",
		Dim:     "#5f5f68",
		Subtle:  "#9a9aa0",
		Accent:  "#2f2f38",
		Accent2: "#6a6a74",

		Working:  "#1c1c22",
		Waiting:  "#4a4a54",
		Finished: "#6a6a74",
		Errored:  "#0a0a0e",
		Idle:     "#9a9aa0",
	},
}

const themeSetting = "theme"
const themeAutoSetting = "theme_auto"

// themeIndex finds a theme by name, falling back to the default when the
// stored name is unknown (a theme removed between releases).
func themeIndex(name string) int {
	for i, t := range themes {
		if t.Name == name {
			return i
		}
	}
	return 0
}

// applyTheme rebuilds every package-level style from a token set. The TUI
// runs one model per process, so the styles stay package-level and a theme
// switch simply repaints from the next frame on.
func applyTheme(t Theme) {
	current = t

	colorBg = lipgloss.Color(t.Bg)
	colorSurface = lipgloss.Color(t.Surface)
	colorOverlay = lipgloss.Color(t.Overlay)
	colorBorder = lipgloss.Color(t.Border)
	colorBright = lipgloss.Color(t.Bright)
	colorText = lipgloss.Color(t.Text)
	colorDim = lipgloss.Color(t.Dim)
	colorSubtle = lipgloss.Color(t.Subtle)
	colorAccent = lipgloss.Color(t.Accent)
	colorAccent2 = lipgloss.Color(t.Accent2)
	colorSelBg = colorSurface

	colorWorking = lipgloss.Color(t.Working)
	colorWaiting = lipgloss.Color(t.Waiting)
	colorFinished = lipgloss.Color(t.Finished)
	colorErrored = lipgloss.Color(t.Errored)
	colorIdle = lipgloss.Color(t.Idle)

	rebuildCaptureBackdrop(t)
	rebuildStyles()
}

// current is the live token set; renderers that need a raw SGR sequence
// (rather than a lipgloss style) read their hex from here.
var current = themes[0]

// SyncTerminalBackground repaints the terminal's own background to the
// theme's backdrop (OSC 11). The frame can only paint its cell grid; any
// window padding the terminal draws around that grid keeps the terminal's
// color, so the two must be the same color for the frame's edges to look
// exact. Terminals without OSC 11 ignore it.
func SyncTerminalBackground() {
	emitToTerminal("\x1b]11;" + current.Bg + "\x07")
}

// syncPaneTheme hands the tmux server the background agent panes render on,
// so an agent that auto-detects its palette resolves to the same side the
// manager is drawing. Sessions that are already running keep whatever they
// resolved at startup; the theme reaches them on their next launch.
func (m *Model) syncPaneTheme() {
	if err := m.tmux.SetPaneTheme(agentPaneTheme()); err != nil {
		m.errBar.text = err.Error()
	}
}

// ResetTerminalBackground restores the terminal's own background (OSC 111)
// when the manager exits.
func ResetTerminalBackground() {
	emitToTerminal("\x1b]111\x07")
}

// SyncAttachBackground repaints the terminal for a full-screen attach. On
// a light theme the agent gets the capture backdrop's dark color — its
// colors were picked for a dark terminal, same as in the preview panel.
// attachDoneMsg restores the theme backdrop on detach. On a dark theme the
// synced color already serves both, so nothing is emitted.
func SyncAttachBackground() {
	if !captureOnDark {
		return
	}
	emitToTerminal("\x1b]11;" + themes[0].Bg + "\x07")
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

// bgSeq is the raw "set background" SGR for a hex color, for the few spots
// that paint a background around content lipgloss already styled. It goes
// through the live color profile, so a 256-color terminal gets an indexed
// sequence rather than a truecolor one lipgloss would have downsampled.
func bgSeq(hex string) string {
	return "\x1b[" + lipgloss.ColorProfile().Color(hex).Sequence(true) + "m"
}

// fgSeq is the raw "set foreground" SGR for a hex color.
func fgSeq(hex string) string {
	return "\x1b[" + lipgloss.ColorProfile().Color(hex).Sequence(false) + "m"
}

// hexRGB parses "#rrggbb"; an unparseable value reads as black, which is
// visible enough in a diff to be caught rather than silently mistinted.
func hexRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	val, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(val>>16) & 0xff, int(val>>8) & 0xff, int(val) & 0xff
}

// mix blends two hex colors, ratio 0 returning a and 1 returning b. Used
// for derived tones (row gutters, gauge tracks) so a theme only has to
// declare its anchors.
func mix(a, b string, ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	blend := func(x, y int) int {
		return int(float64(x)*(1-ratio) + float64(y)*ratio + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", blend(ar, br), blend(ag, bg), blend(ab, bb))
}
