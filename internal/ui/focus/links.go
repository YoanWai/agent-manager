package focus

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

var paneLinkPattern = regexp.MustCompile(`https?://\S+`)

var hyperlinkSchemes = []string{"http", "https"}

// paneLink is one row's cells, by column, that open url. A written link's
// url is its own text, so a wrap carries the text on.
type paneLink struct {
	start, end int
	url        string
	written    bool
}

type wrapPiece struct {
	row, end int
}

func (m Model) linkAt(row, col int) string {
	if row < 0 || row >= len(m.frame.links) {
		return ""
	}
	for _, link := range m.frame.links[row] {
		if col >= link.start && col < link.end {
			return link.url
		}
	}
	return ""
}

// frameLinks finds what a click on each row opens: the targets of the
// pane's hyperlinks first, then URLs written out as text, then the rows a
// link wrapped onto from above.
func frameLinks(raw, plain []string, width int) [][]paneLink {
	links := make([][]paneLink, len(raw))
	own := make([]int, len(raw))
	for row := range raw {
		if strings.Contains(raw[row], "://") {
			links[row] = append(rowHyperlinks(raw[row]), writtenLinks(plain[row])...)
			own[row] = len(links[row])
		}
	}
	for row := range links {
		for i := range own[row] {
			link := &links[row][i]
			pieces, text := wrapPieces(plain, row, width, link.end)
			if link.written {
				link.url = trimLinkPunctuation(link.url + text)
			}
			for _, piece := range pieces {
				links[piece.row] = append(links[piece.row], paneLink{end: piece.end, url: link.url})
			}
		}
	}
	return links
}

func writtenLinks(line string) []paneLink {
	var links []paneLink
	for _, span := range paneLinkPattern.FindAllStringIndex(line, -1) {
		links = append(links, paneLink{
			start:   ansi.StringWidth(line[:span[0]]),
			end:     ansi.StringWidth(line[:span[1]]),
			url:     line[span[0]:span[1]],
			written: true,
		})
	}
	return links
}

// rowHyperlinks reads the OSC 8 hyperlinks capture-pane -e keeps, by column.
func rowHyperlinks(line string) []paneLink {
	var links []paneLink
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	var state byte
	open := paneLink{}
	col := 0
	closeLink := func() {
		if open.url != "" && col > open.start {
			open.end = col
			links = append(links, open)
		}
	}
	for len(line) > 0 {
		seq, width, n, next := ansi.DecodeSequence(line, state, parser)
		if ansi.HasOscPrefix(seq) && parser.Command() == 8 {
			closeLink()
			open = paneLink{start: col, url: openableTarget(string(parser.Data()))}
		}
		col += width
		line, state = line[n:], next
	}
	closeLink()
	return links
}

// openableTarget is the URI of an OSC 8 payload ("8;params;uri") when it
// is a web address a click may open. The payload is the agent's output, so a
// control or space rune in it is refused before it reaches the opener.
func openableTarget(payload string) string {
	parts := strings.SplitN(payload, ";", 3)
	if len(parts) < 3 || strings.IndexFunc(parts[2], func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) >= 0 {
		return ""
	}
	target, err := url.Parse(parts[2])
	if err != nil || target.Host == "" || !slices.Contains(hyperlinkSchemes, strings.ToLower(target.Scheme)) {
		return ""
	}
	return parts[2]
}

// wrapPieces follows a link that runs to the edge of a full-width row onto
// the leading text of the rows below, the way a terminal wraps a long line,
// and returns that text too.
func wrapPieces(plain []string, row, width, end int) ([]wrapPiece, string) {
	var pieces []wrapPiece
	var text string
	for next := row + 1; next < len(plain); next++ {
		if end != ansi.StringWidth(plain[next-1]) || end < width {
			break
		}
		rest := plain[next]
		if rest == "" || strings.HasPrefix(rest, " ") {
			break
		}
		fields := strings.Fields(rest)
		end = ansi.StringWidth(fields[0])
		pieces = append(pieces, wrapPiece{row: next, end: end})
		text += fields[0]
		if len(fields) > 1 {
			break
		}
	}
	return pieces, text
}

func trimLinkPunctuation(url string) string {
	url = strings.TrimRight(url, `.,;:!?'"`)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"<", ">"}} {
		for strings.HasSuffix(url, pair[1]) && strings.Count(url, pair[1]) > strings.Count(url, pair[0]) {
			url = strings.TrimSuffix(url, pair[1])
			url = strings.TrimRight(url, `.,;:!?'"`)
		}
	}
	return url
}
