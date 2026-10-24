package editor

import (
	"errors"
	"strings"
	"testing"
)

// probeFor is a machine with these editors installed and these shell lines,
// with PATH's lookups scripted.
func probeFor(installed []string, visual, editor string) EditorResolution {
	return EditorResolution{
		visual: visual,
		editor: editor,
		gui:    []string{"code", "zed"},
		lookup: func(name string) (string, error) {
			for _, have := range installed {
				if have == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
	}
}

func TestProbeOffersTheShellsEditors(t *testing.T) {
	probed := probeFor([]string{"code"}, "code", " nvim ").Probe()
	if got := strings.Join(probed.Found, ","); got != "code,nvim" {
		t.Fatalf("found = %q, want code once and nvim", got)
	}
}

func TestProbeKeepsTheGUIEditorsPATHHas(t *testing.T) {
	probed := probeFor([]string{"zed"}, "", "").Probe()
	if got := strings.Join(probed.Found, ","); got != "zed" {
		t.Fatalf("found = %q, want just the installed GUI editor", got)
	}
}

func TestProbeOffersTheShellLinesWithoutADuplicate(t *testing.T) {
	probed := probeFor(nil, "code", "code").Probe()
	if got := strings.Join(probed.Found, ","); got != "code" {
		t.Fatalf("found = %q, want code once", got)
	}
}

// A configured editor does not hide what an unset row would fall back to.
func TestProbeAutoIgnoresTheConfiguredLine(t *testing.T) {
	r := probeFor([]string{"zed"}, "", "")
	r.configured = "nvim"
	if auto := r.Probe().Auto; auto != "zed" {
		t.Fatalf("auto = %q, want the PATH editor", auto)
	}
}
