package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
)

// The reviewed repo is code the user did not write. Our own rendering emits
// nothing but SGR colour sequences, so any other control byte in a painted
// frame is the repo driving the terminal.
func strayControl(frame string) string {
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
	}
	for in, want := range cases {
		if got := escapeControls(in); got != want {
			t.Errorf("escapeControls(%q) = %q, want %q", in, got, want)
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
	clean := []diff.Span{{Start: 1, End: 2}}
	if got := escapeSpans("abc", clean); &got[0] != &clean[0] {
		t.Fatal("a line with no control bytes should keep its spans untouched")
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
