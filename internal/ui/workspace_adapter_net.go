package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/sysstat"
	uiworkspace "github.com/YoanWai/agent-manager/internal/ui/workspace"
)

type netStats = uiworkspace.Net

// updateNetRates diffs cumulative interface counters between polls into
// bytes-per-second rates, which the value type owns.
func (m *Model) updateNetRates(snap sysstat.Snapshot) {
	m.workspace.net.Update(snap.NetSent, snap.NetRecv, snap.NetOK, time.Now())
}
