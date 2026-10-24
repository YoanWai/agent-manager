package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestViewPinsTheFrameToTheTerminalHeight(t *testing.T) {
	tall := "a\nb\nc\nd\ne"
	got := uiframe.Clamp(tall, 3, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) })
	if got != "a\nb\nc" {
		t.Fatalf("clamp trim = %q", got)
	}
	short := "x"
	got = uiframe.Clamp(short, 3, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) })
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || lines[0] != "x" {
		t.Fatalf("clamp pad = %q", got)
	}
	for _, line := range lines[1:] {
		// Padding rows wear the backdrop fill rather than being bare, so
		// a short frame cannot show the terminal's background through its
		// bottom rows.
		if !strings.Contains(line, " ") {
			t.Fatalf("padding row is bare: %q", line)
		}
	}
}

// TestZZShot renders a full frame to disk for visual review. Skipped
// unless AM_SHOT names an output path.
func TestZZShot(t *testing.T) {
	out := os.Getenv("AM_SHOT")
	if out == "" {
		t.Skip("set AM_SHOT to render a frame")
	}
	if name := os.Getenv("AM_SHOT_THEME"); name != "" {
		applyTheme(uitheme.Palettes[uitheme.Index(name)])
		t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	switch os.Getenv("AM_SHOT_MODE") {
	case "settings":
		m.push(newSettingsSurface(m, uisettings.State{
			ToolNames: []string{"claude", "codex", "grok", "opencode"},
			ToolIndex: 0, ThemeIndex: 1, Field: uisettings.FieldTheme, LayoutSplit: true,
		}))
		m.release.Version = "0.9.2"
	case "help":
		m.openHelp()
	}
	if phase := os.Getenv("AM_SHOT_PHASE"); phase != "" {
		n, err := strconv.Atoi(phase)
		if err != nil {
			t.Fatal(err)
		}
		m.startup.BannerPhase = n
	}
	if err := os.WriteFile(out, []byte(preparedView(m)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A frame line wider than the terminal wraps, which pushes the whole
// layout down a row and tears the panels. A frame with more rows than the
// terminal loses its footer to the clamp. Both have to hold at every size
// the layout still makes sense at, including the width where the header
// swaps between the wordmark and its one-line form.
func TestFrameFitsTerminal(t *testing.T) {
	for _, width := range []int{80, 100, 120, 160, 200} {
		for _, height := range []int{24, 34, 50} {
			m := shotModel()
			m.layout.width, m.layout.height = width, height
			// View clamps and pads, which would hide a frame that is a row
			// short or a row long, so the raw frame is what gets measured.
			raw := strings.Split(m.viewListFrame(), "\n")
			if len(raw) != height {
				t.Errorf("%dx%d: frame paints %d rows", width, height, len(raw))
			}
			lines := strings.Split(preparedView(m), "\n")
			for i, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d: line %d is %d wide: %q", width, height, i, got, ansi.Strip(line))
				}
			}
		}
	}
}

// The pane's soft edges: the opening row is drawn in sextants and stops a
// third of a cell under the wordmark, the closing row bleeds up by half, and
// every body row ends with the ▐ half-block edge one column past the seam.
// The end cells of the edge rows are the corners: the first is a foreground
// block so the window margin cannot inherit pane tone and smear past the
// corner, and the last is narrowed to the bleed's half width so the fill
// closes at the corner instead of jutting right. The top row's corner is
// also a sextant, which is what keeps its edge level with the run beside it.
func TestPaneSoftEdges(t *testing.T) {
	m := shotModel()
	rows := strings.Split(preparedView(m), "\n")
	leftWidth, _ := m.splitWidths()

	top := []rune(ansi.Strip(rows[m.headerRows()]))
	bottom := []rune(ansi.Strip(rows[m.headerRows()+1+m.listBodyHeight()]))
	if top[0] != '🬹' || bottom[0] != '▀' {
		t.Fatalf("edge rows should open with foreground blocks, got %q and %q",
			string(top[0]), string(bottom[0]))
	}
	for col := 1; col <= leftWidth; col++ {
		if top[col] != '🬂' {
			t.Fatalf("top edge col %d is %q, want 🬂:\n%s", col, string(top[col]), string(top))
		}
		if bottom[col] != '▄' {
			t.Fatalf("bottom edge col %d is %q, want ▄:\n%s", col, string(bottom[col]), string(bottom))
		}
	}
	if top[leftWidth+1] != '🬨' || bottom[leftWidth+1] != '▟' {
		t.Fatalf("edge rows should close on the bleed's half width, got %q and %q",
			string(top[leftWidth+1]), string(bottom[leftWidth+1]))
	}
	if top[leftWidth+2] == '🬂' || bottom[leftWidth+2] == '▄' {
		t.Fatalf("the bleed should stop after the seam's edge column")
	}
	for i := m.headerRows() + 1; i < m.headerRows()+1+m.listBodyHeight(); i++ {
		row := []rune(ansi.Strip(rows[i]))
		if cell := row[0]; cell != '█' {
			t.Fatalf("row %d: first column is %q, want █:\n%s", i, string(cell), string(row))
		}
		if cell := row[leftWidth+1]; cell != '▐' && cell != '─' {
			t.Fatalf("row %d: edge column is %q, want ▐:\n%s", i, string(cell), string(row))
		}
		// The seam is fill, not a drawn line: the glyphs of the old line
		// seam appearing here mean a stale seam renderer shipped again.
		if cell := row[leftWidth]; cell != ' ' && cell != '─' {
			t.Fatalf("row %d: seam column is %q, want pane fill:\n%s", i, string(cell), string(row))
		}
	}
}

func TestViewDoesNotPublishCursorState(t *testing.T) {
	m := &Model{}
	m.focus.imeCursor = &uiframe.CursorAnchor{}
	m.focus.imeCursor.Set(8, 4, true)
	if got := m.View(); got != "loading..." {
		t.Fatalf("initial view = %q", got)
	}
	col, row, ok := m.focus.imeCursor.Get()
	if col != 8 || row != 4 || !ok {
		t.Fatalf("View changed published cursor to (%d, %d, %t)", col, row, ok)
	}
}

func TestUpdatePublishesPreparedFrame(t *testing.T) {
	m := shotModel()
	m.prepareFrame()
	before := m.View()
	m.openHelp()
	if got := m.View(); got != before {
		t.Fatal("View rendered state that had not been prepared")
	}
	updated, _ := m.Update(struct{}{})
	if updated != m {
		t.Fatal("Update replaced the root model")
	}
	if got := m.View(); got == before || got == "loading..." {
		t.Fatal("Update did not publish the changed frame")
	}
}

func preparedView(m *Model) string {
	m.prepareFrame()
	return m.View()
}

func TestHeaderShowsUpdateBadgeBesideWordmark(t *testing.T) {
	m := &Model{
		layout:  layoutState{width: 120},
		release: uirelease.State{Latest: "v0.9.0"},
	}
	header := ansi.Strip(m.viewHeaderRows()[0])
	if !strings.Contains(header, "v0.9.0") || !strings.Contains(header, "available") {
		t.Errorf("header missing update badge: %q", header)
	}
	if strings.Index(header, "available") > strings.Index(header, "agents") {
		t.Errorf("badge should sit left, by the wordmark: %q", header)
	}
	m.release.Latest = ""
	if header := ansi.Strip(m.viewHeaderRows()[0]); strings.Contains(header, "available") {
		t.Errorf("header should have no badge when up to date: %q", header)
	}
}

func TestViewLegendPaintsWaitingMessagesWarm(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	waiting := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		release:  uirelease.State{Latest: "v0.41.0", Version: "0.40.0"},
	}
	bar := paintLegend([]legendSection{waiting.viewLegend()}, 160).Text
	if !strings.Contains(bar, uitheme.LegendAlertStyle().Render("M unread")) {
		t.Fatalf("waiting messages should use the working color:\n%s", bar)
	}
	idle := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		release:  uirelease.State{Version: "0.40.0"},
		notices:  noticesPanel{Panel: uinotices.NewPanel(map[string]bool{uinotices.Welcome: true, uinotices.ArrowStep: true})},
	}
	idleBar := ansi.Strip(paintLegend([]legendSection{idle.viewLegend()}, 160).Text)
	if strings.Contains(idleBar, "unread") {
		t.Fatalf("an empty panel should leave the messages key off:\n%s", idleBar)
	}
}

