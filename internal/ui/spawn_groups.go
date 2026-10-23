package ui

import (
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

type groupOption struct {
	path   string
	depth  int
	sessID string
	name   string
}

// groupPicker is the group list the New Session form and the move card
// pick from, each its own.
type groupPicker struct {
	groups     []groupOption
	groupIndex int
}

// groupPath is the group the picker highlights.
func (p *groupPicker) groupPath() string {
	if p.groupIndex >= 0 && p.groupIndex < len(p.groups) {
		return p.groups[p.groupIndex].path
	}
	return ""
}

func (p *groupPicker) picked() groupOption { return p.groups[p.groupIndex] }

// step moves the cursor, wrapping at the ends.
func (p *groupPicker) step(delta int) bool {
	count := len(p.groups)
	if count == 0 {
		return false
	}
	p.groupIndex = (p.groupIndex + delta + count) % count
	return true
}

// reselect puts the cursor back on path, or on the root without it.
func (p *groupPicker) reselect(path string, match func(groupOption) bool) {
	p.groupIndex = 0
	for i, opt := range p.groups {
		if opt.path == path && match(opt) {
			p.groupIndex = i
			return
		}
	}
}

// groupPickerFor is the picker rows of the group tree, highlighting
// selectPath. Index 0 is always the root.
func (m *Model) groupPickerFor(selectPath string) groupPicker {
	groups, index := m.groupOptions(selectPath)
	return groupPicker{groups: groups, groupIndex: index}
}

// groupOptions is the picker's rows and the index of selectPath in them.
func (m *Model) groupOptions(selectPath string) ([]groupOption, int) {
	paths := groupClosure(m.workspace.groups, m.workspace.sessions)
	for path := range paths {
		if m.groupEffectivelyArchived(path) {
			delete(paths, path)
		}
	}
	children := childIndex(paths, m.workspace.groups)

	options := []groupOption{{path: "", depth: 0}}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		options = append(options, groupOption{path: path, depth: depth})
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 1)
	}

	for i, opt := range options {
		if selectPath != "" && opt.path == selectPath {
			return options, i
		}
	}
	return options, 0
}

func groupClosure(groups []string, sessions []store.Session) map[string]bool {
	paths := map[string]bool{}
	add := func(path string) {
		for path != "" {
			paths[path] = true
			index := strings.LastIndex(path, "/")
			if index < 0 {
				break
			}
			path = path[:index]
		}
	}
	for _, group := range groups {
		add(group)
	}
	for _, session := range sessions {
		add(session.Group)
	}
	return paths
}

func childIndex(paths map[string]bool, ordered []string) map[string][]string {
	rank := make(map[string]int, len(ordered))
	for index, path := range ordered {
		rank[path] = index
	}
	children := map[string][]string{}
	for path := range paths {
		parent := parentGroup(path)
		children[parent] = append(children[parent], path)
	}
	for _, siblings := range children {
		sort.SliceStable(siblings, func(i, j int) bool {
			left, leftKnown := rank[siblings[i]]
			right, rightKnown := rank[siblings[j]]
			if leftKnown && rightKnown {
				return left < right
			}
			if leftKnown != rightKnown {
				return leftKnown
			}
			return siblings[i] < siblings[j]
		})
	}
	return children
}

func (m *Model) groupEffectivelyArchived(path string) bool {
	return store.EffectivelyArchived(m.workspace.archivedGroups, path)
}

func (m *Model) materializeGroupsLocal(paths []string) {
	known := make(map[string]bool, len(m.workspace.groups))
	for _, path := range m.workspace.groups {
		known[path] = true
	}
	for _, path := range paths {
		if !known[path] {
			m.workspace.groups = append(m.workspace.groups, path)
			known[path] = true
		}
	}
}

func (m *Model) viewGroupPicker(p *groupPicker) string {
	return viewGroupOptions(p.groups, p.groupIndex, m.prefs.glyphMode)
}

func viewGroupOptions(groups []groupOption, index int, mode presentation.GlyphMode) string {
	var b strings.Builder
	for i, opt := range groups {
		selected := i == index
		marker := "  "
		if selected {
			marker = lipgloss.NewStyle().Foreground(colorAccent()).Render(presentation.GlyphsFor(mode).SelectionGlyph + " ")
		}
		label := displayGroup(opt.path)
		if opt.sessID != "" {
			label = strings.Repeat("  ", opt.depth) + opt.name
		} else if opt.path != "" {
			label = strings.Repeat("  ", opt.depth) + baseName(opt.path)
		}
		style := mutedStyle()
		if selected {
			style = lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true)
		}
		b.WriteString("  " + marker + style.Render(escapeControlsInline(label)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
