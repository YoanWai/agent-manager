package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type textRun struct {
	text   string
	accent bool
}

func markedRuns(text string) []textRun {
	var runs []textRun
	for index, part := range strings.Split(text, "`") {
		if part != "" {
			runs = append(runs, textRun{text: part, accent: index%2 == 1})
		}
	}
	return runs
}

// Where two phrases start at one place the longer wins.
// An empty phrase would loop forever here, and feed.sanitize drops them.
func phraseRuns(text string, phrases []string) []textRun {
	var runs []textRun
	for text != "" {
		at, hit := -1, ""
		for _, phrase := range phrases {
			found := strings.Index(text, phrase)
			if found < 0 {
				continue
			}
			if at < 0 || found < at || (found == at && len(phrase) > len(hit)) {
				at, hit = found, phrase
			}
		}
		if at < 0 {
			break
		}
		if at > 0 {
			runs = append(runs, textRun{text: text[:at]})
		}
		runs = append(runs, textRun{text: hit, accent: true})
		text = text[at+len(hit):]
	}
	if text != "" {
		runs = append(runs, textRun{text: text})
	}
	return runs
}

type runWord struct {
	text   string
	accent bool
	// gap counts the spaces written before the word, kept when it shares a row.
	gap int
	// glued is true when no row may break before the word: it follows the one before with no space.
	glued bool
}

func runWords(runs []textRun) []runWord {
	var words []runWord
	gap, field := 0, ""
	for _, run := range runs {
		rest := run.text
		for rest != "" {
			trimmed := strings.TrimLeft(rest, " ")
			if trimmed != rest {
				gap += len(rest) - len(trimmed)
				field = ""
			}
			end := strings.IndexByte(trimmed, ' ')
			if end < 0 {
				end = len(trimmed)
			}
			for _, piece := range strings.SplitAfter(trimmed[:end], "-") {
				if piece == "" {
					continue
				}
				glued := field != "" && !breaksAfterHyphen(field, piece)
				words = append(words, runWord{text: piece, accent: run.accent, gap: gap, glued: glued})
				field += piece
				gap = 0
			}
			rest = trimmed[end:]
		}
	}
	return words
}

func breaksAfterHyphen(field, next string) bool {
	before, found := strings.CutSuffix(field, "-")
	if !found || before == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(before)
	first, _ := utf8.DecodeRuneInString(next)
	return wordRune(last) && wordRune(first)
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Spacing inside a row stays as written so column-aligned lines survive.
func wrapRuns(runs []textRun, width int) [][]textRun {
	var lines [][]textRun
	var line []textRun
	used := 0
	words := runWords(runs)
	for index, word := range words {
		cells := ansi.StringWidth(word.text)
		if index > 0 && !word.glued && used+word.gap+gluedWidth(words[index:]) > width {
			lines = append(lines, line)
			line, used = nil, 0
		} else if word.gap > 0 {
			between := word.accent && len(line) > 0 && line[len(line)-1].accent
			line = appendRun(line, strings.Repeat(" ", word.gap), between)
			used += word.gap
		}
		line = appendRun(line, word.text, word.accent)
		used += cells
	}
	if line != nil {
		lines = append(lines, line)
	}
	return lines
}

func gluedWidth(words []runWord) int {
	cells := ansi.StringWidth(words[0].text)
	for _, word := range words[1:] {
		if !word.glued {
			break
		}
		cells += ansi.StringWidth(word.text)
	}
	return cells
}

func appendRun(line []textRun, text string, accent bool) []textRun {
	if last := len(line) - 1; last >= 0 && line[last].accent == accent {
		line[last].text += text
		return line
	}
	return append(line, textRun{text: text, accent: accent})
}

func renderRuns(line []textRun, base lipgloss.Style) string {
	var rendered strings.Builder
	for _, run := range line {
		if run.accent {
			rendered.WriteString(keyStyle.Render(run.text))
		} else {
			rendered.WriteString(base.Render(run.text))
		}
	}
	return rendered.String()
}