// The root's tier literals reach the painter: every field the row and view
// legends set survives the copy into ui/frame.
func TestRootLegendKeepsItsTiersAndHits(t *testing.T) {
	sections := []legendSection{{
		Title:  "Session",
		Leads:  [][2]string{{"p", "quick prompt mode"}},
		Alerts: [][2]string{{"M", "unread"}},
		Hint:   "a hint",
		Pairs:  [][2]string{{"r", "rename"}},
	}}
	painted := paintLegend(sections, 200)
	plain := ansi.Strip(painted.Text)
	for _, want := range []string{"Session", "p quick prompt mode", "M unread", "a hint", "r rename"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("root legend lost %q:\n%s", want, plain)
		}
	}
	if len(painted.Hits) != 3 {
		t.Fatalf("hits = %+v, want one per pressable pair", painted.Hits)
	}
	lines := strings.Split(plain, "\n")
	for _, hit := range painted.Hits {
		if hit.Y < 0 || hit.Y >= len(lines) {
			t.Fatalf("hit %+v lands on no painted row", hit)
		}
	}
}

// The reviewed repo is code the user did not write. Our own rendering emits
// nothing but SGR colour sequences, so any other control byte in a painted
// frame is the repo driving the terminal.
func strayControl(frame string) string {
	if !utf8.ValidString(frame) {
		return "malformed UTF-8, where a raw 8-bit CSI or OSC would hide"
	}
	for i := 0; i < len(frame); i++ {
		c := frame[i]
		if c == '\n' || !presentation.IsControlByte(c) {
			continue
		}
		if n := sgrLen(frame[i:]); n > 0 {
			i += n - 1
			continue
		}
		start := max(0, i-20)
		return frame[start:min(len(frame), i+20)]
	}
	return ""
}

