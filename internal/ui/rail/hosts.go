package rail

import "github.com/charmbracelet/lipgloss"

// HostState is what the last look at a connection found.
type HostState uint8

const (
	// HostConnecting has no answer from the host yet.
	HostConnecting HostState = iota
	HostOnline
	// HostOffline failed its last refresh and keeps the rows it had.
	HostOffline
)

// Host is one SSH connection's copied inventory, in the remote host's own
// order. Its rows nest under the connection's row.
type Host struct {
	Name           string
	State          HostState
	Err            string
	Groups         []string
	ArchivedGroups map[string]bool
	Sessions       []Session
}

// hostSeparator joins a connection to a remote path in keys. A connection
// name cannot hold it, so no remote key is ever a local group path.
const hostSeparator = "::"

func foldKey(host, path string) string {
	if host == "" {
		return path
	}
	return host + hostSeparator + path
}

// foldKey is the row's key in the collapsed set; a session row folds the
// group holding it.
func (r treeRow) foldKey() string {
	if r.kind == SessionRow {
		return foldKey(r.host, r.sess.Group)
	}
	return foldKey(r.host, r.group)
}

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

// foldable lists every key the fold-all key opens or closes: each local
// group, each connection, and each group on a connection.
func (m Model) foldable() map[string]bool {
	keys := groupClosure(m.snapshot.Groups, m.snapshot.Sessions)
	for _, host := range m.snapshot.Hosts {
		keys[foldKey(host.Name, "")] = true
		for path := range groupClosure(host.Groups, host.Sessions) {
			keys[foldKey(host.Name, path)] = true
		}
	}
	return keys
}

// SelectionCollapsed reports whether a group or connection row is folded.
func (m Model) SelectionCollapsed(selection Selection) bool {
	return m.collapsed[foldKey(selection.Host, selection.Group)]
}

// RemoteRefusal is the status line for an action a connection's rows do
// not offer yet.
func RemoteRefusal(action string) string {
	return action + " isn't available on SSH connections yet"
}

func (r renderer) renderConnection(row treeRow, selected bool, width int, pad, background string) string {
	marker := "▾"
	if r.model.collapsed[row.foldKey()] {
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
		meta = r.palette.muted.Render("offline")
	default:
		meta = r.groupStatusGlyphs(row.host, "", true)
		if meta == "" {
			meta = r.palette.subtle.Render("no agents yet")
		}
	}
	return paint(rowColumns(head, meta, width-railGutter), width, background)
}
