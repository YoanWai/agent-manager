package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSessionCommandsParseArgumentsAndPrintSentences(t *testing.T) {
	cases := []struct {
		name    string
		run     func(*bytes.Buffer, *fakeSessions, []string) error
		args    []string
		want    string
		inspect func(*testing.T, *fakeSessions)
	}{
		{
			name: "sessions lists every row",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runSessions(out, f, args, "cafe0001")
			},
			want: "- api-worker (id beef1234) running claude in api at /repo; status=working; running=true",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.callerID != "cafe0001" {
					t.Fatalf("caller = %q", f.callerID)
				}
			},
		},
		{
			name: "send takes a target and a message",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runSend(out, f, args, "cafe0001")
			},
			args: []string{"beef1234", "ship it"},
			want: "queued message 7 for session beef1234 at position 1",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.targetID != "beef1234" || f.message != "ship it" {
					t.Fatalf("send got target %q message %q", f.targetID, f.message)
				}
			},
		},
		{
			name: "read prints the pane",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runRead(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "screen text",
		},
		{
			name: "kill names what it stopped",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runKill(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "killed api-worker (id beef1234)",
		},
		{
			name: "revive names what it brought back",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runRevive(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "revived api-worker (id beef1234)",
		},
		{
			name: "archive files a session away by default",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runArchive(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "archived api-worker (id beef1234)",
			inspect: func(t *testing.T, f *fakeSessions) {
				if !f.archived {
					t.Fatal("archive without --restore should archive")
				}
			},
		},
		{
			name: "archive --restore puts it back",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runArchive(out, f, args, "cafe0001")
			},
			args: []string{"beef1234", "--restore"},
			want: "restored api-worker (id beef1234)",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.archived {
					t.Fatal("--restore should unarchive")
				}
			},
		},
		{
			name: "archive-self asks for an archive once the turn ends",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runArchiveSelf(out, f, args, "cafe0001")
			},
			want: "this session is archived once the current turn ends",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.action != store.AfterTurnArchive || f.callerID != "cafe0001" || f.canceled {
					t.Fatalf("archive-self asked for %q as %q, canceled=%v", f.action, f.callerID, f.canceled)
				}
			},
		},
		{
			name: "kill-self asks for a kill once the turn ends",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runKillSelf(out, f, args, "cafe0001")
			},
			want: "this session is killed once the current turn ends",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.action != store.AfterTurnKill {
					t.Fatalf("kill-self asked for %q", f.action)
				}
			},
		},
		{
			name: "kill-self --cancel withdraws instead",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runKillSelf(out, f, args, "cafe0001")
			},
			args: []string{"--cancel"},
			want: "canceled the pending archive",
			inspect: func(t *testing.T, f *fakeSessions) {
				if !f.canceled || f.action != "" {
					t.Fatalf("--cancel reached the layer as canceled=%v action=%q", f.canceled, f.action)
				}
			},
		},
		{
			name: "message-status reads the id back",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runMessageStatus(out, f, args, "cafe0001")
			},
			args: []string{"7"},
			want: "message 7 to session beef is delivered",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.messageID != 7 {
					t.Fatalf("message id = %d", f.messageID)
				}
			},
		},
		{
			name: "groups lists the tree",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runGroups(out, f, args, "cafe0001")
			},
			want: "- api; sessions=2",
		},
		{
			name: "delete-group names what it removed and moved",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runDeleteGroup(out, f, args, "cafe0001")
			},
			args: []string{"fleet"},
			want: "deleted fleet; 1 session(s) moved to the root group: beef1234",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.groupPath != "fleet" || f.callerID != "cafe0001" {
					t.Fatalf("delete-group got %q as %q", f.groupPath, f.callerID)
				}
			},
		},
		{
			name: "create-group takes a path and a directory",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runCreateGroup(out, f, args, "cafe0001")
			},
			args: []string{"api/web", "--directory", "/repo"},
			want: "created group api/web",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.groupPath != "api/web" || f.directory != "/repo" {
					t.Fatalf("create-group got %q %q", f.groupPath, f.directory)
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			fake := &fakeSessions{session: sampleSession()}
			if err := testCase.run(out, fake, testCase.args); err != nil {
				t.Fatalf("run: %v", err)
			}
			if !strings.Contains(out.String(), testCase.want) {
				t.Fatalf("output = %q, want it to contain %q", out.String(), testCase.want)
			}
			if testCase.inspect != nil {
				testCase.inspect(t, fake)
			}
		})
	}
}

// Only a flag the caller typed may reach the layer: a zero value passed
// anyway would silently override the group and worktree a spawn inherits.
func TestSpawnPassesOnlyTheFlagsGiven(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeSessions{session: sampleSession()}
	args := []string{"--name", "api-worker", "--prompt", "build the api", "--tool", "claude", "--directory", "/repo", "--model", "sonnet", "--effort", "high", "--profile", "work"}
	if err := runSpawn(out, fake, args, "cafe0001"); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if fake.opts.Name != "api-worker" || fake.opts.Prompt != "build the api" || fake.opts.Tool != "claude" || fake.opts.Directory != "/repo" {
		t.Fatalf("spawn opts = %+v", fake.opts)
	}
	if fake.opts.Model != "sonnet" || fake.opts.Effort != "high" || fake.opts.Profile != "work" {
		t.Fatalf("spawn choice = %+v", fake.opts)
	}
	if fake.opts.Group != nil || fake.opts.Worktree != nil {
		t.Fatalf("untyped flags should stay inherited, got group=%v worktree=%v", fake.opts.Group, fake.opts.Worktree)
	}
	if !strings.HasPrefix(out.String(), "created api-worker (id beef1234)") {
		t.Fatalf("spawn output = %q", out.String())
	}

	explicit := &fakeSessions{session: sampleSession()}
	if err := runSpawn(&bytes.Buffer{}, explicit, []string{"--group", "", "--worktree"}, "cafe0001"); err != nil {
		t.Fatalf("spawn explicit: %v", err)
	}
	if explicit.opts.Group == nil || *explicit.opts.Group != "" {
		t.Fatalf("an explicit empty group targets the root, got %v", explicit.opts.Group)
	}
	if explicit.opts.Worktree == nil || !*explicit.opts.Worktree {
		t.Fatalf("--worktree should be passed on, got %v", explicit.opts.Worktree)
	}
}

