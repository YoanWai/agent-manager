package apply

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

// Stages says which durable writes a rename committed, in the order it
// wrote them. A stage that failed keeps the row it belonged to as it was.
type Stages struct {
	GroupRenamed bool
	PathSet      bool
	WorktreeSet  bool
	BaseSet      bool
	NameRenamed  bool
	ToolChanged  bool
}

func (o Outcome) Stages() Stages {
	return Stages{
		GroupRenamed: o.GroupRenamed,
		PathSet:      o.PathSet,
		WorktreeSet:  o.WorktreeSet,
		BaseSet:      o.BaseSet,
		NameRenamed:  o.NameRenamed,
		ToolChanged:  o.ToolChanged,
	}
}

// Inventory is the loaded list a rename mirrors into: the groups, the rows
// and the per-group choices the picker reads.
type Inventory struct {
	Groups         []string
	Sessions       []store.Session
	GroupPaths     map[string]string
	GroupWorktrees map[string]string
	GroupBases     map[string]string
}

// RenamedGroup moves a renamed subtree's groups, rows and choices to
// newPath, keeping only the stages that committed so a failed default-dir or
// worktree write does not leak the requested value into the local list.
func (inv Inventory) RenamedGroup(old, newPath, dir, worktree, base string, stages Stages) Inventory {
	moved := func(group string) (string, bool) {
		if group == old || strings.HasPrefix(group, old+"/") {
			return newPath + group[len(old):], true
		}
		return group, false
	}
	groups := make([]string, len(inv.Groups))
	for i, group := range inv.Groups {
		groups[i], _ = moved(group)
	}
	sessions := make([]store.Session, len(inv.Sessions))
	for i, sess := range inv.Sessions {
		sess.Group, _ = moved(sess.Group)
		sessions[i] = sess
	}
	renamed := Inventory{
		Groups:         groups,
		Sessions:       sessions,
		GroupPaths:     rekey(inv.GroupPaths, moved),
		GroupWorktrees: rekey(inv.GroupWorktrees, moved),
		GroupBases:     rekey(inv.GroupBases, moved),
	}
	if stages.PathSet {
		renamed.GroupPaths = setChoice(renamed.GroupPaths, newPath, dir)
	} else {
		// The moved path inherits whatever the old one had resolved to.
		renamed.GroupPaths = setChoice(renamed.GroupPaths, newPath, inv.GroupPaths[old])
	}
	if stages.WorktreeSet {
		renamed.GroupWorktrees = setChoice(renamed.GroupWorktrees, newPath, worktree)
	}
	if stages.BaseSet {
		renamed.GroupBases = setChoice(renamed.GroupBases, newPath, base)
	}
	return renamed
}

// RenamedSession applies the stages a session rename committed to the row
// it named. A tool change starts the agent over, so it drops the row's
// transcript id.
func (inv Inventory) RenamedSession(id string, stages Stages, name, branch, tool string) []store.Session {
	sessions := make([]store.Session, len(inv.Sessions))
	copy(sessions, inv.Sessions)
	for i := range sessions {
		if sessions[i].ID != id {
			continue
		}
		if stages.NameRenamed {
			sessions[i].Name = name
		}
		if branch != "" {
			sessions[i].WorktreeBranch = branch
		}
		if stages.ToolChanged {
			sessions[i].Tool = tool
			sessions[i].AgentSessionID = ""
		}
	}
	return sessions
}

func rekey(choices map[string]string, moved func(string) (string, bool)) map[string]string {
	out := make(map[string]string, len(choices))
	for group, choice := range choices {
		group, _ = moved(group)
		out[group] = choice
	}
	return out
}

// SetChoice records a group's own choice, "" dropping it back to inherit.
func SetChoice(choices map[string]string, group, value string) map[string]string {
	return setChoice(choices, group, value)
}

func setChoice(choices map[string]string, group, value string) map[string]string {
	if choices == nil {
		choices = map[string]string{}
	}
	if value == "" {
		delete(choices, group)
	} else {
		choices[group] = value
	}
	return choices
}