// sgrLen returns the length of the SGR colour sequence at the head of s, or 0
// when s does not start with one.
func sgrLen(s string) int {
	if len(s) < 3 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		if s[i] == 'm' {
			return i + 1
		}
		if (s[i] < '0' || s[i] > '9') && s[i] != ';' && s[i] != ':' {
			return 0
		}
	}
	return 0
}

func TestEscapeControlsShowsBytesAndKeepsUTF8(t *testing.T) {
	cases := map[string]string{
		"\x1b]0;PWNED\x07": "^[]0;PWNED^G",
		"a\x1b[2Jb":        "a^[[2Jb",
		"over\rwrite":      "over^Mwrite",
		"del\x7f":          "del^?",
		"nul\x00":          "nul^@",
		"שלום ok":          "שלום ok",
		"plain":            "plain",
		// A lone 0x9b/0x9d is the 8-bit CSI/OSC and is malformed UTF-8.
		"x\x9b31mred":  `x\x9B31mred`,
		"x\x9d0;P\x9c": `x\x9D0;P\x9C`,
		// The same code points encoded properly are C1 controls all the
		// same: a terminal that reads them obeys them, and ansi.StringWidth
		// counts them as nothing while they take a cell.
		"x\u009b31m": `x\u009B31m`,
		"x\u009d0;P": `x\u009D0;P`,
	}
	for in, want := range cases {
		if got := presentation.EscapeControls(in); got != want {
			t.Errorf("presentation.EscapeControls(%q) = %q, want %q", in, got, want)
		}
	}
}

