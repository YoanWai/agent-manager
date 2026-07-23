// Package diff builds whole-file line models for changed files: every
// line of the new file with deletions interleaved, ready to render with
// changed lines highlighted in full context.
package diff

import (
	"strings"
	"sync"

	"github.com/YoanWai/agent-manager/internal/git"
	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type LineKind uint8

const (
	Same LineKind = iota
	Add
	Del
)

// Span marks a changed byte range within a line's text.
type Span struct {
	Start, End int
}

type Line struct {
	Kind   LineKind
	OldNum int // 0 when the line only exists in the new file
	NewNum int // 0 when the line only exists in the old file
	Text   string
	Spans  []Span
	Pair   int // index of the counterpart Del/Add line; -1 when unpaired
}

type FileDiff struct {
	File      git.ChangedFile
	Stat      git.FileStat
	Lines     []Line
	OldTotal  int
	NewTotal  int
	Changes   []int // indices where add/del runs begin, for jump keys
	Binary    bool
	Truncated bool
	Err       error
	statKnown bool
	rows      []Row
}

type Set struct {
	Repo         git.Repo
	Scope        git.Scope
	BaseDesc     string
	BaseRef      string
	BaseOverride string
	Files        []FileDiff
}

const (
	maxFileBytes  = 1 << 20
	maxFileLines  = 10000
	maxEagerFiles = 200
	maxSpanLine   = 1000
	maxSpanBlock  = 200
)

// BuildSet loads and diffs every changed file for a scope. A non-empty
// baseOverride selects the ScopeBranch base and fails loudly when it no
// longer resolves; empty keeps auto-detection.
func BuildSet(driver *git.Driver, cwd string, scope git.Scope, baseOverride string) (Set, error) {
	repo, err := driver.OpenRepo(cwd)
	if err != nil {
		return Set{}, err
	}
	set := Set{Repo: repo, Scope: scope, BaseOverride: baseOverride}

	baseRef := ""
	switch scope {
	case git.ScopeBranch:
		var describe string
		baseRef, describe, err = driver.BranchBase(repo.Root, baseOverride)
		if err != nil {
			return Set{}, err
		}
		set.BaseDesc = describe
		set.BaseRef = baseRef
	case git.ScopeStaged, git.ScopeUncommitted:
		set.BaseDesc = "HEAD"
	case git.ScopeLastCommit:
		set.BaseDesc = "HEAD~1"
	}
	if repo.Unborn && scope != git.ScopeUncommitted && scope != git.ScopeStaged {
		return set, nil
	}

	var files []git.ChangedFile
	var stats map[string]git.FileStat
	var filesErr, statsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		files, filesErr = driver.ChangedFiles(repo.Root, scope, baseRef)
	}()
	go func() {
		defer wg.Done()
		stats, statsErr = driver.NumStat(repo.Root, scope, baseRef)
	}()
	wg.Wait()
	if filesErr != nil {
		return Set{}, filesErr
	}
	if statsErr != nil {
		return Set{}, statsErr
	}

	for i, file := range files {
		stat, known := stats[file.Path]
		fd := FileDiff{File: file, Stat: stat, statKnown: known}
		if !known && file.Status == git.Untracked {
			if err := countUnknownStat(driver, repo.Root, &fd); err != nil {
				fd.Err = err
				set.Files = append(set.Files, fd)
				continue
			}
		}
		if i < maxEagerFiles {
			loadFile(driver, repo.Root, scope, baseRef, &fd)
		}
		set.Files = append(set.Files, fd)
	}
	return set, nil
}

// EnsureFile lazily loads a file skipped past the eager cap.
func EnsureFile(driver *git.Driver, set *Set, index int) {
	if index < 0 || index >= len(set.Files) {
		return
	}
	fd := &set.Files[index]
	if fd.Lines != nil || fd.Binary || fd.Err != nil {
		return
	}
	baseRef := set.BaseRef
	loadFile(driver, set.Repo.Root, set.Scope, baseRef, fd)
}