func TestWaitCollectsStatesAndFailsOnTimeout(t *testing.T) {
	reached := &fakeSessions{wait: sessioncmd.WaitResult{
		Session: sampleSession(), Reached: true, Outcome: sessioncmd.WaitReached, Waited: "3s", ManagerAwake: true,
	}}
	out := &bytes.Buffer{}
	if err := runWait(out, reached, []string{"beef1234", "--until", "idle,finished", "--timeout", "10s"}, "cafe0001"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(reached.until) != 2 || reached.until[0] != "idle" || reached.until[1] != "finished" {
		t.Fatalf("until = %v", reached.until)
	}
	if reached.timeout != 10*time.Second {
		t.Fatalf("timeout = %s", reached.timeout)
	}
	if !strings.Contains(out.String(), "is working after 3s") {
		t.Fatalf("wait output = %q", out.String())
	}

	timedOut := &fakeSessions{wait: sessioncmd.WaitResult{
		Session: sampleSession(), Outcome: sessioncmd.WaitTimedOut, Waited: "50s", ManagerAwake: true,
	}}
	err := runWait(&bytes.Buffer{}, timedOut, []string{"beef1234"}, "cafe0001")
	if err == nil || !strings.HasPrefix(err.Error(), "timed out:") {
		t.Fatalf("a timeout should fail the command, got %v", err)
	}
}

func TestMessageStatusRefusesANonNumericID(t *testing.T) {
	err := runMessageStatus(&bytes.Buffer{}, &fakeSessions{}, []string{"seven"}, "cafe0001")
	if err == nil || !strings.Contains(err.Error(), "agent-manager send prints the id it queued") {
		t.Fatalf("error = %v, want it to say where the id comes from", err)
	}
}

func TestSendFromReachesTheLayerOnlyWhenGiven(t *testing.T) {
	plain := &fakeSessions{}
	if err := runSend(&bytes.Buffer{}, plain, []string{"beef1234", "ship it"}, "cafe0001"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if plain.from != nil {
		t.Fatalf("a send without --from went out as %q", *plain.from)
	}

	out := &bytes.Buffer{}
	remote := &fakeSessions{}
	if err := runSend(out, remote, []string{"beef1234", "ship it", "--from", "laptop-agent", "--json"}, ""); err != nil {
		t.Fatalf("send --from: %v", err)
	}
	if remote.from == nil || *remote.from != "laptop-agent" || remote.callerID != "" || remote.targetID != "beef1234" || remote.message != "ship it" {
		t.Fatalf("send --from reached the layer as from=%v caller=%q target=%q message=%q", remote.from, remote.callerID, remote.targetID, remote.message)
	}
	var result sessioncmd.SendResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.MessageID != 8 {
		t.Fatalf("send --from --json = %q (%v)", out.String(), err)
	}

	// The SSH client puts every flag first and the operands after a bare --,
	// so a message that starts with a dash stays the message.
	routed := &fakeSessions{}
	if err := runSend(&bytes.Buffer{}, routed, []string{"--from=laptop-agent", "--json", "--", "beef1234", "--force it"}, ""); err != nil {
		t.Fatalf("send --from=X --json -- id text: %v", err)
	}
	if routed.from == nil || *routed.from != "laptop-agent" || routed.targetID != "beef1234" || routed.message != "--force it" {
		t.Fatalf("routed send reached the layer as from=%v target=%q message=%q", routed.from, routed.targetID, routed.message)
	}

	// An empty name is the layer's to refuse, not a flag to drop silently.
	blank := &fakeSessions{}
	if err := runSend(&bytes.Buffer{}, blank, []string{"beef1234", "ship it", "--from="}, ""); err != nil {
		t.Fatalf("send --from=: %v", err)
	}
	if blank.from == nil || *blank.from != "" {
		t.Fatalf("an empty --from did not reach the layer: %v", blank.from)
	}
}

func TestSnapshotPrintsOnlyJSON(t *testing.T) {
	fake := &fakeSessions{session: sampleSession()}
	err := runSnapshot(&bytes.Buffer{}, fake, nil, "")
	if err == nil || err.Error() != "usage: agent-manager "+usageSnapshot {
		t.Fatalf("snapshot without --json = %v, want the usage", err)
	}
	if fake.callCount != 0 {
		t.Fatal("a refused snapshot should not reach the layer")
	}
	out := &bytes.Buffer{}
	if err := runSnapshot(out, fake, []string{"--json"}, ""); err != nil {
		t.Fatalf("snapshot --json: %v", err)
	}
	var snapshot sessioncmd.Snapshot
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil {
		t.Fatalf("snapshot --json is not JSON: %v (%q)", err, out.String())
	}
	if snapshot.Version != sessioncmd.SnapshotVersion || !snapshot.ManagerAwake || len(snapshot.Sessions) != 1 || fake.callerID != "" {
		t.Fatalf("snapshot = %+v, caller %q", snapshot, fake.callerID)
	}
}
