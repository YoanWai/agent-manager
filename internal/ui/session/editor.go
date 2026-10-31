// Package session holds the value-only parts of the session lifecycle: how an
// editor line becomes a process, and what the manager says about a session it
// cannot reach.
package session

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// guiEditors are probed on PATH, in the order preferred: Settings offers
// the ones found, and the first found opens when nothing is picked.
var guiEditors = []string{"code", "cursor", "windsurf", "zed", "subl", "idea"}

// detachedEditors open a window of their own and return at once, leaving
// the manager on screen. Everything else takes the terminal over, which is
// the safer way round: an editor that draws in the terminal is simply
// broken when started detached, while a windowed one run through
// ExecProcess returns immediately and costs a repaint. An unknown name
// therefore takes the screen rather than disappearing into the background.
var detachedEditors = map[string]bool{
	"code": true, "code-insiders": true, "cursor": true, "windsurf": true,
	"zed": true, "subl": true, "idea": true,
	// The OS openers hand the path to whichever app is registered for it
	// and exit, so a configured "open -a ..." belongs here too.
	"open": true, "xdg-open": true,
}

// GUIEditors are the windowed editors offered in Settings, in preference
// order.
func GUIEditors() []string { return slices.Clone(guiEditors) }

// Resolve picks the command that opens a directory: the one Settings holds,
// then $AGENT_MANAGER_EDITOR, then a GUI editor this machine has. $VISUAL
// and $EDITOR come last because they usually name the editor set for git
// commit messages, not the one a project is meant to open in.
func Resolve(configured, manager, visual, editor string, gui []string, lookup func(string) (string, error)) string {
	for _, line := range []string{configured, manager} {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	if lookup != nil {
		for _, name := range gui {
			if _, err := lookup(name); err == nil {
				return name
			}
		}
	}
	for _, fallback := range []string{visual, editor} {
		if line := strings.TrimSpace(fallback); line != "" {
			return line
		}
	}
	return ""
}

// Launch is fully prepared by a worker command. In particular, Command has
// already resolved a bare executable through exec.LookPath, so the Update path
// only classifies and dispatches the captured process.
type Launch struct {
	Command *exec.Cmd
	Name    string
	Path    string
}

// TakesScreen reports whether the editor runs in the terminal, which the
// manager then hands over, rather than in a window of its own.
func (l Launch) TakesScreen() bool { return !Detached(l.Name) }

// Detached reports whether an editor by that name opens a window of its own
// and returns at once.
func Detached(name string) bool { return detachedEditors[name] }

// Command parses a Settings or environment line as argv, never shell code.
func Command(line, path string) (*exec.Cmd, bool) {
	argv := SplitLine(line)
	if len(argv) == 0 {
		return nil, false
	}
	args := append(append([]string{}, argv[1:]...), path)
	return exec.Command(argv[0], args...), true
}

// SplitLine splits an editor line into argv, grouping on single and
// double quotes so an argument can carry spaces ("open -a 'Visual Studio
// Code'"). Quoting is all it borrows from a shell; an unclosed quote runs
// to the end of the line rather than failing, since the line is a setting
// rather than a program.
func SplitLine(line string) []string {
	var argv []string
	var current strings.Builder
	quote := rune(0)
	quoted := false
	flush := func() {
		if current.Len() > 0 || quoted {
			argv = append(argv, current.String())
			current.Reset()
			quoted = false
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote, quoted = r, true
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return argv
}

// Name is the command word an editor line starts with: what decides where it
// draws, and what the status line calls it.
func Name(line string) string {
	argv := SplitLine(line)
	if len(argv) == 0 {
		return ""
	}
	return filepath.Base(argv[0])
}
