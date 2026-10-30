package execution

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/agentsession"
)

// rowLines only asks for a transcript after KeepsTranscript, so a style
// that has none is not a missing file. Swallowing that error hides a
// caller that asked for a record this package does not read.
func TestResolveTranscriptReturnsAStyleItDoesNotRead(t *testing.T) {
	_, err := resolveTranscript("cursor", t.TempDir(), "", "sess")
	if !errors.Is(err, agentsession.ErrNoTranscript) {
		t.Fatalf("err = %v, want ErrNoTranscript", err)
	}
}
