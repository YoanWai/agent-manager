package composer

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

func TestRefusedSubmissionRestoresSavedPromptForRetry(t *testing.T) {
	prompt := newComposer(SavedToken("Role") + " task")
	prompt.Saved = []spawn.Saved{{Name: "Role", Text: "instructions"}}
	submission := prompt.Submission()
	prompt.Accept(submission)
	prompt.Restore(submission)
	if got := prompt.Message(); got != "instructions task" {
		t.Fatalf("retry prompt = %q", got)
	}
	retry := prompt.Submission()
	if retry.ID == submission.ID {
		t.Fatal("retry reused the earlier submission identity")
	}
}

func TestLateRefusalPreservesNewerPrompt(t *testing.T) {
	for _, state := range []string{"typing", "submitted", "image", "saved prompt"} {
		t.Run(state, func(t *testing.T) {
			prompt := newComposer("first")
			first := prompt.Submission()
			prompt.Accept(first)
			switch state {
			case "typing":
				prompt.Input.SetValue("second")
			case "submitted":
				prompt.Input.SetValue("second")
				prompt.Accept(prompt.Submission())
			case "image":
				prompt.Attachments = []Attachment{{ID: 1, Path: "next.png"}}
			case "saved prompt":
				prompt.Saved = []spawn.Saved{{Name: "Role", Text: "instructions"}}
			}
			before := prompt.Input.Value()
			prompt.Restore(first)
			if got := prompt.Input.Value(); got != before {
				t.Fatalf("late refusal replaced %q with %q", before, got)
			}
		})
	}
}
