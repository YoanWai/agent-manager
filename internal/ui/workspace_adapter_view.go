package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/sysstat"
	uiworkspace "github.com/YoanWai/agent-manager/internal/ui/workspace"
	"github.com/charmbracelet/x/ansi"
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

// computerLines is the machine block docked at the rail's foot: a label
// and one thin meter per resource.
func (m *Model) computerLines(width int) []string {
	snap := snapReadings(m.workspace.snap)
	pad := strings.Repeat(" ", railInset)
	barWidth := uiworkspace.MeterWidth(width)

	render := func(label string, percent float64, extra, bar string) string {
		line := pad + labelStyle().Width(5).Render(label) + bar +
			valueStyle().Render(fmt.Sprintf(" %3.0f%%", percent))
		if extra != "" {
			line += subtleStyle().Render(" " + extra)
		}
		return line
	}
	meter := func(label string, percent float64, ok bool, extra string) string {
		if !ok {
			return pad + labelStyle().Width(5).Render(label) + subtleStyle().Render("n/a")
		}
		return render(label, percent, extra, gauge(percent, barWidth, false))
	}

	lines := []string{pad + subtleStyle().Render("computer")}
	lines = append(lines,
		meter("cpu", snap.CPUPercent, snap.CPUOK, ""),
		meter("mem", snap.MemPercent, snap.MemOK, humanBytes(snap.MemUsed)+"/"+humanBytes(snap.MemTotal)),
	)
	if snap.SwapOK && snap.SwapTotal > 0 {
		// Percent is used/total of the current swap allocation (macOS
		// grows the file under pressure; Linux uses the fixed swap size).
		lines = append(lines, meter("swap", snap.SwapPercent, true,
			humanBytes(snap.SwapUsed)+"/"+humanBytes(snap.SwapTotal)))
	}
	if snap.DiskOK {
		extra := diskBytes(snap.DiskAvailable) + " available"
		line := meter("disk", snap.DiskPercent, true, extra)
		if ansi.StringWidth(line) > width {
			lines = append(lines, meter("disk", snap.DiskPercent, true, ""),
				pad+strings.Repeat(" ", 5)+subtleStyle().Render(extra))
		} else {
			lines = append(lines, line)
		}
	} else {
		lines = append(lines, meter("disk", 0, false, ""))
	}
	if snap.BatteryOK {
		extra := ""
		if snap.BatteryCharging {
			extra = "charging"
		}
		lines = append(lines, render("batt", snap.BatteryPercent, extra,
			gauge(snap.BatteryPercent, barWidth, true)))
	}
	if temps := tempReadings(snap); temps != "" {
		lines = append(lines, pad+labelStyle().Width(5).Render("temp")+temps)
	}
	if m.workspace.net.Rates {
		lines = append(lines, pad+labelStyle().Width(5).Render("net")+
			valueStyle().Render("↓ "+humanBytes(m.workspace.net.Down)+"/s")+
			subtleStyle().Render("  ↑ "+humanBytes(m.workspace.net.Up)+"/s"))
	}
	return append(lines, "")
}

func tempReadings(snap uiworkspace.Readings) string {
	styled := make([]string, 0, 3)
	for _, reading := range uiworkspace.Temperatures(snap) {
		styled = append(styled, valueStyle().Render(reading))
	}
	return strings.Join(styled, subtleStyle().Render("  "))
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
	reading := func(label, value string, ok bool) string {
		if !ok {
			return labelStyle().Render(label+" ") + subtleStyle().Render("n/a")
		}
		return labelStyle().Render(label+" ") + valueStyle().Render(value)
	}
	snap := snapReadings(m.workspace.snap)
	parts := []string{
		reading("cpu", fmt.Sprintf("%.0f%%", snap.CPUPercent), snap.CPUOK),
		reading("mem", fmt.Sprintf("%.0f%% %s/%s", snap.MemPercent, humanBytes(snap.MemUsed), humanBytes(snap.MemTotal)), snap.MemOK),
	}
	if snap.SwapOK && snap.SwapTotal > 0 {
		parts = append(parts, reading("swap", fmt.Sprintf("%.0f%% %s/%s", snap.SwapPercent, humanBytes(snap.SwapUsed), humanBytes(snap.SwapTotal)), true))
	}
	parts = append(parts, reading("disk", fmt.Sprintf("%.0f%% %s available", snap.DiskPercent, diskBytes(snap.DiskAvailable)), snap.DiskOK))
	if snap.BatteryOK {
		value := fmt.Sprintf("%.0f%%", snap.BatteryPercent)
		if snap.BatteryCharging {
			value += " charging"
		}
		parts = append(parts, reading("batt", value, true))
	}
	if temps := tempReadings(snap); temps != "" {
		parts = append(parts, labelStyle().Render("temp ")+temps)
	}
	if m.workspace.net.Rates {
		parts = append(parts, reading("net", "↓ "+humanBytes(m.workspace.net.Down)+"/s ↑ "+humanBytes(m.workspace.net.Up)+"/s", true))
	}
	line := strings.Repeat(" ", railInset) + strings.Join(parts, "  ")
	return []string{ansi.Truncate(line, max(width-railInset, 0), "…")}
}
