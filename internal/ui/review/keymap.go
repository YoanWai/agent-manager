package review

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// action is what a Review key does. Key switches on it, so the work stays
// readable as ordinary code while the keys, labels and help text live in
// keymap alone.
type action uint8

const (
	actComment action = iota + 1
	actDiscard
	actSend
	actReviewed
	actScope
	actRepo
	actBranch
	actTarget
	actLineUp
	actLineDown
	actHalfDown
	actHalfUp
	actPageUp
	actPageDown
	actTop
	actBottom
	actNextFile
	actPrevFile
	actNextChange
	actPrevChange
	actLayout
	actCodeOnly
	actOpenFile
	actHelp
	actClose
)

// binding is one of Review's keys, said once. Key dispatches on Keys, the
// footer draws Legend and the ? map draws Help, so a key cannot be handled
// without being named, or named without being handled.
type binding struct {
	Keys   []string
	Action action
	// Legend is what the footer pair shows for the keys: a glyph, or several
	// keys folded into one pair. Empty uses the keys joined with "/".
	Legend string
	// Help is the ? map's key column. Empty uses Legend.
	Help string
	// Footer is the pair's footer label, computed from the review's state.
	// Nil leaves the binding out of the footer: its pair is a neighbour's.
	Footer func(*Model) string
	// Long is the ? map's description. Empty leaves it out of the map.
	Long string
	// Move puts the pair in the quiet Move tier instead of Review.
	Move bool
}

func label(text string) func(*Model) string { return func(*Model) string { return text } }

// keymap is every key Review answers to outside a draft or a send prompt,
// in the order the footer and the ? map show them.
var keymap = []binding{
	{Keys: []string{"c"}, Action: actComment, Footer: label("comment"), Long: "comment on the line"},
	{Keys: []string{"d"}, Action: actDiscard, Footer: label("remove/handle"), Long: "remove a draft, or mark sent feedback handled / open"},
	{Keys: []string{"C"}, Action: actSend, Footer: sendLabel, Long: "send drafts to the agent as the next review round"},
	{Keys: []string{"space", " "}, Action: actReviewed, Legend: "space", Footer: label("reviewed"), Long: "mark the file reviewed"},
	{Keys: []string{"s"}, Action: actScope, Footer: func(m *Model) string { return "scope: " + m.scope.String() },
		Long: "cycle the scope (uncommitted / vs target / last commit / staged)"},
	{Keys: []string{"r"}, Action: actRepo, Footer: repoLabel, Long: "pick the repo when the session dir holds several"},
	{Keys: []string{"b"}, Action: actBranch, Footer: label("branch"), Long: "switch to another worktree, listed by branch"},
	{Keys: []string{"B"}, Action: actTarget, Footer: label("target"), Long: "pick the target branch the branch diff compares against"},
	{Keys: []string{"up", "k"}, Action: actLineUp, Legend: "↑↓/jk", Help: "↑↓ / jk", Move: true, Footer: label("scroll line"), Long: "move a line"},
	{Keys: []string{"down", "j"}, Action: actLineDown},
	{Keys: []string{"ctrl+d"}, Action: actHalfDown, Legend: "ctrl+d/ctrl+u", Move: true, Footer: label("half page"), Long: "half a page"},
	{Keys: []string{"ctrl+u"}, Action: actHalfUp},
	{Keys: []string{"pgup"}, Action: actPageUp, Legend: "pgup/pgdn", Move: true, Footer: label("page"), Long: "page"},
	{Keys: []string{"pgdn", "pgdown"}, Action: actPageDown},
	{Keys: []string{"g"}, Action: actTop, Legend: "g/G", Move: true, Footer: label("top/bottom"), Long: "top / bottom of the file"},
	{Keys: []string{"G"}, Action: actBottom},
	{Keys: []string{"tab", "J"}, Action: actNextFile, Legend: "tab/J K/shift+tab", Help: "tab / shift+tab", Move: true, Footer: label("file"),
		Long: "next / previous file (J / K too)"},
	{Keys: []string{"shift+tab", "K"}, Action: actPrevFile},
	{Keys: []string{"n"}, Action: actNextChange, Legend: "n/N", Move: true, Footer: label("change"), Long: "next / previous change"},
	{Keys: []string{"N", "shift+n"}, Action: actPrevChange},
	{Keys: []string{"u"}, Action: actLayout, Move: true, Footer: label("layout"), Long: "unified / split layout"},
	{Keys: []string{"f"}, Action: actCodeOnly, Move: true, Footer: filterLabel, Long: "code files only (hides images, assets, lock files)"},
	{Keys: []string{"o", "f3"}, Action: actOpenFile, Move: true, Footer: label("open file"), Long: "open the current file in your editor"},
	{Keys: []string{"?"}, Action: actHelp, Move: true, Footer: label("keys"), Long: "this key map"},
	{Keys: []string{"esc", "q"}, Action: actClose, Move: true, Footer: label("close"), Long: "close the review"},
}

// quitPair is the one footer pair Review names but does not handle: root
// decides ctrl+c for every surface.
var quitPair = [2]string{"ctrl+c", "quit"}

func sendLabel(m *Model) string {
	if count := m.DraftCount(); count > 0 {
		return fmt.Sprintf("send %d", count)
	}
	return "send"
}

func repoLabel(m *Model) string {
	if len(m.repoRoots) > 0 {
		return "repo: " + presentation.EscapeControlsInline(filepath.Base(m.repoSel))
	}
	return "repo"
}

func filterLabel(m *Model) string {
	if m.codeOnly {
		return "all files"
	}
	return "code only"
}

// bindingFor is the binding a key name runs, if Review answers to it.
func bindingFor(key string) (binding, bool) {
	for _, b := range keymap {
		for _, k := range b.Keys {
			if k == key {
				return b, true
			}
		}
	}
	return binding{}, false
}

func (b binding) legendKey() string {
	if b.Legend != "" {
		return b.Legend
	}
	return strings.Join(b.Keys, "/")
}

func (b binding) helpKey() string {
	if b.Help != "" {
		return b.Help
	}
	return strings.ReplaceAll(b.legendKey(), "/", " / ")
}

func HelpRows() [][2]string {
	var rows [][2]string
	for _, b := range keymap {
		if b.Long != "" {
			rows = append(rows, [2]string{b.helpKey(), b.Long})
		}
	}
	return rows
}
