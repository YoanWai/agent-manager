package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/charmbracelet/x/ansi"
)

func TestWrapTintedPreservesText(t *testing.T) {
	rows := wrapTinted("func main() {", nil, bgAdd, bgAddSpan, 40)
	if len(rows) != 1 {
		t.Fatalf("short line should be one row: %d", len(rows))
	}
	// A tinted row pads to width so the background fills the column.
	if got := strings.TrimRight(ansi.Strip(rows[0]), " "); got != "func main() {" {
		t.Fatalf("text mangled: %q", got)
	}
	if !strings.HasPrefix(rows[0], bgAdd) {
		t.Fatalf("background not opened: %q", rows[0])
	}
}

func TestWrapTintedReemitsBackgroundAfterReset(t *testing.T) {
	rows := wrapTinted("\x1b[38;5;197mfunc\x1b[0m main", nil, bgDel, bgDelSpan, 40)
	tinted := rows[0]
	if !strings.HasPrefix(ansi.Strip(tinted), "func main") {
		t.Fatalf("text mangled: %q", ansi.Strip(tinted))
	}
	resetAt := strings.Index(tinted, "\x1b[0m")
	if resetAt < 0 || !strings.HasPrefix(tinted[resetAt+4:], bgDel) {
		t.Fatalf("background must be re-opened after chroma reset: %q", tinted)
	}
}

func TestWrapTintedSpanSwitch(t *testing.T) {
	rows := wrapTinted("if t <= exp", []diff.Span{{Start: 5, End: 7}}, bgAdd, bgAddSpan, 40)
	if !strings.Contains(rows[0], bgAddSpan) {
		t.Fatalf("span background missing: %q", rows[0])
	}
	if !strings.HasPrefix(ansi.Strip(rows[0]), "if t <= exp") {
		t.Fatalf("text mangled: %q", ansi.Strip(rows[0]))
	}
}

func TestWrapTintedWordBoundary(t *testing.T) {
	line := "hello world foo bar"
	rows := wrapTinted(line, nil, "", "", 14)
	stripped := ansi.Strip(strings.Join(rows, ""))
	stripped = strings.TrimSpace(stripped)
	words := strings.Fields(stripped)
	if len(words) != 4 {
		t.Fatalf("expected 4 words, got %d: %q", len(words), words)
	}
	for _, w := range words {
		if !strings.Contains("hello world foo bar", w) {
			t.Fatalf("unexpected word fragment: %q", w)
		}
	}
}

func TestWrapTintedLongWordFallsBack(t *testing.T) {
	word := strings.Repeat("x", 60)
	line := "before " + word + " after"
	rows := wrapTinted(line, nil, "", "", 20)
	stripped := ansi.Strip(strings.Join(rows, ""))
	if !strings.Contains(stripped, "before") {
		t.Fatal("missing before")
	}
	if !strings.Contains(stripped, "after") {
		t.Fatal("missing after")
	}
	if xs := strings.Count(stripped, "x"); xs != 60 {
		t.Fatalf("expected 60 x's, got %d", xs)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 20 {
			t.Fatalf("row exceeds width: %d", w)
		}
	}
}

func TestWrapTintedWordWrapWithSpans(t *testing.T) {
	line := "if a > b return c"
	rows := wrapTinted(line, []diff.Span{{Start: 5, End: 6}}, bgAdd, bgAddSpan, 10)
	stripped := ansi.Strip(strings.Join(rows, ""))
	stripped = strings.ReplaceAll(stripped, " ", "")
	if stripped != "ifa>breturnc" {
		t.Fatalf("content lost or reordered: %q", stripped)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 10 {
			t.Fatalf("row exceeds width: %d row=%q", w, row)
		}
	}
}

func TestWrapTintedWordWrapNoSpaces(t *testing.T) {
	long := strings.Repeat("x", 100)
	rows := wrapTinted(long, nil, "", "", 20)
	if len(rows) != 5 {
		t.Fatalf("100 cols / 20 = 5 rows, got %d", len(rows))
	}
	var joined string
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 20 {
			t.Fatalf("row exceeds width: %d", w)
		}
		joined += ansi.Strip(row)
	}
	if joined != long {
		t.Fatalf("wrap lost content: %q", joined)
	}
}

