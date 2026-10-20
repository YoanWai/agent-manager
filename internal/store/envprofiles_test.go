package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestEnvProfilesSetMergesUnsetAndDelete(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.SetEnvVars("work", map[string]string{"CLAUDE_CONFIG_DIR": "/home/me/.claude.work", "KUBECONFIG": "/k"}); err != nil {
		t.Fatal(err)
	}
	merged, err := st.SetEnvVars("work", map[string]string{"KUBECONFIG": "/k2", "EMPTY": ""})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"CLAUDE_CONFIG_DIR": "/home/me/.claude.work", "KUBECONFIG": "/k2", "EMPTY": ""}
	if !reflect.DeepEqual(merged.Vars, want) {
		t.Fatalf("merged = %v, want %v", merged.Vars, want)
	}
	if _, err := st.SetEnvVars("alpha", map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	profiles, err := st.EnvProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[0].Name != "alpha" || profiles[1].Name != "work" || !reflect.DeepEqual(profiles[1].Vars, want) {
		t.Fatalf("profiles = %#v", profiles)
	}
	unset, err := st.UnsetEnvVars("work", []string{"KUBECONFIG", "EMPTY", "NEVER_SET"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unset.Vars, map[string]string{"CLAUDE_CONFIG_DIR": "/home/me/.claude.work"}) {
		t.Fatalf("unset = %v", unset.Vars)
	}
	if _, err := st.UnsetEnvVars("missing", []string{"A"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unset on a missing profile: %v", err)
	}
	deleted, err := st.DeleteEnvProfile("work")
	if err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	if deleted, _ := st.DeleteEnvProfile("work"); deleted {
		t.Fatal("deleted a profile twice")
	}
	if _, found, err := st.EnvProfile("work"); found || err != nil {
		t.Fatalf("EnvProfile after delete = %v, %v", found, err)
	}
}

func TestEnvProfilesRefuseWhatCannotBeExported(t *testing.T) {
	st := newTestStore(t)
	for _, tc := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"", map[string]string{"A": "1"}, "1 to 60"},
		{" work", map[string]string{"A": "1"}, "1 to 60"},
		{strings.Repeat("x", 61), map[string]string{"A": "1"}, "1 to 60"},
		{"bad\x01", map[string]string{"A": "1"}, "cannot be saved"},
		{"work", nil, "at least one"},
		{"work", map[string]string{"1A": "1"}, "cannot start with a digit"},
		{"work", map[string]string{"A-B": "1"}, "letters, digits"},
		{"work", map[string]string{"AGENT_MANAGER_SESSION_ID": "x"}, "agent-manager itself"},
		{"work", map[string]string{"agent_manager_launch": "x"}, "agent-manager itself"},
		{"work", map[string]string{"A": "a\x00b"}, "NUL"},
	} {
		if _, err := st.SetEnvVars(tc.name, tc.vars); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SetEnvVars(%q, %v) = %v, want an error naming %q", tc.name, tc.vars, err, tc.want)
		}
	}
	if profiles, _ := st.EnvProfiles(); len(profiles) != 0 {
		t.Fatalf("a refused set stored %#v", profiles)
	}
}

func TestLaunchEnvRefusesAProfileThatIsGone(t *testing.T) {
	st := newTestStore(t)
	if vars, err := st.LaunchEnv(""); vars != nil || err != nil {
		t.Fatalf("LaunchEnv(\"\") = %v, %v", vars, err)
	}
	if _, err := st.SetEnvVars("work", map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if vars, err := st.LaunchEnv("work"); err != nil || vars["A"] != "1" {
		t.Fatalf("LaunchEnv(work) = %v, %v", vars, err)
	}
	if _, err := st.LaunchEnv("gone"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("LaunchEnv(gone) = %v", err)
	}
}

func TestSessionAndGroupKeepTheirEnvironment(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{ID: "s1", Name: "one", Tool: "claude", Cwd: "/tmp", Group: "office", Status: "idle", Environment: "work"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("s1")
	if err != nil || got.Environment != "work" {
		t.Fatalf("Get = %q, %v", got.Environment, err)
	}
	listed, err := st.ListSessions(false)
	if err != nil || len(listed) != 1 || listed[0].Environment != "work" {
		t.Fatalf("ListSessions = %#v, %v", listed, err)
	}
	if err := st.AddGroup("team", "/tmp", "", "", "work"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroupEnvironment("office", "work"); err != nil {
		t.Fatal(err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Environment != "work" {
			t.Fatalf("group %s environment = %q", g.Name, g.Environment)
		}
	}
	if err := st.RenameGroup("team", "crew"); err != nil {
		t.Fatal(err)
	}
	groups, _ = st.Groups()
	for _, g := range groups {
		if g.Name == "crew" && g.Environment != "work" {
			t.Fatalf("rename dropped the environment: %#v", g)
		}
	}
}