// A git error arrives from CombinedOutput with its lines intact, and the hint
// under the first line is the half that says what to do. Only a surface owning
// exactly one row folds them together.
func TestEscapeControlsKeepsNewlinesAndInlineDropsThem(t *testing.T) {
	const gitErr = "fatal: ambiguous argument 'x'\nUse '--' to separate paths from revisions"
	if got := presentation.EscapeControls(gitErr); got != gitErr {
		t.Errorf("presentation.EscapeControls(%q) = %q, want it untouched", gitErr, got)
	}
	if got := presentation.EscapeControls("a\n\x1bb"); got != "a\n^[b" {
		t.Errorf(`presentation.EscapeControls("a\n\x1bb") = %q, want "a\n^[b"`, got)
	}
	want := "fatal: ambiguous argument 'x' Use '--' to separate paths from revisions"
	if got := presentation.EscapeControlsInline(gitErr); got != want {
		t.Errorf("presentation.EscapeControlsInline(%q) = %q, want %q", gitErr, got, want)
	}
	if got := presentation.EscapeControlsInline("a\n\x1bb"); got != "a ^[b" {
		t.Errorf(`presentation.EscapeControlsInline("a\n\x1bb") = %q, want "a ^[b"`, got)
	}
}

// Hebrew encodes continuation bytes in the 0x80-0x9f range, so a byte-range
// check for C1 would mangle it. Escaping keys off malformed UTF-8 instead.
func TestEscapeControlsKeepsTextWhoseBytesLookLikeC1(t *testing.T) {
	for _, text := range []string{"שלום", "מה קורה", "日本語", "café"} {
		if got := presentation.EscapeControls(text); got != text {
			t.Errorf("presentation.EscapeControls(%q) = %q, want it untouched", text, got)
		}
	}
}

// The agent in the pane runs `agent-manager rename` itself, so the session
// name is content the review header paints, not something the user typed.
func TestSessionNameCannotDriveTheTerminal(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "named", gitRepoWithTwoChangedFiles(t))
	m.layout.width, m.layout.height = 120, 40
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == m.review.Snapshot().SessionID {
			m.workspace.sessions[i].Name = "rev\x1b]0;PWNED\x07iew"
		}
	}

	frame := m.viewDiffFull()
	if !strings.Contains(frame, "rev^[]0;PWNED^Giew") {
		t.Fatal("the session name should read as caret notation in the header")
	}
	if stray := strayControl(frame); stray != "" {
		t.Errorf("a session name leaks a control byte near %q", stray)
	}

	state := m.review.Snapshot()
	m.review.ApplySend(uireview.SendResult{TargetID: state.SessionID, RepoRoot: state.RepoSelected, Round: 1, Count: 1, TargetName: "rev\x1b]0;PWNED\x07iew", Outcome: uireview.SendConfirmed})
	if stray := strayControl(m.viewDiffFooter()); stray != "" {
		t.Errorf("the review notice leaks a control byte near %q", stray)
	}
	m.errBar.text = "rename failed for rev\x1b]0;PWNED\x07iew"
	if stray := strayControl(m.statusMessage(m.glyphs().ErroredGlyph, m.glyphs().FinishedGlyph, "▲")); stray != "" {
		t.Errorf("the status bar leaks a control byte near %q", stray)
	}
}

