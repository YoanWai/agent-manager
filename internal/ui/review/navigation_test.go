package review_test

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/review"
)

// A compiled artifact is dropped on its name alone, which is what a file git
// has not classified yet needs: an untracked one carries no numstat verdict,
// and nothing sniffs its bytes until the cursor reaches it.
func TestNonCodePathNamesCompiledArtifacts(t *testing.T) {
	hidden := []string{
		"build/Main.class", "app/__pycache__/mod.pyc", "assets/logo.PNG",
		"go.sum", "Cargo.lock", "web/package-lock.json", "vendor/lib.so",
	}
	for _, path := range hidden {
		if !review.IsNonCodePath(path) {
			t.Errorf("%q should be filtered out of a code-only review", path)
		}
	}
	shown := []string{"main.go", "Main.java", "mod.py", "readme.md", "classy.go"}
	for _, path := range shown {
		if review.IsNonCodePath(path) {
			t.Errorf("%q is source and should stay in the review", path)
		}
	}
}
