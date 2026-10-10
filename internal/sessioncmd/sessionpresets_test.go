package sessioncmd

import (
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSessionsCreatePresetPreservesLiteralInstructions(t *testing.T) {
	for _, mode := range []string{"argument", "send"} {
		for _, task := range []string{" \t investigate 界 \r\n", " \t- task bullet\n", " \t\n"} {
			t.Run(mode+"/"+strings.TrimSpace(task), func(t *testing.T) {
				h := newSessionHarness(t)
				if mode == "send" {
					h.sessions.loadConfig = testConfigLoader(t, sessionConfig+"\n[tools.sender]\ncommand = \"cat\"\nprompt_mode = \"send\"\n")
				}
				instructions := "- keep literal 界 ⇥␍\r\n\t  indented\n\n "
				if err := h.store.SaveSessionPreset("", store.SessionPreset{Name: "project/reviewer", Instructions: instructions}); err != nil {
					t.Fatal(err)
				}
				opts := CreateSessionOptions{Name: "literal", Preset: "project/reviewer", Prompt: task}
				if mode == "send" {
					opts.Tool = "sender"
				}
				created, err := h.sessions.Create(h.caller.ID, opts)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := h.store.Get(created.ID)
				if err != nil {
					t.Fatal(err)
				}
				payload := instructions
				if task := strings.TrimSpace(task); task != "" {
					payload += "\n\n" + task
				}
				expected := launch.OnRequestCoordinationNote + "\n\n" + launch.RenameAvailableNote + "\n\n" + payload
				if mode == "send" {
					if stored.LaunchPrompt != "" || !slices.Equal(stored.PendingInputs, []string{expected}) {
						t.Fatalf("send plan: launch=%q pending=%q; want %q", stored.LaunchPrompt, stored.PendingInputs, expected)
					}
				} else {
					if stored.LaunchPrompt != expected || len(stored.PendingInputs) != 0 {
						t.Fatalf("argument plan: launch=%q pending=%q; want %q", stored.LaunchPrompt, stored.PendingInputs, expected)
					}
					waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "keep literal")
				}
			})
		}
	}
}

func TestSessionsCreatePresetRefusesBeforeSpawnSideEffects(t *testing.T) {
	for _, name := range []string{"missing", " ", "bad\nname"} {
		t.Run(name, func(t *testing.T) {
			h := newSessionHarness(t)
			before, err := h.driver.Panes()
			if err != nil {
				t.Fatal(err)
			}
			h.sessions.newGit = func() (*git.Driver, error) {
				t.Fatal("preset refusal reached worktree preparation")
				return nil, errors.New("unexpected git")
			}
			worktree := true
			_, err = h.sessions.Create(h.caller.ID, CreateSessionOptions{Preset: name, Prompt: "task", Worktree: &worktree})
			if err == nil || !strings.Contains(err.Error(), "preset") {
				t.Fatalf("preset refusal: %v", err)
			}
			rows, err := h.store.ListSessions(true)
			if err != nil || len(rows) != 1 {
				t.Fatalf("refused creation wrote rows: %+v %v", rows, err)
			}
			after, err := h.driver.Panes()
			if err != nil || len(after) != len(before) {
				t.Fatalf("refused creation opened panes: %+v %v", after, err)
			}
		})
	}
}

