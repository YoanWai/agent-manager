package spawn

import (
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

// GroupOption is one row of a group picker: a group path, or a session row
// nested under the group it sits in.
type GroupOption struct {
	Path  string
	Depth int
	Name  string
	// SessionID is the agent a shell nests under, empty for a group row.
	SessionID string
}

// Picker is the group list the New Session form and the move card pick
// from, each its own.
type Picker struct {
	Groups     []GroupOption
	GroupIndex int
}

// GroupPath is the group the picker highlights.
func (p *Picker) GroupPath() string {
	if p.GroupIndex >= 0 && p.GroupIndex < len(p.Groups) {
		return p.Groups[p.GroupIndex].Path
	}
	return ""
}

// Picked is the row the picker highlights.
func (p *Picker) Picked() GroupOption { return p.Groups[p.GroupIndex] }

// Step moves the cursor, wrapping at the ends.
func (p *Picker) Step(delta int) bool {
	count := len(p.Groups)
	if count == 0 {
		return false
	}
	p.GroupIndex = (p.GroupIndex + delta + count) % count
	return true
}

// Reselect puts the cursor back on path, or on the root without it.
func (p *Picker) Reselect(path string, match func(GroupOption) bool) {
	p.GroupIndex = 0
	for i, opt := range p.Groups {
		if opt.Path == path && match(opt) {
			p.GroupIndex = i
			return
		}
	}
}

// GroupClosure is every group path in the closure of the listed groups and
// the groups the sessions sit in, each with its ancestors.
func GroupClosure(groups []string, sessionGroups []string) map[string]bool {
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
	for _, group := range sessionGroups {
		add(group)
	}
	return paths
}

// ChildIndex is the groups under each parent, in the order the groups list
// ranks them, then by name for the rest.
func ChildIndex(paths map[string]bool, ordered []string) map[string][]string {
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

// parentGroup is the group a path sits under.
func parentGroup(group string) string {
	if index := strings.LastIndex(group, "/"); index >= 0 {
		return group[:index]
	}
	return ""
}

// baseName is the last segment of a group path.
func baseName(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}

// ViewGroupOptions paints the picker's rows, marking the highlighted one.
func ViewGroupOptions(groups []GroupOption, index int, st Styles) string {
	var b strings.Builder
	for i, opt := range groups {
		selected := i == index
		marker := "  "
		if selected {
			marker = lipgloss.NewStyle().Foreground(st.Accent).Render(presentation.GlyphsFor(st.GlyphMode).SelectionGlyph + " ")
		}
		label := groupLabel(opt.Path)
		if opt.SessionID != "" {
			label = strings.Repeat("  ", opt.Depth) + opt.Name
		} else if opt.Path != "" {
			label = strings.Repeat("  ", opt.Depth) + baseName(opt.Path)
		}
		style := st.Muted
		if selected {
			style = lipgloss.NewStyle().Foreground(st.Accent2).Bold(true)
		}
		b.WriteString("  " + marker + style.Render(presentation.EscapeControlsInline(label)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func groupLabel(path string) string {
	if path == "" {
		return "root"
	}
	return path
}
