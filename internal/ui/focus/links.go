package focus

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

var paneLinkPattern = regexp.MustCompile(`https?://\S+`)

// paneLink is one row's cells, by column, that open url. A written link's
// url is its own text, so a wrap carries the text on. A hyperlink's label is
// the text it covers on its first row, and a shown link's text is its url,
// so a reader sees where it goes.
type paneLink struct {
	start, end int
	url        string
	label      string
	written    bool
	shown      bool
}

type wrapPiece struct {
	row, end int
}

// PaneLinks is where the links of a pane painted on screen sit.
type PaneLinks struct {
	box   Box
	links [][]paneLink
}

// ReadPaneLinks finds the links in the captured rows a pane painted at box.
func ReadPaneLinks(rows []string, box Box) PaneLinks {
	safe, plain := paneText(rows)
	return PaneLinks{box: box, links: frameLinks(safe, plain, box.Width)}
}

// ShownAt is the link at screen cell x, y when its text is its own address,
// so a click there goes where the reader expects without a hover first.
func (p PaneLinks) ShownAt(x, y int) string {
	row, col := y-p.box.Y, x-p.box.X
	if !p.box.Valid || row < 0 || row >= len(p.links) || col < 0 || col >= p.box.Width {
		return ""
	}
	if link := linkIn(p.links[row], col); link.shown {
		return link.url
	}
	return ""
}

func (m Model) linkAt(row, col int) string {
	if row < 0 || row >= len(m.frame.links) {
		return ""
	}
	return linkIn(m.frame.links[row], col).url
}

func linkIn(links []paneLink, col int) paneLink {
	for _, link := range links {
		if col >= link.start && col < link.end {
			return link
		}
	}
	return paneLink{}
}

// paneText is each captured row with the escapes that move the cursor
// removed, and the same row as plain text.
func paneText(rows []string) (safe, plain []string) {
	safe = make([]string, len(rows))
	plain = make([]string, len(rows))
	for i, row := range rows {
		safe[i] = safePaneLine(row)
		plain[i] = ansi.Strip(safe[i])
	}
	return safe, plain
}

// frameLinks finds what a click on each row opens: the targets of the
// pane's hyperlinks first, then URLs written out as text, then the rows a
// link wrapped onto from above.
func frameLinks(raw, plain []string, width int) [][]paneLink {
	links := make([][]paneLink, len(raw))
	own := make([]int, len(raw))
	for row := range raw {
		if strings.Contains(raw[row], "://") {
			links[row] = append(rowHyperlinks(raw[row], plain[row]), writtenLinks(plain[row])...)
			own[row] = len(links[row])
		}
	}
	for row := range links {
		for i := range own[row] {
			link := &links[row][i]
			pieces, text := wrapPieces(plain, row, width, link.end)
			if link.written {
				link.url = trimLinkPunctuation(link.url + text)
			} else {
				link.shown = strings.TrimSpace(link.label+text) == link.url
			}
			for _, piece := range pieces {
				links[piece.row] = append(links[piece.row], paneLink{end: piece.end, url: link.url, shown: link.shown})
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
			shown:   true,
		})
	}
	return links
}

// rowHyperlinks reads the OSC 8 hyperlinks capture-pane -e keeps, by column,
// from a row and the same row as plain text.
func rowHyperlinks(line, plain string) []paneLink {
	var links []paneLink
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	var state byte
	open := paneLink{}
	col := 0
	closeLink := func() {
		if open.url != "" && col > open.start {
			open.end = col
			startByte, endByte := graphemeRangeAtColumns(plain, open.start, open.end)
			open.label = plain[startByte:endByte]
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
// is a web address or a file on this machine. The payload is the agent's
// output, so a control or space rune in it is refused before it reaches
// the opener.
func openableTarget(payload string) string {
	parts := strings.SplitN(payload, ";", 3)
	if len(parts) < 3 || strings.IndexFunc(parts[2], func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) >= 0 {
		return ""
	}
	target, err := url.Parse(parts[2])
	if err != nil {
		return ""
	}
	switch strings.ToLower(target.Scheme) {
	case "http", "https":
		if target.Host == "" {
			return ""
		}
	case "file":
		if (target.Host != "" && target.Host != "localhost") || !strings.HasPrefix(target.Path, "/") || strings.HasPrefix(target.Path, "//") {
			return ""
		}
	default:
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
		if first, _ := utf8.DecodeRuneInString(rest); rest == "" || unicode.IsSpace(first) {
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
