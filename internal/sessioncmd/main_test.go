package sessioncmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/YoanWai/agent-manager/internal/testenv"
)

// TestMain drops an inherited GIT_DIR and its siblings, which would point
// the temp repos these tests build at the repo running them.
func TestMain(m *testing.M) {
	if err := testenv.IsolateGit(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