// git.Driver.run embeds CombinedOutput, and the second line of an ambiguous
// -argument error is the one that says what to do about it. The review's empty
// state owns the rows to show it; paint would truncate a folded single row.
func TestMultiLineGitErrorKeepsItsLines(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	req := seedReviewForTest(m, uireview.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	m.review.ApplyLoad(uireview.LoadResult{TargetID: "s1", Scope: req.Scope, Generation: req.Generation, RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Err: errors.New("fatal: ambiguous argument 'x'\nUse '--' to separate paths from revisions")})
	rows := strings.Split(m.review.EmptyText(m.reviewRenderContext()), "\n")
	if len(rows) != 2 {
		t.Fatalf("a two-line git error rendered %d rows: %q", len(rows), rows)
	}
	if !strings.Contains(rows[1], "separate paths from revisions") {
		t.Fatalf("the hint line was lost, rows = %q", rows)
	}
}

// The whole point: a file under review must not be able to drive the terminal
// the review is painted into. Both layouts, because they render through
// different cells.
func TestReviewedFileCannotDriveTheTerminal(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	// An OSC window-title set and a clear-screen, sequences our renderer
	// never emits, so finding them in the frame is unambiguous.
	payload := "const banner = \"\x1b]0;PWNED\x07\x1b[2J\x1b[31mgotcha\"\n"
	dir := gitRepoWithPayload(t, "payload.go", payload)
	openReviewOn(t, m, "escape", dir)
	m.layout.width, m.layout.height = 120, 40

	for _, layout := range []string{"unified", "split"} {
		frame := m.viewDiffFull()
		if !strings.Contains(frame, "PWNED") {
			t.Fatalf("%s: the payload line should be on screen to be a real test", layout)
		}
		if stray := strayControl(frame); stray != "" {
			t.Errorf("%s layout leaks a control byte to the terminal near %q", layout, stray)
		}
		m.review.ToggleSideBySide(m.diffCodeHeight())
	}
}

// A clean repo proves the assertion above is not vacuous: the frame really
// does carry nothing but SGR, so a stray control byte means something leaked.
func TestCleanReviewFrameCarriesOnlySGR(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "clean", gitRepoWithTwoChangedFiles(t))
	m.layout.width, m.layout.height = 120, 40
	if stray := strayControl(m.viewDiffFull()); stray != "" {
		t.Fatalf("a clean review frame should be SGR only, found %q", stray)
	}
}

// Git is driven with -z, so a path is handed to us as raw bytes and reaches
// the file rail, the code title and the comment bar unquoted.
func TestReviewedPathCannotDriveTheTerminal(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := committedRepo(t)
	name := "w\x1b]0;P\x07.go"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("package a\n"), 0o644); err != nil {
		t.Skipf("filesystem rejects control bytes in a name: %v", err)
	}
	openReviewOn(t, m, "path", dir)
	m.layout.width, m.layout.height = 120, 40

	frame := m.viewDiffFull()
	if !strings.Contains(frame, "w^[]0;P^G.go") {
		t.Fatalf("the path should read as caret notation on screen, got %q", frame)
	}
	if stray := strayControl(frame); stray != "" {
		t.Errorf("a reviewed path leaks a control byte near %q", stray)
	}
}

// gitRepoWithPayload commits a plain first version of name, then writes the
// payload over it, so review has a real change to render.
func gitRepoWithPayload(t *testing.T, name, content string) string {
	t.Helper()
	dir := committedRepo(t)
	writeFile(t, dir, name, "package a\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "add")
	writeFile(t, dir, name, content)
	return dir
}

func committedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	writeFile(t, dir, "seed.go", "package a\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// Focused, the keyboard belongs to the agent: one tier with the keys the
// manager keeps, and the app-wide keys — which would go to the agent, not
// the manager — stay out.
func TestFooterInFocusMode(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "focused", t.TempDir(), "")
	m.push(newFocusSurface(m))
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "Focused") {
		t.Fatalf("the tier should name the mode it describes:\n%s", footer)
	}
	if !strings.Contains(footer, "ctrl+q / ctrl+\\") || !strings.Contains(footer, "click the list") || !strings.Contains(footer, "mouse back") {
		t.Fatalf("focus footer should carry the reserved keys and mouse leave:\n%s", footer)
	}
	listH := lipgloss.Height(m.listFooter())
	if lipgloss.Height(m.viewFooter()) != listH {
		t.Fatalf("focus footer must keep the list footer's height %d, got %d:\n%s", listH, lipgloss.Height(m.viewFooter()), footer)
	}
	if strings.Contains(footer, "navigate") || strings.Contains(footer, "View") {
		t.Fatalf("app-wide keys go to the agent while focused, so the tier must go:\n%s", footer)
	}
	// Blank rows below hold the list footer's height so focusing never
	// resizes the pane. The keys themselves may wrap inside that budget.
	if strings.Contains(footer, "agent UI") {
		t.Fatalf("a plain focused pane should not offer mouse pass-through:\n%s", footer)
	}
	// Full screen focus paints no list, so the gesture that needs one goes.
	m.prefs.fullLayout = true
	full := ansi.Strip(m.viewFooter())
	if strings.Contains(full, "click the list") {
		t.Fatalf("full screen focus has no list to click:\n%s", full)
	}
	if !strings.Contains(full, "mouse back") {
		t.Fatalf("the button still leaves a full screen session:\n%s", full)
	}

	setFocusPaneFacts(m, railSelectedSession(m).ID, true, false, false, 0, paneCursor{})
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "click / alt+drag") || !strings.Contains(footer, "agent UI") {
		t.Fatalf("a mouse-tracking pane should advertise pass-through:\n%s", footer)
	}
}

