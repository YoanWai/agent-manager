package composer

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

func (c *Composer) Submission() spawn.Submission {
	return spawn.Submission{ID: c.submitted + 1, Draft: c.Input.Value(), Saved: slices.Clone(c.Saved)}
}

// Accepted images belong to the delivery request.
func (c *Composer) Accept(submission spawn.Submission) {
	c.submitted = submission.ID
	c.Input.SetValue("")
	c.Saved = nil
	c.Attachments = nil
}

func (c *Composer) Untouched(id uint64) bool {
	return c.submitted == id && c.Input.Value() == "" && len(c.Saved) == 0 && len(c.Attachments) == 0
}

func (c *Composer) Restore(submission spawn.Submission) bool {
	if !c.Untouched(submission.ID) {
		return false
	}
	c.Input.SetValue(submission.Draft)
	c.Saved = submission.Saved
	return true
}
