package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/update"
)

// swapUpdateSeams steers the update path: exec, detection and the refresh
// catalog; apply is swapped per test where the test needs to see it.
func swapUpdateSeams(t *testing.T, execPath string, manager update.Manager, result update.Result, refreshErr error) {
	t.Helper()
	oldExec, oldDetect, oldRefresh := updateExecutable, updateDetect, updateRefresh
	updateExecutable = func() (string, error) { return execPath, nil }
	updateDetect = func(string) update.Manager { return manager }
	updateRefresh = func(context.Context, string, string) (update.Result, error) { return result, refreshErr }
	t.Cleanup(func() { updateExecutable, updateDetect, updateRefresh = oldExec, oldDetect, oldRefresh })
}

func TestUpdateShowsUsageOnHelp(t *testing.T) {
	var out bytes.Buffer
	err := runUpdate("0.31.0")(&out, []string{"-h"}, "", t.TempDir())
	if !errors.Is(err, ErrUsageShown) {
		t.Fatalf("update -h returned %v, want ErrUsageShown", err)
	}
	if !strings.Contains(out.String(), usageUpdate) {
		t.Fatalf("update -h output = %q", out.String())
	}
}

func TestUpdateRejectsOperands(t *testing.T) {
	if err := runUpdate("0.31.0")(&bytes.Buffer{}, []string{"extra"}, "", t.TempDir()); err == nil {
		t.Fatal("update with an operand returned a nil error")
	}
}

func TestUpdateReportsAdviceOnlyManagers(t *testing.T) {
	swapUpdateSeams(t, "agent-manager",
		update.Manager{Name: "pacman", Advice: "install an AUR helper first, then: yay -S agent-manager-bin"},
		update.Result{}, nil)
	err := runUpdate("0.31.0")(&bytes.Buffer{}, nil, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "AUR helper") {
		t.Fatalf("update with advice returned %v", err)
	}
}

func TestUpdateDelegatedRunsTheManagerCommand(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{Name: "true", Command: []string{"true"}}, update.Result{}, nil)
	var out bytes.Buffer
	if err := runUpdate("0.31.0")(&out, nil, "", t.TempDir()); err != nil {
		t.Fatalf("delegated update: %v", err)
	}
	if !strings.Contains(out.String(), "updated with true") {
		t.Fatalf("delegated update output = %q", out.String())
	}
}

func TestUpdateDelegatedReportsFailure(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{Name: "false", Command: []string{"false"}}, update.Result{}, nil)
	err := runUpdate("0.31.0")(&bytes.Buffer{}, nil, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "false:") {
		t.Fatalf("failed delegated update returned %v", err)
	}
}

func TestUpdateReportsUpToDate(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{},
		update.Result{Releases: []update.Release{{Version: "v0.31.0"}}}, nil)
	var out bytes.Buffer
	if err := runUpdate("0.31.0")(&out, nil, "", t.TempDir()); err != nil {
		t.Fatalf("up-to-date update: %v", err)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("up-to-date output = %q", out.String())
	}
}

func TestUpdateRefusesNonReleaseBuilds(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{}, update.Result{}, nil)
	err := runUpdate("dev")(&bytes.Buffer{}, nil, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a release") {
		t.Fatalf("dev build update returned %v", err)
	}
}

func TestUpdateSwapsTheBinaryForTheLatestRelease(t *testing.T) {
	swapUpdateSeams(t, "/bin/agent-manager", update.Manager{}, update.Result{Latest: "v0.32.0"}, nil)
	var appliedTag, appliedPath string
	oldApply := updateApply
	updateApply = func(_ context.Context, tag, path string) error {
		appliedTag, appliedPath = tag, path
		return nil
	}
	t.Cleanup(func() { updateApply = oldApply })
	var out bytes.Buffer
	if err := runUpdate("0.31.0")(&out, nil, "", t.TempDir()); err != nil {
		t.Fatalf("update: %v", err)
	}
	if appliedTag != "v0.32.0" || appliedPath != "/bin/agent-manager" {
		t.Fatalf("apply got (%q, %q), want (v0.32.0, /bin/agent-manager)", appliedTag, appliedPath)
	}
	if !strings.Contains(out.String(), "updating to v0.32.0...") {
		t.Fatalf("update output = %q", out.String())
	}
	if !strings.Contains(out.String(), "updated to v0.32.0") {
		t.Fatalf("update output = %q", out.String())
	}
}

func TestUpdateJSONReportsTheVersion(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{}, update.Result{Latest: "v0.32.0"}, nil)
	oldApply := updateApply
	updateApply = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { updateApply = oldApply })
	var out bytes.Buffer
	if err := runUpdate("0.31.0")(&out, []string{"--json"}, "", t.TempDir()); err != nil {
		t.Fatalf("update --json: %v", err)
	}
	if !strings.Contains(out.String(), `"version": "v0.32.0"`) {
		t.Fatalf("update --json output = %q", out.String())
	}
	if strings.Contains(out.String(), "updating to") {
		t.Fatalf("update --json output carries the human progress line: %q", out.String())
	}
}

func TestUpdateReportsRefreshFailures(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{}, update.Result{}, errors.New("github unreachable"))
	err := runUpdate("0.31.0")(&bytes.Buffer{}, nil, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "github unreachable") {
		t.Fatalf("refresh failure returned %v", err)
	}
}

func TestUpdateReportsApplyFailures(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{}, update.Result{Latest: "v0.32.0"}, nil)
	oldApply := updateApply
	updateApply = func(context.Context, string, string) error { return errors.New("disk full") }
	t.Cleanup(func() { updateApply = oldApply })
	err := runUpdate("0.31.0")(&bytes.Buffer{}, nil, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("apply failure returned %v", err)
	}
}

func TestUpdateExecutableFailureReachesTheCaller(t *testing.T) {
	oldExec := updateExecutable
	updateExecutable = func() (string, error) { return "", errors.New("no executable") }
	t.Cleanup(func() { updateExecutable = oldExec })
	err := runUpdate("0.31.0")(&bytes.Buffer{}, nil, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no executable") {
		t.Fatalf("executable failure returned %v", err)
	}
}

func TestUpdateUpToDateJSONReportsIt(t *testing.T) {
	swapUpdateSeams(t, "agent-manager", update.Manager{},
		update.Result{Releases: []update.Release{{Version: "v0.31.0"}}}, nil)
	var out bytes.Buffer
	if err := runUpdate("0.31.0")(&out, []string{"--json"}, "", t.TempDir()); err != nil {
		t.Fatalf("up-to-date --json: %v", err)
	}
	if !strings.Contains(out.String(), `"up_to_date": true`) {
		t.Fatalf("up-to-date --json output = %q", out.String())
	}
}

func TestUpdateDelegatedJSONKeepsTheRecordClean(t *testing.T) {
	swapUpdateSeams(t, "agent-manager",
		update.Manager{Name: "echo", Command: []string{"echo", "progress-line"}}, update.Result{}, nil)
	var out bytes.Buffer
	if err := runUpdate("0.31.0")(&out, []string{"--json"}, "", t.TempDir()); err != nil {
		t.Fatalf("delegated --json: %v", err)
	}
	if !strings.Contains(out.String(), `"delegate": "echo progress-line"`) {
		t.Fatalf("delegated --json output = %q", out.String())
	}
	// The manager's own stdout must not share the stream with the record.
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 3 {
		t.Fatalf("delegated --json output holds %d lines, want the record only: %q", len(lines), out.String())
	}
}