func TestPagingHintFitsFullFocusFooter(t *testing.T) {
	shipped, err := config.Default()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m, sessID := focusedWithHistory(t, "paging-footer")
	m.services.cfg.Tools["gemini"] = shipped.Tools["gemini"]
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == sessID {
			m.workspace.sessions[i].Tool = "gemini"
		}
	}
	m.rebuildRows()
	m.prefs.fullLayout = true
	m.layout.width = 110
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "pgup/pgdn scroll") {
		t.Fatalf("full focus footer omits paging: %q", footer)
	}
	if got := lipgloss.Height(m.viewFooter()); got != 1 {
		t.Fatalf("full focus footer spans %d rows, want one: %q", got, footer)
	}
}

// The scrolled notice names PgDn only while PgDn is what walks the pane
// back down.
func TestScrolledStatusNamesPgDnWhileItPages(t *testing.T) {
	m, sessID := focusedWithHistory(t, "scrolled-status")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(*Model)
	if !m.focus.pane.ScrolledBack() {
		t.Fatal("test setup: PgUp did not scroll the pane back")
	}
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "lines back · wheel down, pgdn or type to catch up") {
		t.Fatalf("paging pane's scrolled notice = %q", got)
	}
	updateFocusPane(m, sessID, func(update *uifocus.PaneUpdate) { update.Mouse = true })
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "lines back · wheel down or type to catch up") {
		t.Fatalf("mouse-tracking pane's scrolled notice = %q", got)
	}
}

func TestArrowStepFooterHintsFollowSetting(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("arrow-group", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "arrow-hints", t.TempDir(), "")
	assertHint := func(context, hint string, enabled bool) {
		t.Helper()
		footer := ansi.Strip(m.viewFooter())
		if got := strings.Contains(footer, hint); got != enabled {
			t.Fatalf("arrow step enabled = %v: %s hint %q = %v:\n%s", enabled, context, hint, got, footer)
		}
	}

	for _, enabled := range []bool{true, false} {
		m.prefs.arrowStep = enabled
		m.base = baseList
		m.clearSurfaces()
		m.selectSessionRow(t, "arrow-hints")
		assertHint("session", "→ focus", enabled)
		m.selectGroupRow(t, "arrow-group")
		assertHint("group", "←/→ close / open", enabled)
		m.push(newFocusSurface(m))
		assertHint("focus", "← prompt start: back", enabled)
	}
}

// Every transient tier holds the list footer's height: the footer sets the
// preview box, and a box that moves resizes every session's pane, which
// costs an agent drawing on the normal screen a full transcript redraw.
func TestTransientFootersKeepListHeight(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "sizer", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	listed := lipgloss.Height(m.viewFooter())

	for _, tier := range []struct {
		name string
		open func()
	}{
		{"resize", func() { m.enterResizeMode() }},
		{"rename", func() {
			m.push(uidialogs.NewRenameSurface(renameRoot{m}, uidialogs.RenameSession("s", "alpha", "claude", nil), ""))
		}},
		{"focus", func() { m.push(newFocusSurface(m)) }},
	} {
		tier.open()
		if got := lipgloss.Height(m.viewFooter()); got != listed {
			t.Errorf("%s footer = %d rows, want %d", tier.name, got, listed)
		}
		m.base = baseList
		m.clearSurfaces()
		m.clearSurfaces()
	}
}

