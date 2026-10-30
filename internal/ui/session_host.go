package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	sessiondialogs "github.com/YoanWai/agent-manager/internal/ui/session/dialogs"
)

// sessionHost is the root as the session packages read it: the loaded rows,
// the pane projection, and the view on screen. It answers questions only;
// what a key decided travels back as a sessiondialogs.Intent and what a run
// changed as a lifecycle.Change, so nothing here writes.
type sessionHost struct{ m *Model }

func (m *Model) sessionHost() sessionHost { return sessionHost{m: m} }

func (h sessionHost) Sessions() []store.Session { return h.m.workspace.sessions }

func (h sessionHost) Listed() []store.Session { return h.m.listedSessions() }

func (h sessionHost) Groups() []string { return h.m.workspace.groups }

func (h sessionHost) ArchivedView() bool { return h.m.rail.ShowArchived() }

// Live is the pane projection: the last poll, plus launches accepted since it.
func (h sessionHost) Live(id string) bool { return h.m.sessionWindowProjectedLive(id) }

var _ sessiondialogs.View = sessionHost{}
