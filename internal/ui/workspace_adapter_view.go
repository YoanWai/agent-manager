package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/sysstat"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
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
		line := pad + uitheme.LabelStyle().Width(5).Render(label) + bar +
			uitheme.ValueStyle().Render(fmt.Sprintf(" %3.0f%%", percent))
		if extra != "" {
			line += uitheme.SubtleStyle().Render(" " + extra)
		}
		return line
	}
	meter := func(label string, percent float64, ok bool, extra string) string {
		if !ok {
			return pad + uitheme.LabelStyle().Width(5).Render(label) + uitheme.SubtleStyle().Render("n/a")
		}
		return render(label, percent, extra, uitheme.Gauge(percent, barWidth, false))
	}

	lines := []string{pad + uitheme.SubtleStyle().Render("computer")}
	lines = append(lines,
		meter("cpu", snap.CPUPercent, snap.CPUOK, ""),
		meter("mem", snap.MemPercent, snap.MemOK, uiframe.HumanBytes(snap.MemUsed)+"/"+uiframe.HumanBytes(snap.MemTotal)),
	)
	if snap.SwapOK && snap.SwapTotal > 0 {
		// Percent is used/total of the current swap allocation (macOS
		// grows the file under pressure; Linux uses the fixed swap size).
		lines = append(lines, meter("swap", snap.SwapPercent, true,
			uiframe.HumanBytes(snap.SwapUsed)+"/"+uiframe.HumanBytes(snap.SwapTotal)))
	}
	if snap.DiskOK {
		extra := uiframe.DiskBytes(snap.DiskAvailable) + " available"
		line := meter("disk", snap.DiskPercent, true, extra)
		if ansi.StringWidth(line) > width {
			lines = append(lines, meter("disk", snap.DiskPercent, true, ""),
				pad+strings.Repeat(" ", 5)+uitheme.SubtleStyle().Render(extra))
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
			uitheme.Gauge(snap.BatteryPercent, barWidth, true)))
	}
	if temps := tempReadings(snap); temps != "" {
		lines = append(lines, pad+uitheme.LabelStyle().Width(5).Render("temp")+temps)
	}
	if m.workspace.net.Rates {
		lines = append(lines, pad+uitheme.LabelStyle().Width(5).Render("net")+
			uitheme.ValueStyle().Render("↓ "+uiframe.HumanBytes(m.workspace.net.Down)+"/s")+
			uitheme.SubtleStyle().Render("  ↑ "+uiframe.HumanBytes(m.workspace.net.Up)+"/s"))
	}
	return append(lines, "")
}

func tempReadings(snap uiworkspace.Readings) string {
	styled := make([]string, 0, 3)
	for _, reading := range uiworkspace.Temperatures(snap) {
		styled = append(styled, uitheme.ValueStyle().Render(reading))
	}
	return strings.Join(styled, uitheme.SubtleStyle().Render("  "))
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
			return uitheme.LabelStyle().Render(label+" ") + uitheme.SubtleStyle().Render("n/a")
		}
		return uitheme.LabelStyle().Render(label+" ") + uitheme.ValueStyle().Render(value)
	}
	snap := snapReadings(m.workspace.snap)
	parts := []string{
		reading("cpu", fmt.Sprintf("%.0f%%", snap.CPUPercent), snap.CPUOK),
		reading("mem", fmt.Sprintf("%.0f%% %s/%s", snap.MemPercent, uiframe.HumanBytes(snap.MemUsed), uiframe.HumanBytes(snap.MemTotal)), snap.MemOK),
	}
	if snap.SwapOK && snap.SwapTotal > 0 {
		parts = append(parts, reading("swap", fmt.Sprintf("%.0f%% %s/%s", snap.SwapPercent, uiframe.HumanBytes(snap.SwapUsed), uiframe.HumanBytes(snap.SwapTotal)), true))
	}
	parts = append(parts, reading("disk", fmt.Sprintf("%.0f%% %s available", snap.DiskPercent, uiframe.DiskBytes(snap.DiskAvailable)), snap.DiskOK))
	if snap.BatteryOK {
		value := fmt.Sprintf("%.0f%%", snap.BatteryPercent)
		if snap.BatteryCharging {
			value += " charging"
		}
		parts = append(parts, reading("batt", value, true))
	}
	if temps := tempReadings(snap); temps != "" {
		parts = append(parts, uitheme.LabelStyle().Render("temp ")+temps)
	}
	if m.workspace.net.Rates {
		parts = append(parts, reading("net", "↓ "+uiframe.HumanBytes(m.workspace.net.Down)+"/s ↑ "+uiframe.HumanBytes(m.workspace.net.Up)+"/s", true))
	}
	line := strings.Repeat(" ", railInset) + strings.Join(parts, "  ")
	return []string{ansi.Truncate(line, max(width-railInset, 0), "…")}
}
