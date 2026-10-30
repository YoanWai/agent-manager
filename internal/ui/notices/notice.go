// Package notices is the messages panel: the notices it builds from release,
// feed and setup facts, its cursor and body scroll, its keys and its frame.
// The root fetches releases and the feed, persists dismissals, applies
// updates and opens links; it hands the panel copied facts and runs what
// its keys ask for.
package notices

import (
	"strings"

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

type Notice struct {
	ID            string
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

type Release struct {
	Version      string
	URL          string
	Headline     string
	Summary      string
	Highlights   []string
	Thanks       []string
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
	Title    string
	Headline string
	Body     []string
	// Accent holds phrases of Body to draw in the accent color, never an empty one.
	Accent []string
	URL    string
}