func TestHighlightFileBothSides(t *testing.T) {
	fd := diff.BuildFile(
		[]byte("package a\n\nfunc A() {}\n"),
		[]byte("package a\n\nfunc A() int { return 1 }\n"),
		git.ChangedFile{Path: "a.go", OldPath: "a.go", Status: git.Modified}, git.FileStat{})
	hl := highlightFile(&fd)
	if hl == nil || len(hl.newLines) != 3 || len(hl.oldLines) != 3 {
		t.Fatalf("hl sides = %d/%d", len(hl.oldLines), len(hl.newLines))
	}
	if !strings.Contains(hl.newLines[0], "\x1b[") {
		t.Fatalf("go source should highlight: %q", hl.newLines[0])
	}
	for _, line := range fd.Lines {
		if ansi.Strip(hl.hlLine(line)) != line.Text {
			t.Fatalf("highlight text drifted: %q vs %q", ansi.Strip(hl.hlLine(line)), line.Text)
		}
	}
}

func TestHLCacheEvicts(t *testing.T) {
	cache := newHLCache()
	for i := 0; i < highlightCacheCap+3; i++ {
		cache.put(hlKey{path: string(rune('a' + i))}, &fileHL{})
	}
	if len(cache.entries) != highlightCacheCap {
		t.Fatalf("cache size = %d", len(cache.entries))
	}
}

// The review header always names the repo and its branch; in branch scope it
// shows the base and the branch it diffs into.
func TestReviewHeaderShowsRepoBranchAndBase(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "hdr", dir)

	header := m.viewDiffHeader("hdr")
	if !strings.Contains(header, filepath.Base(dir)) {
		t.Fatalf("header should name the repo, got %q", header)
	}
	if !strings.Contains(header, "main") {
		t.Fatalf("header should show the branch, got %q", header)
	}

	for m.diff.scope != git.ScopeBranch {
		m.drainCmds(t, m.cycleDiffScope())
	}
	header = m.viewDiffHeader("hdr")
	if !strings.Contains(header, "→ main") {
		t.Fatalf("branch scope header should show base → branch, got %q", header)
	}
}

// The target pill in the header is the one place a reviewer figures out
// what they are diffing into, so it has to read cleanly: the @<hash>
// suffix BaseDesc carries internally is dropped, each changeable pill
// wears its key, and an auto-detected target says so out loud while an
// explicitly set one does not.
func TestReviewHeaderTargetLabelCleanAndKeyed(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "hdr", dir)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	for m.diff.scope != git.ScopeBranch {
		m.drainCmds(t, m.cycleDiffScope())
	}

	header := ansi.Strip(m.viewDiffHeader("hdr"))
	if !strings.Contains(header, "B ") || !strings.Contains(header, "main") {
		t.Fatalf("target pill should wear its B key, got %q", header)
	}
	if strings.Contains(header, "@") {
		t.Fatalf("header should drop the @hash suffix from the target, got %q", header)
	}
	if !strings.Contains(header, "(auto)") {
		t.Fatalf("auto-detected target should be marked, got %q", header)
	}

	if err := m.store.SetReviewBase(sess.ID, m.diff.repoSel, "feature"); err != nil {
		t.Fatal(err)
	}
	m.diff.set.BaseOverride = "feature"
	m.diff.set.BaseDesc = "feature@deadbee"
	m.diff.set.Repo.Branch = "feature"
	header = ansi.Strip(m.viewDiffHeader("hdr"))
	if strings.Contains(header, "(auto)") {
		t.Fatalf("explicit target should not be marked auto, got %q", header)
	}
	if !strings.Contains(header, "feature → feature") {
		t.Fatalf("header should show the cleaned target → branch, got %q", header)
	}
}
