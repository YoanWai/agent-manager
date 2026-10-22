//go:build !windows

package sysstat

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParsePSTime(t *testing.T) {
	cases := map[string]float64{
		"0:00.50":    0.5,
		"1:30.00":    90,
		"37:06.59":   37*60 + 6.59,
		"975:30.99":  975*60 + 30.99,
		"01:02:03":   1*3600 + 2*60 + 3,
		"2-01:00:00": 2*86400 + 3600,
	}
	for in, want := range cases {
		got, err := parsePSTime(in)
		if err != nil {
			t.Fatalf("parsePSTime(%q): %v", in, err)
		}
		if got < want-0.01 || got > want+0.01 {
			t.Fatalf("parsePSTime(%q) = %v, want %v", in, got, want)
		}
	}
}

// A child that exits between the two ps passes frees its pid, and a pid the
// kernel hands to something unrelated must not be read as this pane's agent.
func TestChildNamesRequireTheSampledParent(t *testing.T) {
	stats := map[int]ProcStat{100: {OK: true}}
	children := map[int][]int{100: {101, 102}}
	applyChildNames(stats, children, "  101   100 /opt/homebrew/bin/codex --resume 7\n  102   999 /usr/bin/vim notes.txt\n")
	want := []string{"/opt/homebrew/bin/codex"}
	if got := stats[100].Children; !slices.Equal(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
}

func TestPSForPIDsListsOnlyThosePIDs(t *testing.T) {
	want := []string{strconv.Itoa(os.Getpid()), strconv.Itoa(os.Getppid())}
	out, err := psForPIDs(want).Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, _ := nextField(line)
		got = append(got, pid)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("ps listed pids %v, want %v", got, want)
	}
}

// A pane shell's child that exited unreaped, such as fish leaves behind on
// its first start, is not an agent still running in the pane.
func TestTreesLeaveOutZombies(t *testing.T) {
	parent := exec.Command("sh", "-c", "sleep 0 & exec sleep 30")
	if err := parent.Start(); err != nil {
		t.Fatalf("start parent: %v", err)
	}
	t.Cleanup(func() {
		parent.Process.Kill()
		parent.Wait()
	})
	pid := parent.Process.Pid
	hasZombie := func() bool {
		out, err := exec.Command("ps", "-axo", "ppid=,stat=").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if fields := strings.Fields(line); len(fields) == 2 && fields[0] == strconv.Itoa(pid) && strings.HasPrefix(fields[1], "Z") {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(3 * time.Second); !hasZombie(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the parent never held a zombie child")
		}
	}
	stat := Trees([]int{pid})[pid]
	if !stat.OK || stat.Procs != 1 || len(stat.Children) != 0 {
		t.Fatalf("tree = %d procs, children %v, OK %v; want the parent alone", stat.Procs, stat.Children, stat.OK)
	}
}
