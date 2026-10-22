package rail

import (
	"cmp"
	"slices"

	"github.com/charmbracelet/lipgloss"
)

// HostState is what the last look at a connection found.
type HostState uint8

const (
	// HostConnecting has no answer from the host yet.
	HostConnecting HostState = iota
	HostOnline
	// HostOffline failed its last refresh and keeps the rows it had; its
	// Trouble says why in a few words.
	HostOffline
)

// Host is one SSH connection's copied inventory, in the remote host's own
// order. Its rows nest under the connection's row.
type Host struct {
	Name           string
	State          HostState
	Err            string
	Trouble        string
	Groups         []string
	ArchivedGroups map[string]bool
	Sessions       []Session
}

// HostFold is a folded row on a connection: the connection's own row when
// Path is empty, otherwise one of its groups. Folds on connections are kept
// apart from local ones, since a local group path can hold any text,
// "box::web" included.
type HostFold struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

// folded reports whether the group at path, on host or locally, is folded.
func (m Model) folded(host, path string) bool {
	if host == "" {
		return m.collapsed[path]
	}
	return m.hostFolds[HostFold{Host: host, Path: path}]
}

func (m *Model) setFolded(host, path string, folded bool) {
	if host == "" {
		m.collapsed[path] = folded
		return
	}
	m.hostFolds[HostFold{Host: host, Path: path}] = folded
}

// foldPath is the group a row folds; a session row folds the group
// holding it.
func (r treeRow) foldPath() string {
	if r.kind == SessionRow {
		return r.sess.Group
	}
	return r.group
}

func (m Model) rowFolded(row treeRow) bool { return m.folded(row.host, row.foldPath()) }

func (m Model) host(name string) (Host, bool) {
	for _, host := range m.snapshot.Hosts {
		if host.Name == name {
			return host, true
		}
	}
	return Host{}, false
}

// sessionsOf is the inventory of the host a row belongs to.
func (m Model) sessionsOf(host string) []Session {
	if host == "" {
		return m.snapshot.Sessions
	}
	remote, _ := m.host(host)
	return remote.Sessions
}

// foldable lists every row the fold-all key opens or closes: each local
// group, each connection, and each group on a connection.
func (m Model) foldable() []HostFold {
	var folds []HostFold
	for path := range groupClosure(m.snapshot.Groups, m.snapshot.Sessions) {
		folds = append(folds, HostFold{Path: path})
	}
	for _, host := range m.snapshot.Hosts {
		folds = append(folds, HostFold{Host: host.Name})
		for path := range groupClosure(host.Groups, host.Sessions) {
			folds = append(folds, HostFold{Host: host.Name, Path: path})
		}
	}
	return folds
}

// HostCollapsed lists the folded rows on connections, for saving beside the
// local folds.
func (m Model) HostCollapsed() []HostFold {
	folds := make([]HostFold, 0, len(m.hostFolds))
	for fold, folded := range m.hostFolds {
		if folded {
			folds = append(folds, fold)
		}
	}
	slices.SortFunc(folds, func(a, b HostFold) int {
		return cmp.Or(cmp.Compare(a.Host, b.Host), cmp.Compare(a.Path, b.Path))
	})
	return folds
}

// RenameHost carries a renamed connection's folds to its new name.
func (m *Model) RenameHost(old, next string) Decision {
	for fold, folded := range m.hostFolds {
		if fold.Host == old {
			delete(m.hostFolds, fold)
			m.hostFolds[HostFold{Host: next, Path: fold.Path}] = folded
		}
	}
	m.rebuildRows()
	return m.collapsedDecision()
}

// ForgetHost drops a removed connection's folds, so a later connection by
// that name starts open.
func (m *Model) ForgetHost(name string) Decision {
	for fold := range m.hostFolds {
		if fold.Host == name {
			delete(m.hostFolds, fold)
		}
	}
	m.rebuildRows()
	return m.collapsedDecision()
}

// RestoreHostFolds folds the connection rows a previous run saved.
func (m *Model) RestoreHostFolds(folds []HostFold) {
	for _, fold := range folds {
		if fold.Host != "" {
			m.hostFolds[fold] = true
		}
	}
}

// SelectionCollapsed reports whether a group or connection row is folded.
func (m Model) SelectionCollapsed(selection Selection) bool {
	return m.folded(selection.Host, selection.Group)
}

// RemoteRefusal is the status line for an action a connection's rows do
// not offer yet.
func RemoteRefusal(action string) string {
	return action + " isn't available on SSH connections yet"
}

func (r renderer) renderConnection(row treeRow, selected bool, width int, pad, background string) renderedTreeRow {
	marker := "▾"
	if r.model.rowFolded(row) {
		marker = "▸"
	}
	tint := lipgloss.Color(r.ctx.Theme.Remote)
	nameStyle := lipgloss.NewStyle().Foreground(tint).Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(lipgloss.Color(r.ctx.Theme.Bright))
	}
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Bg)).Background(tint).Bold(true).Render(" ssh ")
	head := pad + r.palette.subtle.Render(marker) + " " + r.palette.highlight(row.host, r.model.search, nameStyle) + " " + label
	host, _ := r.model.host(row.host)
	var meta string
	switch host.State {
	case HostConnecting:
		meta = r.palette.subtle.Render("connecting…")
	case HostOffline:
		meta = r.palette.muted.Render(cmp.Or(host.Trouble, "offline"))
	default:
		meta = r.groupStatusGlyphs(row.host, "", true)
		if meta == "" {
			meta = r.palette.subtle.Render("no agents yet")
		}
	}
	return renderedTreeRow{text: paint(rowColumns(head, meta, width-railGutter), width, background), handleX: -1}
}
