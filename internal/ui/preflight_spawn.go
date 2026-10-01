package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/git"
)

// directoryPreflight is the read-only filesystem/repository seam captured by
// spawn and group effects. Its calls run inside the accepted FIFO worker.
type directoryPreflight interface {
	resolve(raw string, fallbacks []string) (string, bool)
	repoCapable(dir string) bool
}

type systemDirectoryPreflight struct{ git *git.Driver }

func (p systemDirectoryPreflight) resolve(raw string, fallbacks []string) (string, bool) {
	if raw != "" {
		return resolveExistingDir(raw, "")
	}
	var resolved string
	for _, fallback := range fallbacks {
		candidate, ok := resolveExistingDir("", fallback)
		resolved = candidate
		if ok {
			return candidate, true
		}
	}
	return resolved, false
}

func (p systemDirectoryPreflight) repoCapable(dir string) bool {
	if p.git == nil || dir == "" {
		return false
	}
	_, err := p.git.RepoRoot(dir)
	return err == nil
}

func initialWorkingDir() string {
	dir, _ := os.Getwd()
	return dir
}

func initialHomeDir() string {
	dir, _ := os.UserHomeDir()
	return dir
}

// capturedAbsolutePath normalizes a displayed path without consulting the
// filesystem. It uses the startup directory/home captured before the event
// loop began.
func (m *Model) capturedAbsolutePath(raw, fallback string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		path = fallback
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = filepath.Join(m.homeDir, strings.TrimPrefix(path, "~"))
	}
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(m.workDir, path)
	}
	return filepath.Clean(path)
}

// groupDirCandidates captures the same nearest-ancestor preference as the
// old synchronous lookup. Validation happens later inside the command.
func (m *Model) groupDirCandidates(group string) []string {
	candidates := make([]string, 0, 4)
	for current := group; current != ""; current = parentGroup(current) {
		if path := m.workspace.groupPaths[current]; path != "" {
			candidates = append(candidates, path)
		}
	}
	if m.workDir != "" {
		candidates = append(candidates, m.workDir)
	}
	return candidates
}

func (m *Model) capturedGroupDefaultDir(group string) string {
	candidates := m.groupDirCandidates(group)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (m *Model) capturedAncestorGroupDir(group string) string {
	for current := group; current != ""; current = parentGroup(current) {
		if path := m.workspace.groupPaths[current]; path != "" {
			return path
		}
	}
	return ""
}
