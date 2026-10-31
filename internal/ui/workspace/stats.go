// Package workspace holds the value-only parts of the workspace: the machine
// readings the rail foot renders and the network rate math behind them, both
// taking plain numbers so neither needs an OS probe.
package workspace

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"time"
)

// Net is the network reading the foot shows, and the baseline it diffs
// against. Up and Down are bytes per second.
type Net struct {
	Up    uint64
	Down  uint64
	Rates bool

	prevSent uint64
	prevRecv uint64
	prevAt   time.Time
	prevOK   bool
}

// Update diffs cumulative interface counters between polls into
// bytes-per-second rates. Counters can reset (sleep, interface changes), so a
// backwards jump just reseeds the baseline.
func (n *Net) Update(sent, recv uint64, ok bool, now time.Time) {
	if n.prevOK && ok && sent >= n.prevSent && recv >= n.prevRecv {
		if dt := now.Sub(n.prevAt).Seconds(); dt > 0 {
			n.Up = uint64(float64(sent-n.prevSent) / dt)
			n.Down = uint64(float64(recv-n.prevRecv) / dt)
			n.Rates = true
		}
	}
	n.prevSent = sent
	n.prevRecv = recv
	n.prevAt = now
	n.prevOK = ok
}

// Readings is the OS snapshot the foot lays out.
type Readings = sysstat.Snapshot

// Temperatures lists the sensor readings in display order, each as bare text
// the foot styles.
func Temperatures(r Readings) []string {
	var parts []string
	if r.CPUTempOK {
		parts = append(parts, fmt.Sprintf("cpu %.0f°C", r.CPUTemp))
	}
	if r.GPUTempOK {
		parts = append(parts, fmt.Sprintf("gpu %.0f°C", r.GPUTemp))
	}
	if r.SoCTempOK {
		parts = append(parts, fmt.Sprintf("soc %.0f°C", r.SoCTemp))
	}
	return parts
}

// MeterWidth is the gauge column that fits a foot of width, kept between a
// readable floor and a ceiling that leaves the label and reading their room.
func MeterWidth(width int) int {
	barWidth := width - 22
	if barWidth < 4 {
		barWidth = 4
	}
	if barWidth > 10 {
		barWidth = 10
	}
	return barWidth
}
