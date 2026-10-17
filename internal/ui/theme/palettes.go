package theme

// Theme is the full token set every rendered pixel of the TUI resolves
// through. Colors are hex so terminals with truecolor get the exact
// palette; lipgloss degrades them to the closest 256 index elsewhere.
//
// Bg is the backdrop: the root paints it under every cell a view leaves
// unpainted, and it is the ink on accent fills.
type Theme struct {
	Name string

	// Surfaces and structure.
	Bg      string // deepest tone: the backdrop, and ink on accent fills
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

	// Counterpart names the same family on the other side of LightBackdrop,
	// which theme auto-detect switches to before falling back to a default.
	Counterpart string
}

// Palettes is the built-in set, in picker order. Classic leads: it
// is the default, a restrained take on the sixteen colors every terminal
// has had for decades.
var Palettes = []Theme{
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

		Counterpart: "solarized light",
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

		Counterpart: "catppuccin latte",
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

		Counterpart: "tokyo night day",
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

		Counterpart: "gruvbox light",
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

		Counterpart: "rosé pine dawn",
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
		Name:    "kanagawa wave",
		Bg:      "#1f1f28",
		Surface: "#2a2a37",
		Overlay: "#363646",
		Border:  "#363646",
		Bright:  "#dcd7ba",
		Text:    "#c8c093",
		Dim:     "#938aa9",
		Subtle:  "#727169",
		Accent:  "#7e9cd8",
		Accent2: "#7aa89f",

		Working:  "#ff9e3b",
		Waiting:  "#957fb8",
		Finished: "#98bb6c",
		Errored:  "#e82424",
		Idle:     "#727169",

		Counterpart: "kanagawa lotus",
	},
	{
		Name:    "onedark",
		Bg:      "#282c34",
		Surface: "#393f4a",
		Overlay: "#3e4452",
		Border:  "#3e4452",
		Bright:  "#dcdfe4",
		Text:    "#abb2bf",
		Dim:     "#848b98",
		Subtle:  "#5c6370",
		Accent:  "#61afef",
		Accent2: "#56b6c2",

		Working:  "#d19a66",
		Waiting:  "#c678dd",
		Finished: "#98c379",
		Errored:  "#e86671",
		Idle:     "#5c6370",
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

		Counterpart: "solarized dark",
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

		Counterpart: "catppuccin mocha",
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

		Counterpart: "tokyo night",
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

		Counterpart: "gruvbox dark",
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

		Counterpart: "rosé pine",
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
	{
		Name:    "kanagawa lotus",
		Bg:      "#f2ecbc",
		Surface: "#dcd5ac",
		Overlay: "#d5cea3",
		Border:  "#d5cea3",
		Bright:  "#43436c",
		Text:    "#545464",
		Dim:     "#766b90",
		Subtle:  "#8a8980",
		Accent:  "#4d699b",
		Accent2: "#597b75",

		Working:  "#e98a00",
		Waiting:  "#624c83",
		Finished: "#6f894e",
		Errored:  "#e82424",
		Idle:     "#8a8980",

		Counterpart: "kanagawa wave",
	},
}
