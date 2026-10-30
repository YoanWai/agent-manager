package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnvSetListShowUnsetAndRemove(t *testing.T) {
	configDir := t.TempDir()
	var out bytes.Buffer
	if err := runEnvSet(&out, []string{"work", "CLAUDE_CONFIG_DIR=/home/me/.claude.work", "TOKEN=a=b"}, configDir); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "set CLAUDE_CONFIG_DIR TOKEN on work\n" {
		t.Fatalf("set output = %q", got)
	}
	out.Reset()
	if err := runEnvList(&out, nil, configDir); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "work  CLAUDE_CONFIG_DIR TOKEN\n" || strings.Contains(got, "a=b") {
		t.Fatalf("list output = %q, want names and keys only", got)
	}
	out.Reset()
	if err := runEnvList(&out, []string{"--json"}, configDir); err != nil {
		t.Fatal(err)
	}
	var listed []envProfileRecord
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if want := []envProfileRecord{{Name: "work", Keys: []string{"CLAUDE_CONFIG_DIR", "TOKEN"}}}; !reflect.DeepEqual(listed, want) {
		t.Fatalf("list --json = %+v, want %+v", listed, want)
	}
	out.Reset()
	if err := runEnvShow(&out, []string{"work"}, configDir); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "CLAUDE_CONFIG_DIR=/home/me/.claude.work\nTOKEN=a=b\n" {
		t.Fatalf("show output = %q", got)
	}
	out.Reset()
	if err := runEnvUnset(&out, []string{"work", "TOKEN"}, configDir); err != nil {
		t.Fatal(err)
	}
	profile, found, err := settingsStore(t, configDir).EnvProfile("work")
	if err != nil || !found || !reflect.DeepEqual(profile.Vars, map[string]string{"CLAUDE_CONFIG_DIR": "/home/me/.claude.work"}) {
		t.Fatalf("after unset = %+v, %t, %v", profile, found, err)
	}
	if err := runEnvRm(&out, []string{"work"}, configDir); err != nil {
		t.Fatal(err)
	}
	if err := runEnvRm(&out, []string{"work"}, configDir); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("second rm = %v", err)
	}
	if err := runEnvShow(&out, []string{"work"}, configDir); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("show after rm = %v", err)
	}
}

func TestEnvSetRefusesAnOperandThatIsNotAnAssignment(t *testing.T) {
	configDir := t.TempDir()
	for _, args := range [][]string{{"work", "CLAUDE_CONFIG_DIR"}, {"work"}, {"work", "AGENT_MANAGER_SESSION_ID=x"}} {
		if err := runEnvSet(&bytes.Buffer{}, args, configDir); err == nil {
			t.Errorf("set %q succeeded", args)
		}
	}
	if profiles, err := settingsStore(t, configDir).EnvProfiles(); err != nil || len(profiles) != 0 {
		t.Fatalf("a refused set stored %+v, %v", profiles, err)
	}
}

// A setup script defines its profiles before the manager has ever run.
func TestEnvSetCreatesTheConfigDirectory(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "agent-manager")
	if err := runEnvSet(&bytes.Buffer{}, []string{"work", "A=1"}, configDir); err != nil {
		t.Fatalf("set into a fresh config directory: %v", err)
	}
	if _, found, err := settingsStore(t, configDir).EnvProfile("work"); err != nil || !found {
		t.Fatalf("profile after set = %t, %v", found, err)
	}
}
