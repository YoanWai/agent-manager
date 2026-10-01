//go:build windows

package sysstat

import (
	"github.com/YoanWai/agent-manager/internal/proctree"
	"github.com/shirou/gopsutil/v4/process"
)

// Trees reports the combined CPU and resident memory of each requested
// process and all of its descendants. The tree comes from one Toolhelp
// snapshot of the machine; times and memory are read per visited process,
// since Windows has no ps that reports them in the same pass.
//
// CPUSeconds is cumulative CPU time for interval host-share math. PCPU
// stays 0: Windows keeps no lifetime %cpu figure, so the poller's interval
// delta supplies CPU% from the second sample on.
func Trees(rootPIDs []int) map[int]ProcStat {
	stats := make(map[int]ProcStat, len(rootPIDs))
	if len(rootPIDs) == 0 {
		return stats
	}
	entries, err := proctree.Snapshot()
	if err != nil {
		return stats
	}
	alive := make(map[int]bool, len(entries))
	children := map[int][]proctree.Entry{}
	for _, entry := range entries {
		alive[entry.PID] = true
		// The idle process names itself as its own parent.
		if entry.PID != entry.PPID {
			children[entry.PPID] = append(children[entry.PPID], entry)
		}
	}

	for _, root := range rootPIDs {
		if !alive[root] {
			continue
		}
		stat := ProcStat{OK: true}
		seen := map[int]bool{}
		var walk func(pid int, created int64)
		walk = func(pid int, created int64) {
			seen[pid] = true
			stat.Procs++
			for _, child := range children[pid] {
				if seen[child.PID] {
					continue
				}
				childCreated, ok := sampleProcess(child.PID, created, &stat)
				if !ok {
					continue
				}
				if pid == root {
					stat.Children = append(stat.Children, child.Name)
				}
				walk(child.PID, childCreated)
			}
		}
		created, _ := sampleProcess(root, 0, &stat)
		walk(root, created)
		// Leave CPUPercent 0 until the caller applies interval or fallback.
		stats[root] = stat
	}
	return stats
}

// sampleProcess adds one process's CPU time and working set to stat and
// returns its creation time in milliseconds. Windows never reparents: a
// process whose parent exited keeps naming that pid, so a child created
// before parentCreated is an orphan of an earlier holder of the pid and
// answers false, as does a process that cannot be opened.
func sampleProcess(pid int, parentCreated int64, stat *ProcStat) (int64, bool) {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return 0, false
	}
	created, err := proc.CreateTime()
	if err != nil || created < parentCreated {
		return 0, false
	}
	if times, err := proc.Times(); err == nil {
		stat.CPUSeconds += times.User + times.System
	}
	if mem, err := proc.MemoryInfo(); err == nil {
		stat.RSS += mem.RSS
	}
	return created, true
}
