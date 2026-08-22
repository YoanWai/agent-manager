package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
)

// The reviewed repo is code the user did not write. Our own rendering emits
// nothing but SGR colour sequences, so any other control byte in a painted
// frame is the repo driving the terminal.
func strayControl(frame string) string {
	if !utf8.ValidString(frame) {
		return "malformed UTF-8, where a raw 8-bit CSI or OSC would hide"
	}
	for i := 0; i < len(frame); i++ {
		c := frame[i]
		if c == '\n' || !isControlByte(c) {
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
		if got := escapeControls(in); got != want {
			t.Errorf("escapeControls(%q) = %q, want %q", in, got, want)
		}
	}
}

// A git error arrives from CombinedOutput with its lines intact, and the hint
// under the first line is the half that says what to do. Only a surface owning
// exactly one row folds them together.
func TestEscapeControlsKeepsNewlinesAndInlineDropsThem(t *testing.T) {
	const gitErr = "fatal: ambiguous argument 'x'\nUse '--' to separate paths from revisions"
	if got := escapeControls(gitErr); got != gitErr {
		t.Errorf("escapeControls(%q) = %q, want it untouched", gitErr, got)
	}
	if got := escapeControls("a\n\x1bb"); got != "a\n^[b" {
		t.Errorf(`escapeControls("a\n\x1bb") = %q, want "a\n^[b"`, got)
	}
	want := "fatal: ambiguous argument 'x' Use '--' to separate paths from revisions"
	if got := escapeControlsInline(gitErr); got != want {
		t.Errorf("escapeControlsInline(%q) = %q, want %q", gitErr, got, want)
	}
	if got := escapeControlsInline("a\n\x1bb"); got != "a ^[b" {
		t.Errorf(`escapeControlsInline("a\n\x1bb") = %q, want "a ^[b"`, got)
	}
}

// Hebrew encodes continuation bytes in the 0x80-0x9f range, so a byte-range
// check for C1 would mangle it. Escaping keys off malformed UTF-8 instead.
func TestEscapeControlsKeepsTextWhoseBytesLookLikeC1(t *testing.T) {
	for _, text := range []string{"שלום", "מה קורה", "日本語", "café"} {
		if got := escapeControls(text); got != text {
			t.Errorf("escapeControls(%q) = %q, want it untouched", text, got)
		}
	}
}

func TestEscapeSpansFollowTheEscapedText(t *testing.T) {
	// "a\x1bbc": the span over "bc" starts at byte 2 raw, byte 3 escaped,
	// because the ESC now occupies "^[".
	spans := escapeSpans("a\x1bbc", []diff.Span{{Start: 2, End: 4}})
	if len(spans) != 1 || spans[0] != (diff.Span{Start: 3, End: 5}) {
		t.Fatalf("spans = %+v, want [{3 5}]", spans)
	}
	// "a\x9bbc": the malformed byte renders as four bytes of hex, so the span
	// over "bc" moves from byte 2 to byte 5.
	spans = escapeSpans("a\x9bbc", []diff.Span{{Start: 2, End: 4}})
	if len(spans) != 1 || spans[0] != (diff.Span{Start: 5, End: 7}) {
		t.Fatalf("malformed-byte spans = %+v, want [{5 7}]", spans)
	}

	// "a\u009bbc": the C1 rune is two bytes raw and six escaped, so the span
	// over "bc" moves from byte 3 to byte 7.
	spans = escapeSpans("a\u009bbc", []diff.Span{{Start: 3, End: 5}})
	if len(spans) != 1 || spans[0] != (diff.Span{Start: 7, End: 9}) {
		t.Fatalf("C1 spans = %+v, want [{7 9}]", spans)
	}

	clean := []diff.Span{{Start: 1, End: 2}}
	if got := escapeSpans("abc", clean); &got[0] != &clean[0] {
		t.Fatal("a line with no control bytes should keep its spans untouched")
	}
}

// The agent in the pane runs `agent-manager rename` itself, so the session
// name is content the review header paints, not something the user typed.
func TestSessionNameCannotDriveTheTerminal(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "named", gitRepoWithTwoChangedFiles(t))
	m.width, m.height = 120, 40
	for i := range m.sessions {
		if m.sessions[i].ID == m.diff.sessID {
			m.sessions[i].Name = "rev\x1b]0;PWNED\x07iew"
		}
	}

	frame := m.viewDiffFull()
	if !strings.Contains(frame, "rev^[]0;PWNED^Giew") {
		t.Fatal("the session name should read as caret notation in the header")
	}
	if stray := strayControl(frame); stray != "" {
		t.Errorf("a session name leaks a control byte near %q", stray)
	}

	m.diff.notice = "sent review round 1 to rev\x1b]0;PWNED\x07iew"
	if stray := strayControl(m.viewDiffFooter()); stray != "" {
		t.Errorf("the review notice leaks a control byte near %q", stray)
	}
	m.diff.notice = ""
	m.errBar.text = "rename failed for rev\x1b]0;PWNED\x07iew"
	if stray := strayControl(m.statusMessage("✕", "●")); stray != "" {
		t.Errorf("the status bar leaks a control byte near %q", stray)
	}
}

// git.Driver.run embeds CombinedOutput, and the second line of an ambiguous
// -argument error is the one that says what to do about it. The review's empty
// state owns the rows to show it; paint would truncate a folded single row.
func TestMultiLineGitErrorKeepsItsLines(t *testing.T) {
	m := buildModel(t)
	m.diff.errText = "fatal: ambiguous argument 'x'\nUse '--' to separate paths from revisions"
	rows := strings.Split(m.diffEmptyText(), "\n")
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
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	// An OSC window-title set and a clear-screen, sequences our renderer
	// never emits, so finding them in the frame is unambiguous.
	payload := "const banner = \"\x1b]0;PWNED\x07\x1b[2J\x1b[31mgotcha\"\n"
	dir := gitRepoWithPayload(t, "payload.go", payload)
	openReviewOn(t, m, "escape", dir)
	m.width, m.height = 120, 40

	for _, layout := range []string{"unified", "split"} {
		frame := m.viewDiffFull()
		if !strings.Contains(frame, "PWNED") {
			t.Fatalf("%s: the payload line should be on screen to be a real test", layout)
		}
		if stray := strayControl(frame); stray != "" {
			t.Errorf("%s layout leaks a control byte to the terminal near %q", layout, stray)
		}
		m.diff.sideBySide = !m.diff.sideBySide
	}
}

// A clean repo proves the assertion above is not vacuous: the frame really
// does carry nothing but SGR, so a stray control byte means something leaked.
func TestCleanReviewFrameCarriesOnlySGR(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "clean", gitRepoWithTwoChangedFiles(t))
	m.width, m.height = 120, 40
	if stray := strayControl(m.viewDiffFull()); stray != "" {
		t.Fatalf("a clean review frame should be SGR only, found %q", stray)
	}
}

// Git is driven with -z, so a path is handed to us as raw bytes and reaches
// the file rail, the code title and the comment bar unquoted.
func TestReviewedPathCannotDriveTheTerminal(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := committedRepo(t)
	name := "w\x1b]0;P\x07.go"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("package a\n"), 0o644); err != nil {
		t.Skipf("filesystem rejects control bytes in a name: %v", err)
	}
	openReviewOn(t, m, "path", dir)
	m.width, m.height = 120, 40

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
