// Package editor resolves an editor line into the process that opens a
// directory, and probes what this machine has for the Settings row.
package editor

import (
	"os"
	"slices"
	"strings"

	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

// EditorResolution is the editor line the manager launches with: the one
// Settings holds, else $AGENT_MANAGER_EDITOR, else a windowed editor this
// machine has, else $VISUAL or $EDITOR.
type EditorResolution struct {
	configured string
	manager    string
	visual     string
	editor     string
	gui        []string
	lookup     func(string) (string, error)
}

// Resolution captures the environment, and the PATH lookup that decides which
// windowed editor an unset Settings row falls back to.
func Resolution(configured string, lookup func(string) (string, error)) EditorResolution {
	return EditorResolution{
		configured: configured,
		manager:    os.Getenv("AGENT_MANAGER_EDITOR"),
		visual:     os.Getenv("VISUAL"),
		editor:     os.Getenv("EDITOR"),
		gui:        uisession.GUIEditors(),
		lookup:     lookup,
	}
}

func (r EditorResolution) Line() string {
	return uisession.Resolve(r.configured, r.manager, r.visual, r.editor, r.gui, r.lookup)
}

func (r EditorResolution) Prepare(path string) uisession.Launch {
	line := r.Line()
	if command, ok := uisession.Command(line, path); ok {
		return uisession.Launch{Command: command, Name: uisession.Name(line), Path: path}
	}
	return uisession.Launch{}
}

// Probe is what Settings offers: the editors found on PATH, and the line an
// unset row resolves to.
func (r EditorResolution) Probe() Probe {
	var found []string
	if r.lookup != nil {
		for _, name := range r.gui {
			if _, err := r.lookup(name); err == nil {
				found = append(found, name)
			}
		}
	}
	for _, line := range []string{r.visual, r.editor} {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(found, line) {
			found = append(found, line)
		}
	}
	r.configured = ""
	return Probe{Found: found, Auto: r.Line()}
}

type Probe struct {
	Found []string
	Auto  string
}
