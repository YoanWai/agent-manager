//go:build !windows

package sysstat

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// parsePSTime turns a ps time/cputime field into cumulative CPU seconds.
// Handles DD-HH:MM:SS, HH:MM:SS, and the common Darwin MM:SS.ss form
// (minutes may exceed 59).
func parsePSTime(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, nil
	}
	var days float64
	if i := strings.IndexByte(s, '-'); i >= 0 {
		d, err := strconv.ParseFloat(s[:i], 64)
		if err != nil {
			return 0, err
		}
		days = d
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		sec, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
		return days*86400 + sec, nil
	case 2:
		min, err1 := strconv.ParseFloat(parts[0], 64)
		sec, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("ps time %q", s)
		}
		return days*86400 + min*60 + sec, nil
	case 3:
		hour, err1 := strconv.ParseFloat(parts[0], 64)
		min, err2 := strconv.ParseFloat(parts[1], 64)
		sec, err3 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, fmt.Errorf("ps time %q", s)
		}
		return days*86400 + hour*3600 + min*60 + sec, nil
	default:
		return 0, fmt.Errorf("ps time %q", s)
	}
}

func nextField(line string) (string, string) {
	line = strings.TrimLeft(line, " ")
	if i := strings.IndexByte(line, ' '); i >= 0 {
		return line[:i], line[i+1:]
	}
	return line, ""
}

// Trees reports the combined CPU and resident memory of each requested
// process and all of its descendants, from one ps pass over the machine
// and a second limited to the roots' own children. tmux pane pids are
// shells whose real work happens in child processes, so a tree sum is the
// only honest number.
//
// CPUSeconds is cumulative CPU time for interval host-share math. PCPU is
// the raw ps %cpu sum (fallback). Callers convert to host % via
// HostCPUFromDelta between polls, or ScaleToHost for a one-shot sample.
func Trees(rootPIDs []int) map[int]ProcStat {
	stats := make(map[int]ProcStat, len(rootPIDs))
	if len(rootPIDs) == 0 {
		return stats
	}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,pcpu=,rss=,time=").Output()
	if err != nil {
		return stats
	}

	type proc struct {
		pcpu    float64
		rss     uint64
		cpuSecs float64
	}
	procs := map[int]proc{}
	children := map[int][]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		cpuPct, err3 := strconv.ParseFloat(fields[2], 64)
		rssKB, err4 := strconv.ParseUint(fields[3], 10, 64)
		cpuSecs, err5 := parsePSTime(fields[4])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}
		procs[pid] = proc{pcpu: cpuPct, rss: rssKB * 1024, cpuSecs: cpuSecs}
		children[ppid] = append(children[ppid], pid)
	}

	for _, root := range rootPIDs {
		if _, alive := procs[root]; !alive {
			continue
		}
		stat := ProcStat{OK: true}
		seen := map[int]bool{}
		var walk func(pid int)
		walk = func(pid int) {
			if seen[pid] {
				return
			}
			seen[pid] = true
			stat.Procs++
			stat.PCPU += procs[pid].pcpu
			stat.CPUSeconds += procs[pid].cpuSecs
			stat.RSS += procs[pid].rss
			for _, child := range children[pid] {
				walk(child)
			}
		}
		walk(root)
		// Leave CPUPercent 0 until the caller applies interval or fallback.
		stats[root] = stat
	}
	nameChildren(stats, children)
	return stats
}

// nameChildren fills in what each root runs directly. The programs come
// from a second ps limited to those pids: arguments cost the kernel a
// lookup per process, which is worth paying for a pane's own children and
// not for every process on the machine.
func nameChildren(stats map[int]ProcStat, children map[int][]int) {
	var wanted []string
	for root := range stats {
		for _, child := range children[root] {
			wanted = append(wanted, strconv.Itoa(child))
		}
	}
	if len(wanted) == 0 {
		return
	}
	// ps exits non-zero when every pid it was given has gone, which is a
	// child that ended between the two calls rather than a failure: there is
	// nothing left to name and the next sample sees whatever replaced it.
	out, err := exec.Command("ps", "-o", "pid=,ppid=,args=", "-p", strings.Join(wanted, ",")).Output()
	if err != nil {
		return
	}
	applyChildNames(stats, children, string(out))
}

// applyChildNames matches the second ps pass back to the tree the first one
// built. The parent has to still be the root it was sampled under: a child
// that exited between the two calls leaves its pid free for a process that
// is nothing to do with this pane.
func applyChildNames(stats map[int]ProcStat, children map[int][]int, psOutput string) {
	type child struct {
		ppid    int
		command string
	}
	named := map[int]child{}
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		pidText, rest := nextField(line)
		ppidText, rest := nextField(rest)
		command, _ := nextField(rest)
		pid, err1 := strconv.Atoi(pidText)
		ppid, err2 := strconv.Atoi(ppidText)
		if err1 != nil || err2 != nil || command == "" {
			continue
		}
		named[pid] = child{ppid: ppid, command: command}
	}
	for root, stat := range stats {
		for _, pid := range children[root] {
			if named[pid].ppid == root {
				stat.Children = append(stat.Children, named[pid].command)
			}
		}
		stats[root] = stat
	}
}