// With nothing under the cursor there is nothing to act on, so the footer
// carries only the app-wide tier.
func TestFooterWithoutASelectedRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	resetRailModel(m)
	footer := ansi.Strip(m.viewFooter())
	if strings.Contains(footer, "Session") || strings.Contains(footer, "Group") {
		t.Fatalf("no row selected, no row tier:\n%s", footer)
	}
	if !strings.Contains(footer, "View") {
		t.Fatalf("the app-wide tier should stay:\n%s", footer)
	}
}

func TestFooterTierFollowsTheCursor(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "legend", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())

	for i, row := range railRows(m) {
		if !row.isGroup {
			setRailCursor(m, i)
			break
		}
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "Session") {
		t.Fatalf("a session under the cursor should title the tier Session:\n%s", footer)
	}

	for i, row := range railRows(m) {
		if row.isGroup {
			setRailCursor(m, i)
			break
		}
	}
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "Group") {
		t.Fatalf("a group under the cursor should title the tier Group:\n%s", footer)
	}
	if strings.Contains(footer, "fork") {
		t.Fatalf("a group cannot be forked, so the key should not be offered:\n%s", footer)
	}
}

// A toggle's label names what the key will do next, not the state it is
// already in, so the footer reads as an instruction.
func TestFooterTogglesNameTheNextAction(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	// Wide enough that the row budget keeps every app-wide binding.
	m.layout.width = 260
	dir := t.TempDir()
	if err := m.services.store.AddGroup("work", dir, "off", ""); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "toggles", dir, "work")
	m.applyCmd(t, m.refreshCmd())

	if footer := m.viewFooter(); !strings.Contains(footer, uitheme.KeyCapQuiet("F", "fold all")) {
		t.Fatalf("an open tree should offer folding:\n%s", ansi.Strip(footer))
	}
	updated, _ := m.handleKey(runeKey("F"))
	m = updated.(*Model)
	if footer := m.viewFooter(); !strings.Contains(footer, uitheme.KeyCapQuiet("F", "unfold all")) {
		t.Fatalf("a folded tree should offer unfolding:\n%s", ansi.Strip(footer))
	}

	group := -1
	for i, row := range railRows(m) {
		if row.isGroup && !row.isRoot() {
			group = i
			break
		}
	}
	if group < 0 {
		t.Fatalf("the fixture should list a group to fold, rows: %v", m.groupRowPaths())
	}
	setRailCursor(m, group)
	if footer := m.viewFooter(); !strings.Contains(footer, uitheme.KeyCap("↵", "unfold")) {
		t.Fatalf("a collapsed group should offer unfolding:\n%s", ansi.Strip(footer))
	}

	m.rail.SetArchived(true)
	if footer := m.viewFooter(); !strings.Contains(footer, uitheme.KeyCapQuiet("t", "back to active")) {
		t.Fatalf("the archived view should offer the way back:\n%s", ansi.Strip(footer))
	}
}

func TestLegendOffersEmptyGroupsOnlyInTheActiveView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	offered := func() bool {
		for _, pair := range m.viewLegend().Pairs {
			if pair[1] == "hide empty" || pair[1] == "show empty" {
				return true
			}
		}
		return false
	}
	if !offered() {
		t.Fatal("the active view should offer hiding empty groups")
	}
	m.rail.SetHideEmptyGroups(true)
	if !offered() {
		t.Fatal("the active view should offer showing empty groups again")
	}
	m.rail.SetArchived(true)
	if offered() {
		t.Fatal("the archived view should not offer the empty-groups key")
	}
}

