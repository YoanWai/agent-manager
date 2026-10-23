package model

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/git/value"
	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/sergi/go-diff/diffmatchpatch"
	"strings"
	"unicode/utf8"
)

type LineKind uint8

const (
	Same LineKind = iota
	Add
	Del
	Gap // a marker standing in for lines the hunk model left out
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
	File      value.ChangedFile
	Stat      value.FileStat
	Lines     []Line
	OldTotal  int
	NewTotal  int
	Changes   []int // indices where add/del runs begin, for jump keys
	Binary    bool
	Truncated bool
	Err       error
	HasStat   bool `json:"-"`
	IsLoaded  bool `json:"-"`
	rows      []Row
}

type Set struct {
	Repo         value.Repo
	Scope        value.Scope
	BaseDesc     string
	BaseRef      string
	BaseOverride string
	Files        []FileDiff
}

const (
	// Past either threshold the whole-file model would be too big to build
	// or to scroll, so the file falls back to changed hunks with a little
	// context around them.
	maxWholeFileBytes = 1 << 20
	maxWholeFileLines = 10000

	maxHunkModelLines = 10000
	maxHunkLineBytes  = 4096
	hunkContext       = 3

	maxSpanLine  = 1000
	maxSpanBlock = 200
)

// StatKnown reports whether Stat holds a real count rather than an unknown one.
func (fd *FileDiff) StatKnown() bool { return fd.HasStat }

func (fd *FileDiff) Loaded() bool { return fd.IsLoaded }

func BuildFile(oldContent, newContent []byte, file value.ChangedFile, stat value.FileStat) FileDiff {
	fd := FileDiff{File: file, Stat: stat, IsLoaded: true}
	oldText, newText := string(oldContent), string(newContent)
	fd.OldTotal, fd.NewTotal = countLines(oldText), countLines(newText)

	hunksOnly := len(oldText) > maxWholeFileBytes || len(newText) > maxWholeFileBytes ||
		fd.OldTotal > maxWholeFileLines || fd.NewTotal > maxWholeFileLines
	context := maxWholeFileLines * 2
	if hunksOnly {
		context = hunkContext
	}

	edits := udiff.Lines(oldText, newText)
	unified, err := udiff.ToUnifiedDiff("a", "b", oldText, edits, context)
	if err != nil {
		fd.Err = err
		return fd
	}

	reached := 0
hunks:
	for _, hunk := range unified.Hunks {
		if skipped := hunk.ToLine - 1 - reached; hunksOnly && skipped > 0 {
			fd.Lines = append(fd.Lines, gapLine(fmt.Sprintf("⋯ %d unchanged lines", skipped)))
		}
		oldNum, newNum := hunk.FromLine-1, hunk.ToLine-1
		for _, hunkLine := range hunk.Lines {
			if hunksOnly && len(fd.Lines) >= maxHunkModelLines {
				fd.Lines = append(fd.Lines[:maxHunkModelLines], gapLine("⋯ diff truncated"))
				fd.Truncated = true
				break hunks
			}
			text := strings.TrimSuffix(hunkLine.Content, "\n")
			text = strings.ReplaceAll(strings.TrimSuffix(text, "\r"), "\t", "    ")
			if hunksOnly {
				text = capLine(text)
			}
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
		reached = newNum
	}
	if skipped := fd.NewTotal - reached; hunksOnly && !fd.Truncated && skipped > 0 {
		fd.Lines = append(fd.Lines, gapLine(fmt.Sprintf("⋯ %d unchanged lines", skipped)))
	}
	// Unchanged small files still show their full content.
	if !hunksOnly && len(unified.Hunks) == 0 && newText != "" {
		for i, text := range strings.Split(strings.TrimSuffix(newText, "\n"), "\n") {
			clean := strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\t", "    ")
			fd.Lines = append(fd.Lines, Line{Kind: Same, OldNum: i + 1, NewNum: i + 1, Text: clean, Pair: -1})
		}
	}
	pairBlocks(&fd)
	markChanges(&fd)
	return fd
}

func gapLine(text string) Line { return Line{Kind: Gap, Text: text, Pair: -1} }

func countLines(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}

// capLine keeps one minified or data line from wrapping into thousands of
// visual rows.
func capLine(text string) string {
	if len(text) <= maxHunkLineBytes {
		return text
	}
	cut := maxHunkLineBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
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
		if line.Kind == Gap {
			previous = Same
			continue
		}
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
		if line.Kind != Add && line.Kind != Del {
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
