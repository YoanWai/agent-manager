package ui

import (
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"hash/fnv"
	"time"
)

type netStats struct {
	up       uint64
	down     uint64
	rates    bool
	prevSent uint64
	prevRecv uint64
	prevAt   time.Time
	prevOK   bool
}

// updateNetRates diffs cumulative interface counters between polls into
// bytes-per-second rates. Counters can reset (sleep, interface changes),
// so a backwards jump just reseeds the baseline.
func (m *Model) updateNetRates(snap sysstat.Snapshot) {
	now := time.Now()
	if m.workspace.net.prevOK && snap.NetOK &&
		snap.NetSent >= m.workspace.net.prevSent && snap.NetRecv >= m.workspace.net.prevRecv {
		if dt := now.Sub(m.workspace.net.prevAt).Seconds(); dt > 0 {
			m.workspace.net.up = uint64(float64(snap.NetSent-m.workspace.net.prevSent) / dt)
			m.workspace.net.down = uint64(float64(snap.NetRecv-m.workspace.net.prevRecv) / dt)
			m.workspace.net.rates = true
		}
	}
	m.workspace.net.prevSent = snap.NetSent
	m.workspace.net.prevRecv = snap.NetRecv
	m.workspace.net.prevAt = now
	m.workspace.net.prevOK = snap.NetOK
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