func TestSessionsCreatePresetCatalogFailureDoesNotFallBack(t *testing.T) {
	h := newSessionHarness(t)
	db, err := sql.Open("sqlite", filepath.Join(h.sessions.configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE session_presets RENAME COLUMN instructions TO unavailable"); err != nil {
		t.Fatal(err)
	}
	h.sessions.newGit = func() (*git.Driver, error) {
		t.Fatal("catalog failure reached worktree preparation")
		return nil, errors.New("unexpected git")
	}
	worktree := true
	_, err = h.sessions.Create(h.caller.ID, CreateSessionOptions{Preset: "saved", Prompt: "task", Worktree: &worktree})
	if err == nil || !strings.Contains(err.Error(), "preset") || !strings.Contains(err.Error(), "instructions") {
		t.Fatalf("catalog error: %v", err)
	}
	rows, err := h.store.ListSessions(true)
	if err != nil || len(rows) != 1 {
		t.Fatalf("catalog error spawned: %+v %v", rows, err)
	}
	// No selection still uses the existing native task path even if the catalog cannot be read.
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "plain", Prompt: " \t plain task \r\n"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := launch.OnRequestCoordinationNote + "\n\n" + launch.RenameAvailableNote + "\n\nplain task"
	if stored.LaunchPrompt != expected {
		t.Fatalf("plain task changed: %q", stored.LaunchPrompt)
	}
}

func TestSessionsCreatePresetReadsFreshWithoutRewritingQueuedPrompt(t *testing.T) {
	h := newSessionHarness(t)
	h.sessions.loadConfig = testConfigLoader(t, sessionConfig+"\n[tools.sender]\ncommand = \"cat\"\nprompt_mode = \"send\"\nrevive_command = \"cat\"\n")
	preset := store.SessionPreset{Name: "project/reviewer", Instructions: "  old instructions\n"}
	if err := h.store.SaveSessionPreset("", preset); err != nil {
		t.Fatal(err)
	}
	opts := CreateSessionOptions{Name: "old", Tool: "sender", Preset: preset.Name, Prompt: " \t task \n"}
	old, err := h.sessions.Create(h.caller.ID, opts)
	if err != nil {
		t.Fatal(err)
	}
	original, err := h.store.Get(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	expectedOld := launch.OnRequestCoordinationNote + "\n\n" + launch.RenameAvailableNote + "\n\n  old instructions\n\n\ntask"
	if !slices.Equal(original.PendingInputs, []string{expectedOld}) {
		t.Fatalf("old queued prompt: %q", original.PendingInputs)
	}
	preset.Instructions = "\t new instructions\r\n"
	if err := h.store.SaveSessionPreset(preset.Name, preset); err != nil {
		t.Fatal(err)
	}
	opts.Name = "new"
	fresh, err := h.sessions.Create(h.caller.ID, opts)
	if err != nil {
		t.Fatal(err)
	}
	current, err := h.store.Get(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	expectedNew := launch.OnRequestCoordinationNote + "\n\n" + launch.RenameAvailableNote + "\n\n\t new instructions\r\n\n\ntask"
	if !slices.Equal(current.PendingInputs, []string{expectedNew}) {
		t.Fatalf("fresh prompt: %q", current.PendingInputs)
	}
	deleted, err := h.store.DeleteSessionPreset(preset.Name)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	unchanged, err := h.store.Get(old.ID)
	if err != nil || !slices.Equal(unchanged.PendingInputs, original.PendingInputs) {
		t.Fatalf("catalog edits rewrote queued instructions: %+v %v", unchanged, err)
	}
	// The actual pending-delivery store path receives the already composed bytes.
	claimed, err := h.store.ClaimPendingInput(old.ID, expectedOld)
	if err != nil || !claimed {
		t.Fatalf("claim composed prompt: %v %v", claimed, err)
	}
	consumed, err := h.store.ConsumeClaimedPendingInput(old.ID, expectedOld)
	if err != nil || !consumed {
		t.Fatalf("consume composed prompt: %v %v", consumed, err)
	}
	if _, err := h.sessions.Create(h.caller.ID, opts); err == nil {
		t.Fatal("deleted preset silently fell back")
	}
	if err := h.driver.Kill(fresh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Revive(h.caller.ID, fresh.ID); err != nil {
		t.Fatalf("revive reread deleted preset: %v", err)
	}
}

func TestSessionsCreateWithoutPresetKeepsTaskNormalization(t *testing.T) {
	h := newSessionHarness(t)
	if err := h.store.SaveSessionPreset("", store.SessionPreset{Name: "unused", Instructions: "unselected instructions"}); err != nil {
		t.Fatal(err)
	}
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "plain", Prompt: " \t plain task \r\n"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := launch.OnRequestCoordinationNote + "\n\n" + launch.RenameAvailableNote + "\n\nplain task"
	if stored.LaunchPrompt != expected {
		t.Fatalf("no-preset prompt changed: %q", stored.LaunchPrompt)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: " \t - legacy flag \n"}); err == nil {
		t.Fatal("no-preset leading dash lost its rejection")
	}
}
