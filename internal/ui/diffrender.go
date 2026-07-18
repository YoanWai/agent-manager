package ui

import (
	"hash/fnv"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxHighlightBytes = 256 << 10
	highlightCacheCap = 8

	bgAdd     = "\x1b[48;5;22m"
	bgDel     = "\x1b[48;5;52m"
	bgAddSpan = "\x1b[48;5;28m"
	bgDelSpan = "\x1b[48;5;88m"
)

var (
	chromaStyle     = styles.Get("monokai")
	chromaFormatter = formatters.Get("terminal256")
)

// fileHL holds one file's syntax-highlighted lines per side, indexed by
// OldNum/NewNum minus one.
type fileHL struct {
	oldLines []string
	newLines []string
}

type hlKey struct {
	sessID string
	scope  git.Scope
	path   string
	hash   uint64
}

type hlCache struct {
	entries map[hlKey]*fileHL
	order   []hlKey
}

func newHLCache() *hlCache {
	return &hlCache{entries: map[hlKey]*fileHL{}}
}

func (c *hlCache) get(key hlKey) *fileHL {
	return c.entries[key]
}

func (c *hlCache) put(key hlKey, hl *fileHL) {
	if _, ok := c.entries[key]; !ok {
		c.order = append(c.order, key)
		if len(c.order) > highlightCacheCap {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.entries[key] = hl
}

func contentHash(fd *diff.FileDiff) uint64 {
	hash := fnv.New64a()
	for _, line := range fd.Lines {
		hash.Write([]byte{byte(line.Kind)})
		hash.Write([]byte(line.Text))
		hash.Write([]byte{'\n'})
	}
	return hash.Sum64()
}

// highlightFile syntax-highlights both sides of a file diff. Deleted
// lines highlight from the old file version so their coloring is exact.
func highlightFile(fd *diff.FileDiff) *fileHL {
	oldText, newText := sideTexts(fd)
	if len(oldText)+len(newText) > maxHighlightBytes {
		return &fileHL{}
	}
	lexer := lexers.Match(fd.File.Path)
	if lexer == nil {
		lexer = lexers.Analyse(newText)
	}
	if lexer == nil {
		return &fileHL{}
	}
	lexer = chroma.Coalesce(lexer)
	return &fileHL{
		oldLines: highlightSide(lexer, oldText),
		newLines: highlightSide(lexer, newText),
	}
}

func sideTexts(fd *diff.FileDiff) (oldText, newText string) {
	var oldBuilder, newBuilder strings.Builder
	for _, line := range fd.Lines {
		if line.Kind != diff.Add {
			oldBuilder.WriteString(line.Text)
			oldBuilder.WriteByte('\n')
		}
		if line.Kind != diff.Del {
			newBuilder.WriteString(line.Text)
			newBuilder.WriteByte('\n')
		}
	}
	return oldBuilder.String(), newBuilder.String()
}

func highlightSide(lexer chroma.Lexer, text string) []string {
	if text == "" {
		return nil
	}
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	tokenLines := chroma.SplitTokensIntoLines(iterator.Tokens())
	lines := make([]string, 0, len(tokenLines))
	var builder strings.Builder
	for _, tokens := range tokenLines {
		builder.Reset()
		if err := chromaFormatter.Format(&builder, chromaStyle, chroma.Literator(tokens...)); err != nil {
			return nil
		}
		lines = append(lines, strings.TrimRight(builder.String(), "\n"))
	}
	return lines
}

// hlLine returns the highlighted text for a diff line, falling back to
// the raw text when highlighting is unavailable.
func (hl *fileHL) hlLine(line diff.Line) string {
	if hl != nil {
		if line.Kind == diff.Del {
			if line.OldNum >= 1 && line.OldNum <= len(hl.oldLines) {
				return hl.oldLines[line.OldNum-1]
			}
		} else if line.NewNum >= 1 && line.NewNum <= len(hl.newLines) {
			return hl.newLines[line.NewNum-1]
		}
	}
	return line.Text
}

// wrapTinted overlays a diff background onto a chroma-highlighted line
// and wraps it to width, returning one entry per visual row. The diff
// background is re-emitted after every SGR reset chroma writes so it
// survives across tokens and across wrap boundaries; word spans (byte
// offsets into the raw text) switch to the brighter span background.
// When a background is set, each row is padded so the tint fills the
// full width; a plain line (empty baseBg) is left unpadded for the
// caller to pad. Every row is closed with a reset.
func wrapTinted(highlighted string, spans []diff.Span, baseBg, spanBg string, width int) []string {
	if width < 1 {
		width = 1
	}
	bgFor := func(offset int) string {
		for _, span := range spans {
			if offset >= span.Start && offset < span.End {
				return spanBg
			}
		}
		return baseBg
	}

	var rows []string
	var b strings.Builder
	activeBg := ""
	rowWidth := 0
	fresh := true

	closeRow := func() {
		if baseBg != "" && rowWidth < width {
			b.WriteString(strings.Repeat(" ", width-rowWidth))
		}
		if b.Len() > 0 || baseBg != "" {
			b.WriteString("\x1b[0m")
		}
		rows = append(rows, b.String())
		b.Reset()
		fresh = true
		rowWidth = 0
	}

	offset := 0
	for i := 0; i < len(highlighted); {
		if fresh {
			activeBg = bgFor(offset)
			if activeBg != "" {
				b.WriteString(activeBg)
			}
			fresh = false
		}
		if highlighted[i] == 0x1b {
			end := i + 1
			if end < len(highlighted) && highlighted[end] == '[' {
				end++
				for end < len(highlighted) && highlighted[end] != 'm' {
					end++
				}
				if end < len(highlighted) {
					end++
				}
			}
			sequence := highlighted[i:end]
			b.WriteString(sequence)
			if sequence == "\x1b[0m" && activeBg != "" {
				b.WriteString(activeBg)
			}
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(highlighted[i:])
		runeWidth := ansi.StringWidth(string(r))
		if rowWidth+runeWidth > width {
			closeRow()
			continue
		}
		if bg := bgFor(offset); bg != activeBg {
			activeBg = bg
			b.WriteString(bg)
		}
		b.WriteString(highlighted[i : i+size])
		offset += size
		rowWidth += runeWidth
		i += size
	}
	if !fresh || len(rows) == 0 {
		closeRow()
	}
	return rows
}
