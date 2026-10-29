package sessioncmd

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/testenv"
)

func envName(name string) *string { return &name }

func TestSessionsCreateLaunchesUnderItsEnvironmentAndKeepsItThroughRevive(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	h := newSessionHarness(t)
	if _, err := h.store.SetEnvVars("work", map[string]string{"AM_TEST_ENV": "first"}); err != nil {
		t.Fatal(err)
	}
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "envshow", Name: "worker", Environment: envName("work")})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Environment != "work" {
		t.Fatalf("created = %+v, want environment work", created)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "env=[first]")
	// The surviving pane relaunches in place; the killed one gets a new pane.
	if _, err := h.store.SetEnvVars("work", map[string]string{"AM_TEST_ENV": "second"}); err != nil {
		t.Fatal(err)
	}
	waitForAgentGone(t, h.driver, created.ID)
	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err != nil {
		t.Fatalf("Revive in the pane: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "env=[second]")
	if _, err := h.store.SetEnvVars("work", map[string]string{"AM_TEST_ENV": "third"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Kill(h.caller.ID, created.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err != nil {
		t.Fatalf("Revive: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "env=[third]")

	if _, err := h.store.DeleteEnvProfile("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Kill(h.caller.ID, created.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err == nil || !strings.Contains(err.Error(), `environment "work" no longer exists`) {
		t.Fatalf("revive under a deleted profile = %v, want a refusal", err)
	}
}

func TestSessionsCreatePicksTheEnvironmentFromTheGroupThenTheCaller(t *testing.T) {
	h := newSessionHarness(t)
	for _, name := range []string{"work", "team"} {
		if _, err := h.store.SetEnvVars(name, map[string]string{"AM_TEST_ENV": name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.SetGroupEnvironment(h.caller.Group, "team"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.CreateGroup(h.caller.Group+"/api", ""); err != nil {
		t.Fatal(err)
	}
	inGroup, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "envshow", Name: "grouped", Group: envName(h.caller.Group + "/api")})
	if err != nil {
		t.Fatalf("Create in a subgroup: %v", err)
	}
	if inGroup.Environment != "team" {
		t.Fatalf("subgroup spawn environment = %q, want the parent group's team", inGroup.Environment)
	}
	none, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "envshow", Name: "plain", Environment: envName("")})
	if err != nil {
		t.Fatalf("Create with no environment: %v", err)
	}
	if none.Environment != "" {
		t.Fatalf("an empty environment still picked %q", none.Environment)
	}
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "envshow", Name: "root-worker", Group: envName(""), Environment: envName("work")})
	if err != nil {
		t.Fatalf("Create in the root: %v", err)
	}
	helper, err := h.sessions.Create(worker.ID, CreateSessionOptions{Tool: "envshow", Name: "helper"})
	if err != nil {
		t.Fatalf("Create beside the worker: %v", err)
	}
	if helper.Environment != "work" {
		t.Fatalf("helper environment = %q, want its caller's work", helper.Environment)
	}
	shell, err := h.terminals.Create(worker.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("terminal Create: %v", err)
	}
	row, err := h.store.Get(shell.ID)
	if err != nil || row.Environment != "work" {
		t.Fatalf("terminal environment = %q, %v; want its caller's work", row.Environment, err)
	}
}

func TestSessionsCreateRefusesAnEnvironmentThatIsNotDefined(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "envshow", Environment: envName("work")}); err == nil || !strings.Contains(err.Error(), "none is defined") {
		t.Fatalf("Create with no profiles = %v", err)
	}
	if _, err := h.store.SetEnvVars("team", map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "envshow", Environment: envName("work")}); err == nil || !strings.Contains(err.Error(), "defined are team") {
		t.Fatalf("Create with an unknown profile = %v", err)
	}
	sessions, err := h.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("a refused environment still spawned: %d rows, %v", len(sessions), err)
	}
}
