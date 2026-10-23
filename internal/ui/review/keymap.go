package review

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// binding is one of Review's keys, said once. Key dispatches on Keys, the
// footer draws Legend and the ? map draws Help, so a key cannot be handled
// without being named, or named without being handled.
type binding struct {
	Keys []string
	// Legend is what the footer pair shows for the keys: a glyph, or several
	// keys folded into one pair. Empty uses the keys joined with "/".
	Legend string
	// Help is the ? map's key column. Empty uses Legend.
	Help string
	// Footer is the pair's footer label, computed from the review's state.
	// Nil leaves the binding out of the footer.
	Footer func(*Model) string
	// Long is the ? map's description. Empty leaves it out of the map.
	Long string
	// Move puts the pair in the quiet Move tier instead of Review.
	Move bool
	Run  func(*Model, KeyContext, int, *KeyResult)
}

func label(text string) func(*Model) string { return func(*Model) string { return text } }

// keymap is every key Review answers to outside a draft or a send prompt,
// in the order the footer and the ? map show them.
var keymap = []binding{
	{Keys: []string{"c"}, Footer: label("comment"), Long: "comment on the line",
		Run: func(m *Model, _ KeyContext, _ int, _ *KeyResult) { m.OpenAnnotation() }},
	{Keys: []string{"d"}, Footer: label("remove/handle"), Long: "remove a draft, or mark sent feedback handled / open",
		Run: func(m *Model, ctx KeyContext, _ int, r *KeyResult) {
			r.Requests, r.Error = m.DiscardOrToggle(ctx.PersistenceAvailable)
		}},
	{Keys: []string{"C"}, Footer: sendLabel, Long: "send drafts to the agent as the next review round",
		Run: func(m *Model, _ KeyContext, _ int, r *KeyResult) {
			if m.DraftCount() == 0 {
				r.Error = "no comments to send - press c on a line first"
				return
			}
			m.sendConfirm = true
		}},
	{Keys: []string{"space", " "}, Legend: "space", Footer: label("reviewed"), Long: "mark the file reviewed",
		Run: func(m *Model, ctx KeyContext, _ int, r *KeyResult) {
			if !ctx.PersistenceAvailable {
				r.Error = "review state is unavailable"
				return
			}
			r.Requests = m.ToggleReviewed()
		}},
	{Keys: []string{"s"}, Footer: func(m *Model) string { return "scope: " + m.scope.String() },
		Long: "cycle the scope (uncommitted / vs target / last commit / staged)",
		Run: func(m *Model, _ KeyContext, _ int, r *KeyResult) {
			if request, ok := m.CycleScope(); ok {
				r.Requests.Load = &request
				r.Requests.StartupTick = true
			}
		}},
	{Keys: []string{"r"}, Footer: repoLabel, Long: "pick the repo when the session dir holds several",
		Run: navigate(NavigationRepoPicker)},
	{Keys: []string{"b"}, Footer: label("branch"), Long: "switch to another worktree, listed by branch",
		Run: navigate(NavigationBranchPicker)},
	{Keys: []string{"B"}, Footer: label("target"), Long: "pick the target branch the branch diff compares against",
		Run: navigate(NavigationBasePicker)},
	{Keys: []string{"up", "k"}, Legend: "↑↓/jk", Help: "↑↓ / jk", Move: true, Footer: label("scroll line"), Long: "move a line",
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.MoveCursor(-1, h) }},
	{Keys: []string{"down", "j"},
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.MoveCursor(1, h) }},
	{Keys: []string{"ctrl+d"}, Legend: "ctrl+d/ctrl+u", Move: true, Footer: label("half page"), Long: "half a page",
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.MoveCursor(h/2, h) }},
	{Keys: []string{"ctrl+u"},
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.MoveCursor(-h/2, h) }},
	{Keys: []string{"pgup"}, Legend: "pgup/pgdn", Move: true, Footer: label("page"), Long: "page",
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.MoveCursor(-h, h) }},
	{Keys: []string{"pgdn", "pgdown"},
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.MoveCursor(h, h) }},
	{Keys: []string{"g"}, Legend: "g/G", Move: true, Footer: label("top/bottom"), Long: "top / bottom of the file",
		Run: func(m *Model, _ KeyContext, _ int, _ *KeyResult) { m.First() }},
	{Keys: []string{"G"},
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.Last(h) }},
	{Keys: []string{"tab", "J"}, Legend: "tab/J K/shift+tab", Help: "tab / shift+tab", Move: true, Footer: label("file"),
		Long: "next / previous file (J / K too)",
		Run:  func(m *Model, _ KeyContext, _ int, r *KeyResult) { r.Requests = m.SwitchFile(1) }},
	{Keys: []string{"shift+tab", "K"},
		Run: func(m *Model, _ KeyContext, _ int, r *KeyResult) { r.Requests = m.SwitchFile(-1) }},
	{Keys: []string{"n"}, Legend: "n/N", Move: true, Footer: label("change"), Long: "next / previous change",
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.JumpChange(1, h) }},
	{Keys: []string{"N", "shift+n"},
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.JumpChange(-1, h) }},
	{Keys: []string{"u"}, Move: true, Footer: label("layout"), Long: "unified / split layout",
		Run: func(m *Model, _ KeyContext, h int, _ *KeyResult) { m.ToggleSideBySide(h) }},
	{Keys: []string{"f"}, Move: true, Footer: filterLabel, Long: "code files only (hides images, assets, lock files)",
		Run: func(m *Model, _ KeyContext, _ int, r *KeyResult) { r.Requests = m.ToggleCodeOnly() }},
	{Keys: []string{"o", "f3"}, Move: true, Footer: label("open file"), Long: "open the current file in your editor",
		Run: func(m *Model, _ KeyContext, _ int, r *KeyResult) { r.Requests.FileCheck = m.OpenFileRequest() }},
	{Keys: []string{"?"}, Move: true, Footer: label("keys"), Long: "this key map",
		Run: navigate(NavigationHelp)},
	{Keys: []string{"esc", "q"}, Move: true, Footer: label("close"), Long: "close the review",
		Run: navigate(NavigationExit)},
}

// quitPair is the one footer pair Review names but does not handle: root
// decides ctrl+c for every surface.
var quitPair = [2]string{"ctrl+c", "quit"}

func navigate(to Navigation) func(*Model, KeyContext, int, *KeyResult) {
	return func(_ *Model, _ KeyContext, _ int, r *KeyResult) { r.Navigation = to }
}

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

// HelpRows are Review's rows in the ? map, read from the same keymap the
// keys run from.
func HelpRows() [][2]string {
	var rows [][2]string
	for _, b := range keymap {
		if b.Long != "" {
			rows = append(rows, [2]string{b.helpKey(), b.Long})
		}
	}
	return rows
}
