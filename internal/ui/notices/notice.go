// Package notices is the messages panel: the notices it builds from release,
// feed and setup facts, its cursor and body scroll, its keys and its frame.
// The root fetches releases and the feed, persists dismissals, applies
// updates and opens links; it hands the panel copied facts and runs what
// its keys ask for.
package notices

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// IDs of the notices that ship in the binary.
const (
	ConfigNotImported = "config-not-imported"
)

const RepoURL = "https://github.com/YoanWai/agent-manager"

type Tint uint8

const (
	TintAccent Tint = iota
	TintAccent2
	TintWarm
)

const warmHex = "#e2c044"

// Kind groups notices in the list, in the order the groups are shown.
type Kind uint8

const (
	KindRelease Kind = iota
	KindNews
	KindHeadsUp
	KindTip
)

type Notice struct {
	ID            string
	Kind          Kind
	Subtitle      string
	Glyph         string
	Tint          Tint
	Title         string
	Headline      string
	Accent        []string
	Body          []string
	ReleaseNotes  bool
	Releases      []Release
	After         []string
	RangeComplete bool
	URL           string
	Action        string
	Expires       time.Time
}

func (n Notice) IsUpdate() bool {
	return strings.HasPrefix(n.ID, "update-")
}

func (n Notice) mark(t Theme) string {
	color := lipgloss.Color(warmHex)
	switch n.Tint {
	case TintAccent:
		color = t.Accent
	case TintAccent2:
		color = t.Accent2
	}
	return lipgloss.NewStyle().Foreground(color).Render(n.Glyph)
}

// Change kinds, as the release catalog files them.
const (
	KindFeature = "feat"
	KindFix     = "fix"
	KindOther   = "other"
)

type Change struct {
	Kind   string
	Scope  string
	Text   string
	Author string
}

func (c Change) label() string {
	if c.Scope == "" {
		return c.Text
	}
	return c.Scope + ": " + c.Text
}

// Thanks groups, as the release catalog files them.
const (
	ThanksBuilt   = "built"
	ThanksFound   = "found"
	ThanksStarted = "started"
)

type Highlight struct {
	Label string
	Text  string
}

func (h Highlight) line() string {
	return joinWords(h.Label, h.Text)
}

type Thanks struct {
	Handle string
	Group  string
	Text   string
	Refs   []string
}

func (t Thanks) line() string {
	line := joinWords(t.Handle, t.Text)
	if len(t.Refs) > 0 {
		line += " (" + strings.Join(t.Refs, ", ") + ")"
	}
	return line
}

func joinWords(words ...string) string {
	var present []string
	for _, word := range words {
		if word != "" {
			present = append(present, word)
		}
	}
	return strings.Join(present, " ")
}

type Release struct {
	Version      string
	URL          string
	PublishedAt  time.Time
	Headline     string
	Summary      string
	Highlights   []Highlight
	Thanks       []Thanks
	Changes      []Change
	TotalChanges int
}

// ReleaseRange is the releases between two versions, oldest first.
// Complete is false when the local catalog covers only part of it.
type ReleaseRange struct {
	Releases []Release
	Complete bool
}

type Message struct {
	ID       string
	Kind     Kind
	Title    string
	Banner   string
	Headline string
	Body     []string
	// Accent holds phrases of Body to draw in the accent color, never an empty one.
	Accent  []string
	URL     string
	Action  string
	Expires time.Time
}