func loadFile(driver *git.Driver, root string, scope git.Scope, baseRef string, fd *FileDiff) {
	oldContent, newContent, err := fileSides(driver, root, scope, baseRef, fd.File)
	if err != nil {
		fd.Err = err
		return
	}
	if git.IsBinary(oldContent) || git.IsBinary(newContent) {
		fd.Binary = true
		return
	}
	if len(oldContent) > maxFileBytes || len(newContent) > maxFileBytes {
		fd.Truncated = true
		return
	}
	known := fd.statKnown
	*fd = BuildFile(oldContent, newContent, fd.File, fd.Stat)
	fd.statKnown = known
}

// Counting raw bytes keeps untracked files correct past the diff model's caps.
func countUnknownStat(driver *git.Driver, root string, fd *FileDiff) error {
	count, err := driver.CountWorkingLines(root, fd.File.Path)
	if err != nil {
		return err
	}
	if !count.Counted {
		return nil
	}
	fd.statKnown = true
	fd.Stat = git.FileStat{Adds: count.Lines, Binary: count.Binary}
	fd.Binary = count.Binary
	return nil
}

// StatKnown reports whether Stat holds a real count rather than an unknown one.
func (fd *FileDiff) StatKnown() bool { return fd.statKnown }

func fileSides(driver *git.Driver, root string, scope git.Scope, baseRef string, file git.ChangedFile) (oldContent, newContent []byte, err error) {
	oldRef, newRef := "", ""
	switch scope {
	case git.ScopeUncommitted:
		oldRef = "HEAD"
	case git.ScopeStaged:
		oldRef, newRef = "HEAD", ":0"
	case git.ScopeLastCommit:
		oldRef, newRef = baseRef, "HEAD"
		if oldRef == "" {
			oldRef = driver.LastCommitParent(root)
		}
	case git.ScopeBranch:
		oldRef, newRef = baseRef, "HEAD"
	}

	if file.Status != git.Added && file.Status != git.Untracked {
		oldContent, err = driver.ShowFile(root, oldRef, file.OldPath)
		if err != nil {
			return nil, nil, err
		}
	}
	if file.Status != git.Deleted {
		if newRef == "" {
			newContent, err = driver.WorkingFile(root, file.Path)
		} else {
			newContent, err = driver.ShowFile(root, newRef, file.Path)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return oldContent, newContent, nil
}

// BuildFile diffs two file versions into the whole-file line model:
// every new-file line in order, with deleted old lines interleaved
// ahead of the lines that replaced them.
func BuildFile(oldContent, newContent []byte, file git.ChangedFile, stat git.FileStat) FileDiff {
	fd := FileDiff{File: file, Stat: stat}
	oldText, oldTruncated := capLines(string(oldContent))
	newText, newTruncated := capLines(string(newContent))
	fd.Truncated = oldTruncated || newTruncated

	edits := udiff.Lines(oldText, newText)
	unified, err := udiff.ToUnifiedDiff("a", "b", oldText, edits, maxFileLines*2)
	if err != nil {
		fd.Err = err
		return fd
	}

	oldNum, newNum := 0, 0
	for _, hunk := range unified.Hunks {
		for _, hunkLine := range hunk.Lines {
			text := strings.TrimSuffix(hunkLine.Content, "\n")
			text = strings.ReplaceAll(strings.TrimSuffix(text, "\r"), "\t", "    ")
			switch hunkLine.Kind {
			case udiff.Equal:
				oldNum++
				newNum++
				fd.Lines = append(fd.Lines, Line{Kind: Same, OldNum: oldNum, NewNum: newNum, Text: text, Pair: -1})
			case udiff.Delete:
				oldNum++
				fd.Lines = append(fd.Lines, Line{Kind: Del, OldNum: oldNum, Text: text, Pair: -1})
			case udiff.Insert:
				newNum++
				fd.Lines = append(fd.Lines, Line{Kind: Add, NewNum: newNum, Text: text, Pair: -1})
			}
		}
	}
	// A no-context diff of an unchanged file yields no hunks; equal-only
	// content still needs the full file present.
	if len(unified.Hunks) == 0 && newText != "" {
		for i, text := range strings.Split(strings.TrimSuffix(newText, "\n"), "\n") {
			clean := strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\t", "    ")
			fd.Lines = append(fd.Lines, Line{Kind: Same, OldNum: i + 1, NewNum: i + 1, Text: clean, Pair: -1})
		}
	}
	fd.OldTotal = oldNum
	fd.NewTotal = newNum
	pairBlocks(&fd)
	markChanges(&fd)
	return fd
}

func capLines(text string) (string, bool) {
	count := strings.Count(text, "\n")
	if count <= maxFileLines {
		return text, false
	}
	lines := strings.SplitAfterN(text, "\n", maxFileLines+1)
	return strings.Join(lines[:maxFileLines], ""), true
}

// pairBlocks matches runs of deletions with the additions that follow
// them, wiring Pair indices and computing intra-line word spans.
func pairBlocks(fd *FileDiff) {
	dmp := diffmatchpatch.New()
	i := 0
	for i < len(fd.Lines) {
		if fd.Lines[i].Kind != Del {
			i++
			continue
		}
		delStart := i
		for i < len(fd.Lines) && fd.Lines[i].Kind == Del {
			i++
		}
		addStart := i
		for i < len(fd.Lines) && fd.Lines[i].Kind == Add {
			i++
		}
		dels, adds := addStart-delStart, i-addStart
		pairs := dels
		if adds < pairs {
			pairs = adds
		}
		if pairs > maxSpanBlock {
			pairs = maxSpanBlock
		}
		for p := 0; p < pairs; p++ {
			delLine := &fd.Lines[delStart+p]
			addLine := &fd.Lines[addStart+p]
			delLine.Pair = addStart + p
			addLine.Pair = delStart + p
			if len(delLine.Text) <= maxSpanLine && len(addLine.Text) <= maxSpanLine {
				delLine.Spans, addLine.Spans = wordSpans(dmp, delLine.Text, addLine.Text)
			}
		}
	}
}

// wordSpans computes changed byte ranges on both sides of a modified
// line pair, dropped when most of the line changed anyway.
func wordSpans(dmp *diffmatchpatch.DiffMatchPatch, oldLine, newLine string) (oldSpans, newSpans []Span) {
	diffs := dmp.DiffCleanupSemantic(dmp.DiffMain(oldLine, newLine, false))
	oldOffset, newOffset := 0, 0
	oldChanged, newChanged := 0, 0
	for _, part := range diffs {
		size := len(part.Text)
		switch part.Type {
		case diffmatchpatch.DiffDelete:
			oldSpans = append(oldSpans, Span{oldOffset, oldOffset + size})
			oldOffset += size
			oldChanged += size
		case diffmatchpatch.DiffInsert:
			newSpans = append(newSpans, Span{newOffset, newOffset + size})
			newOffset += size
			newChanged += size
		default:
			oldOffset += size
			newOffset += size
		}
	}
	if len(oldLine) > 0 && oldChanged*10 > len(oldLine)*7 {
		oldSpans = nil
	}
	if len(newLine) > 0 && newChanged*10 > len(newLine)*7 {
		newSpans = nil
	}
	return oldSpans, newSpans
}

func markChanges(fd *FileDiff) {
	previous := Same
	for i, line := range fd.Lines {
		if line.Kind != Same && previous == Same {
			fd.Changes = append(fd.Changes, i)
		}
		previous = line.Kind
	}
}

// Row addresses one side-by-side display row; -1 means a blank cell.
type Row struct {
	Left, Right int
}

func (fd *FileDiff) SideBySideRows() []Row {
	if fd.rows != nil {
		return fd.rows
	}
	i := 0
	for i < len(fd.Lines) {
		line := fd.Lines[i]
		if line.Kind == Same {
			fd.rows = append(fd.rows, Row{i, i})
			i++
			continue
		}
		delStart := i
		for i < len(fd.Lines) && fd.Lines[i].Kind == Del {
			i++
		}
		addStart := i
		for i < len(fd.Lines) && fd.Lines[i].Kind == Add {
			i++
		}
		dels, adds := addStart-delStart, i-addStart
		rows := dels
		if adds > rows {
			rows = adds
		}
		for r := 0; r < rows; r++ {
			row := Row{-1, -1}
			if r < dels {
				row.Left = delStart + r
			}
			if r < adds {
				row.Right = addStart + r
			}
			fd.rows = append(fd.rows, row)
		}
	}
	if fd.rows == nil {
		fd.rows = []Row{}
	}
	return fd.rows
}