// The focus footer reads the key table: a moved key shows under its new
// name, and an action turned off has no hint to mislead with.
func TestFooterInFocusModeNamesTheKeyTable(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	useSessionKeys(t, m, []string{"f9"}, nil, []string{"alt+e"})
	m.push(newFocusSurface(m))
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "f9 / click the list / mouse back") || !strings.Contains(footer, "alt+e editor") {
		t.Fatalf("focus footer should name the configured keys:\n%s", footer)
	}
	if strings.Contains(footer, "review") || strings.Contains(footer, "ctrl+q") {
		t.Fatalf("focus footer should drop the review hint and the old detach key:\n%s", footer)
	}
	rule := ansi.Strip(focusTopRule(80, m.services.keys))
	if !strings.Contains(rule, "f9 back") || !strings.Contains(rule, "alt+e editor") || strings.Contains(rule, "review") {
		t.Fatalf("split focus rule should follow the table:\n%s", rule)
	}
}

func TestRowLegendDropsRestoreInActiveView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "zone")

	m.selectSessionRow(t, "alpha")
	legend := m.rowLegend()
	for _, pair := range legend.Pairs {
		if strings.Contains(pair[1], "restore") {
			t.Fatalf("session legend in active view should not offer restore, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.Pairs, func(pair [2]string) bool { return pair[1] == "archive" }) {
		t.Fatal("session legend in active view should still offer archive")
	}

	m.selectGroupRow(t, "zone")
	legend = m.rowLegend()
	for _, pair := range legend.Pairs {
		if strings.Contains(pair[1], "restore") {
			t.Fatalf("group legend in active view should not offer restore, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.Pairs, func(pair [2]string) bool { return pair[1] == "archive" }) {
		t.Fatal("group legend in active view should still offer archive")
	}
}

// In the archived view, archiveSelected no-ops, so the legend should offer
// only restore, on both a session row and a group row.
func TestRowLegendDropsArchiveInArchivedView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "zone")
	if err := m.services.store.SetArchived(m.sessionRows()[0].ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "alpha")
	legend := m.rowLegend()
	for _, pair := range legend.Pairs {
		if strings.Contains(pair[1], "archive") {
			t.Fatalf("session legend in archived view should not offer archive, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.Pairs, func(pair [2]string) bool { return pair[1] == "restore" }) {
		t.Fatal("session legend in archived view should still offer restore")
	}

	m.selectGroupRow(t, "zone")
	legend = m.rowLegend()
	for _, pair := range legend.Pairs {
		if strings.Contains(pair[1], "archive") {
			t.Fatalf("group legend in archived view should not offer archive, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.Pairs, func(pair [2]string) bool { return pair[1] == "restore" }) {
		t.Fatal("group legend in archived view should still offer restore")
	}
}

func TestQuickPromptFooterKeys(t *testing.T) {
	m := shotModel()
	pushQuick(m)
	m.quick().Input = textarea.New()
	m.quick().SetTools([]string{"claude"}, 0)

	footerOne := m.viewFooter()
	if strings.Contains(footerOne, "shift+tab") || strings.Contains(footerOne, "previous tool") {
		t.Errorf("one tool enabled, footer shouldn't have shift+tab: %q", footerOne)
	}
	if !strings.Contains(footerOne, "tab tool") {
		t.Errorf("one tool enabled, missing tab pair: %q", footerOne)
	}

	m.quick().SetTools([]string{"claude", "codex"}, 0)
	footerTwo := m.viewFooter()
	if !strings.Contains(footerTwo, "shift+tab") || !strings.Contains(footerTwo, "previous tool") {
		t.Errorf("two tools enabled, missing shift+tab pair: %q", footerTwo)
	}
	if !strings.Contains(footerTwo, "tab tool") {
		t.Errorf("two tools enabled, missing tab pair: %q", footerTwo)
	}
	if !strings.Contains(footerTwo, "ctrl+t worktree") {
		t.Errorf("missing ctrl+t worktree pair: %q", footerTwo)
	}
}
