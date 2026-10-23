package review

import (
	"strings"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const maxHighlightBytes = 256 << 10

var (
	chromaStyle     = brightCommentStyle()
	chromaFormatter = formatters.Get("terminal256")
)

// brightCommentStyle lifts monokai's dim comment color so source comments
// stay legible over the diff background tints.
func brightCommentStyle() *chroma.Style {
	style, err := styles.Get("monokai").Builder().Add(chroma.Comment, "#b0af99").Build()
	if err != nil {
		return styles.Get("monokai")
	}
	return style
}

// HighlightFile syntax-highlights both sides of a file diff. Deleted
// lines highlight from the old file version so their coloring is exact.
func HighlightFile(fd *diff.FileDiff) *Highlight {
	oldText, newText := sideTexts(fd)
	if len(oldText)+len(newText) > maxHighlightBytes {
		return NewHighlight(nil)
	}
	lexer := lexers.Match(fd.File.Path)
	if lexer == nil {
		lexer = lexers.Analyse(newText)
	}
	if lexer == nil {
		return NewHighlight(nil)
	}
	lexer = chroma.Coalesce(lexer)
	return NewHighlight(alignSides(fd, highlightSide(lexer, oldText), highlightSide(lexer, newText)))
}

// alignSides walks the model in the order sideTexts wrote the two sides,
// pairing every line with its highlighted text.
func alignSides(fd *diff.FileDiff, oldLines, newLines []string) []string {
	lines := make([]string, len(fd.Lines))
	oldIdx, newIdx := 0, 0
	for i, line := range fd.Lines {
		switch line.Kind {
		case diff.Gap:
			continue
		case diff.Del:
			if oldIdx < len(oldLines) {
				lines[i] = oldLines[oldIdx]
			}
			oldIdx++
		case diff.Add:
			if newIdx < len(newLines) {
				lines[i] = newLines[newIdx]
			}
			newIdx++
		default:
			if newIdx < len(newLines) {
				lines[i] = newLines[newIdx]
			}
			oldIdx++
			newIdx++
		}
	}
	return lines
}

func sideTexts(fd *diff.FileDiff) (oldText, newText string) {
	var oldBuilder, newBuilder strings.Builder
	for _, line := range fd.Lines {
		if line.Kind == diff.Gap {
			continue
		}
		text := presentation.EscapeControls(line.Text)
		if line.Kind != diff.Add {
			oldBuilder.WriteString(text)
			oldBuilder.WriteByte('\n')
		}
		if line.Kind != diff.Del {
			newBuilder.WriteString(text)
			newBuilder.WriteByte('\n')
		}
		if oldBuilder.Len()+newBuilder.Len() > maxHighlightBytes {
			break
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
