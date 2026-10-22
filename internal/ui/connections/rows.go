package connections

import remote "github.com/YoanWai/agent-manager/internal/remote/value"

// Kind is which of a connection's rows a target names.
type Kind uint8

const (
	ConnectionRow Kind = iota
	GroupRow
	SessionRow
)

// Target is a row on a connection as the rail names it: the connection
// itself, one of its groups, or an agent or terminal by ID.
type Target struct {
	Kind  Kind
	Host  string
	Group string
	ID    string
}

// Row is the row a target names, as the last snapshot of its host has it.
type Row struct {
	Kind     Kind
	Host     string
	Group    string
	Ref      remote.Ref
	Name     string
	Status   string
	Terminal bool
	Archived bool
}

// Row finds the target's row; an agent or terminal the last snapshot does
// not list has none.
func (c *Connections) Row(target Target) (Row, bool) {
	row := Row{Kind: target.Kind, Host: target.Host, Group: target.Group}
	if target.Kind != SessionRow {
		return row, true
	}
	host := c.hosts[target.Host]
	for _, sess := range host.Sessions {
		if sess.ID == target.ID {
			return row.of(sess, false), true
		}
	}
	for _, term := range host.Terminals {
		if term.ID == target.ID {
			return row.of(term, true), true
		}
	}
	return Row{}, false
}

func (r Row) of(sess Session, terminal bool) Row {
	r.Ref = remote.Ref{Host: r.Host, ID: sess.ID}
	r.Name, r.Status, r.Group, r.Archived = sess.Name, sess.Status, sess.Group, sess.Archived
	r.Terminal = terminal
	return r
}

// TroubleLabel names a failed refresh in a few words for the connection's
// row, telling a host ssh cannot reach from one it reaches where
// agent-manager is missing or on another version.
func TroubleLabel(trouble remote.Trouble) string {
	switch trouble {
	case remote.TroubleUnreachable:
		return "unreachable"
	case remote.TroubleMissing:
		return "agent-manager missing"
	case remote.TroubleOutdated:
		return "agent-manager too old"
	case remote.TroubleNewer:
		return "agent-manager newer"
	}
	return "error"
}
