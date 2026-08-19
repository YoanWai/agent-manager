package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

func TestTaskVerbsDispatch(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeSessions{task: sessioncmd.Task{ID: "t1", Title: "wire the cli", State: "in_progress", OwnerName: "api-worker"}}

	if err := runTaskList(out, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("task list: %v", err)
	}
	if !strings.Contains(out.String(), "- wire the cli (t1) [in_progress] held by api-worker") {
		t.Fatalf("task list output = %q", out.String())
	}

	created := &bytes.Buffer{}
	if err := runTaskCreate(created, fake, []string{"wire the cli", "--body", "five files", "--depends-on", "a1,b2"}, "cafe0001"); err != nil {
		t.Fatalf("task create: %v", err)
	}
	if fake.title != "wire the cli" || fake.body != "five files" {
		t.Fatalf("task create got %q / %q", fake.title, fake.body)
	}
	if len(fake.dependsOn) != 2 || fake.dependsOn[0] != "a1" || fake.dependsOn[1] != "b2" {
		t.Fatalf("depends-on = %v", fake.dependsOn)
	}

	fake.taskID = "unset"
	if err := runTaskClaim(&bytes.Buffer{}, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("task claim: %v", err)
	}
	if fake.taskID != "" {
		t.Fatalf("a bare claim takes the next task, got id %q", fake.taskID)
	}
	if err := runTaskClaim(&bytes.Buffer{}, fake, []string{"t1"}, "cafe0001"); err != nil {
		t.Fatalf("task claim t1: %v", err)
	}
	if fake.taskID != "t1" {
		t.Fatalf("claim id = %q", fake.taskID)
	}

	settled := map[string]func(io.Writer, taskCommands, []string, string) error{
		"finished": runTaskFinish,
		"released": runTaskRelease,
	}
	for verb, run := range settled {
		buf := &bytes.Buffer{}
		fake.taskID = "unset"
		if err := run(buf, fake, []string{"t1"}, "cafe0001"); err != nil {
			t.Fatalf("task %s: %v", verb, err)
		}
		if fake.taskID != "t1" {
			t.Fatalf("task %s reached the layer with id %q", verb, fake.taskID)
		}
		if !strings.HasPrefix(buf.String(), verb+" wire the cli (t1)") {
			t.Fatalf("task %s output = %q", verb, buf.String())
		}
	}

	deleted := &bytes.Buffer{}
	if err := runTaskDelete(deleted, fake, []string{"t1"}, "cafe0001"); err != nil {
		t.Fatalf("task delete: %v", err)
	}
	if !fake.deleted || deleted.String() != "deleted task t1\n" {
		t.Fatalf("task delete output = %q", deleted.String())
	}
}
