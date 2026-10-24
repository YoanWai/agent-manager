package ui

import (
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uiworkspace "github.com/YoanWai/agent-manager/internal/ui/workspace"
)

// snapReadings flattens an OS snapshot off its probe into the values the foot
// lays out, so the layout never reads sysstat itself.
func snapReadings(snap sysstat.Snapshot) uiworkspace.Readings {
	return uiworkspace.Readings{
		CPUPercent: snap.CPUPercent, CPUOK: snap.CPUOK,
		MemPercent: snap.MemPercent, MemOK: snap.MemOK, MemUsed: snap.MemUsed, MemTotal: snap.MemTotal,
		SwapOK: snap.SwapOK, SwapPercent: snap.SwapPercent, SwapUsed: snap.SwapUsed, SwapTotal: snap.SwapTotal,
		DiskOK: snap.DiskOK, DiskPercent: snap.DiskPercent, DiskAvailable: snap.DiskAvailable,
		BatteryOK: snap.BatteryOK, BatteryPercent: snap.BatteryPercent, BatteryCharging: snap.BatteryCharging,
		CPUTempOK: snap.CPUTempOK, CPUTemp: snap.CPUTemp,
		GPUTempOK: snap.GPUTempOK, GPUTemp: snap.GPUTemp,
		SoCTempOK: snap.SoCTempOK, SoCTemp: snap.SoCTemp,
	}
}

// computerLines is the machine block docked at the rail's foot.
func (m *Model) computerLines(width int) []string {
	return uiworkspace.ComputerLines(snapReadings(m.workspace.snap), m.workspace.net, width)
}

func (m *Model) railFootLines(width int) []string {
	if m.prefs.hideStats {
		return nil
	}
	if m.prefs.fullLayout {
		return m.fullFootLine(width)
	}
	return m.computerLines(width)
}

func (m *Model) fullFootLine(width int) []string {
	return uiworkspace.FootLine(snapReadings(m.workspace.snap), m.workspace.net, width)
}
