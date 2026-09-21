package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

func fleetFixture(t *testing.T) (*fleetSessions, *fleetTerminals, *fakeSessionCommands, *fakeTerminalCommands, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	t.Setenv("FLEET_TEST_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$FLEET_TEST_LOG"
case "$*" in
 *"'sessions'"*) printf '%s' '[{"id":"same","name":"remote","group":"work","running":true}]' ;;
 *"'terminal' 'list'"*) printf '%s' '[{"id":"term","name":"web","group":"work","parent_id":"same"}]' ;;
 *"'groups'"*) printf '%s' '[{"path":"work","directory":"/srv/work"}]' ;;
 *"'terminal' 'read'"*) printf '%s' '{"terminal":{"id":"term"},"output":"terminal output"}' ;;
 *"'terminal' 'create'"*) printf '%s' '{"id":"term","name":"new shell"}' ;;
 *"'terminal' 'send'"*) printf '%s' '{"terminal_id":"term","sent":"command"}' ;;
 *"'terminal' 'close'"*) printf '%s' 'closed' ;;
 *"'read'"*) printf '%s' '{"session":{"id":"same"},"output":"remote output"}' ;;
 *"'send'"*) printf '%s' '{"message_id":42,"manager_awake":true}' ;;
 *"'message-status'"*) printf '%s' '{"message_id":42,"status":"delivered"}' ;;
 *"'wait'"*) printf '%s' '{"session":{"id":"same"},"reached":true}' ;;
 *) printf '%s' '{"id":"new","name":"created"}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fleet.json"), []byte(`{"hosts":[{"name":"mac"},{"name":"floripa","ssh":"floripa-agent","controller":"ctl","binary":"/usr/local/bin/agent-manager"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	local := &fakeSessionCommands{listed: []sessioncmd.Session{{ID: "same", Name: "local"}}}
	term := &fakeTerminalCommands{}
	s, ts := federate(dir, local, term)
	return s.(*fleetSessions), ts.(*fleetTerminals), local, term, log
}
func TestFleetMCPListsQualifiedSessionsAndGroups(t *testing.T) {
	s, ts, _, _, _ := fleetFixture(t)
	rows, err := s.List("caller")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "mac::same" || rows[1].ID != "floripa::same" {
		t.Fatalf("%+v", rows)
	}
	groups, err := s.Groups("caller")
	if err != nil || len(groups) != 1 || groups[0].Path != "floripa::work" {
		t.Fatalf("%+v %v", groups, err)
	}
	terms, err := ts.List("caller")
	if err != nil || terms[0].ParentID != "floripa::same" {
		t.Fatalf("%+v %v", terms, err)
	}
}
func TestFleetMCPRoutesLocalWithoutSSHAndRemoteThroughCLI(t *testing.T) {
	s, _, local, _, log := fleetFixture(t)
	if _, err := s.Send("caller", "mac::same", "local message"); err != nil {
		t.Fatal(err)
	}
	if local.sentID != "same" {
		t.Fatal(local.sentID)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("local operation called SSH")
	}
	sent, err := s.Send("caller", "floripa::same", "remote message")
	if err != nil || sent.MessageID != 42 {
		t.Fatalf("%+v %v", sent, err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "'send' 'same' 'remote message'") {
		t.Fatal(string(b))
	}
	screen, err := s.Read("caller", "floripa::same")
	if err != nil || screen.Session.ID != "floripa::same" {
		t.Fatalf("%+v %v", screen, err)
	}
}
func TestFleetMCPSpawnAndLifecycleStayOnSelectedHost(t *testing.T) {
	s, _, local, _, log := fleetFixture(t)
	group := "floripa::work"
	worktree := true
	created, err := s.Create("caller", sessioncmd.CreateSessionOptions{Group: &group, Name: "test", Directory: "/srv/work", Worktree: &worktree})
	if err != nil || created.ID != "floripa::new" {
		t.Fatalf("%+v %v", created, err)
	}
	if local.createdOpts.Name != "" {
		t.Fatal("remote spawn touched local")
	}
	if _, err = s.Archive("caller", "floripa::same", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "'--group' 'work'") || !strings.Contains(string(b), "'--worktree=true'") || !strings.Contains(string(b), "'archive' 'same' '--json' '--restore'") {
		t.Fatal(string(b))
	}
}
func TestFleetMCPTerminalActionsKeepQualifiedIdentity(t *testing.T) {
	_, ts, _, local, log := fleetFixture(t)
	sent, err := ts.Send("caller", "floripa::term", "pwd", nil)
	if err != nil || sent.TerminalID != "floripa::term" || local.sentID != "" {
		t.Fatalf("%+v %v", sent, err)
	}
	screen, err := ts.Read("caller", "floripa::term")
	if err != nil || screen.Terminal.ID != "floripa::term" {
		t.Fatalf("%+v %v", screen, err)
	}
	if err = ts.Close("caller", "floripa::term"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "'terminal' 'close' 'term'") {
		t.Fatal(string(b))
	}
}
func TestFleetMCPRejectsUnknownHostBeforeLocalMutation(t *testing.T) {
	s, _, local, _, log := fleetFixture(t)
	if _, err := s.Kill("caller", "unknown::same"); err == nil {
		t.Fatal("unknown host accepted")
	}
	if local.killedID != "" {
		t.Fatal("local kill happened")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("SSH happened")
	}
}
func TestFleetMCPWaitAndMessageStatusRoute(t *testing.T) {
	s, _, _, _, log := fleetFixture(t)
	if _, err := s.Wait(context.Background(), "caller", "floripa::same", []string{"finished"}, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MessageStatusHost("caller", "floripa", 42); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "'wait' 'same'") || !strings.Contains(string(b), "'message-status' '42'") {
		t.Fatal(string(b))
	}
}
func TestRoutedGroupRejectsConflictingExplicitHost(t *testing.T) {
	group := "floripa::work"
	if _, err := routedGroup("mac", &group); err == nil {
		t.Fatal("conflicting host accepted")
	}
}

func TestFleetMCPOfflineRemotePreservesLocalRows(t *testing.T) {
	s, _, _, _, log := fleetFixture(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(log), "ssh"), []byte("#!/bin/sh\nprintf offline >&2\nexit 255\n"), 0700); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List("caller")
	if err == nil || !strings.Contains(err.Error(), "floripa") || len(rows) != 1 || rows[0].ID != "mac::same" {
		t.Fatalf("%+v %v", rows, err)
	}
}
func TestFleetMCPRepeatedListDoesNotDoubleQualifyNativeRows(t *testing.T) {
	s, _, local, _, _ := fleetFixture(t)
	for range 2 {
		rows, err := s.List("caller")
		if err != nil || rows[0].ID != "mac::same" {
			t.Fatalf("%+v %v", rows, err)
		}
	}
	if local.listed[0].ID != "same" {
		t.Fatal("mutated source row")
	}
}
