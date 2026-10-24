package settings

import (
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// probeFor is the probe a machine with these editors and these shell lines
// answers, with PATH's lookups scripted.
func probeFor(installed []string, visual, editor, line string) EditorProbe {
	return EditorProbe{
		GUI:    []string{"code", "zed"},
		Visual: visual,
		Editor: editor,
		Lookup: func(name string) (string, error) {
			for _, have := range installed {
				if have == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		Line: func() string { return line },
	}
}

func TestProbeOffersTheShellsEditors(t *testing.T) {
	probed := probeFor([]string{"code"}, "code", " nvim ", "nvim").Resolve()
	if got := strings.Join(probed.Found, ","); got != "code,nvim" {
		t.Fatalf("found = %q, want code once and nvim", got)
	}
	if probed.Auto != "nvim" {
		t.Fatalf("auto = %q, want the root's launch line", probed.Auto)
	}
}

func TestProbeKeepsTheGUIEditorsPATHHas(t *testing.T) {
	probed := probeFor([]string{"zed"}, "", "", "vim").Resolve()
	if got := strings.Join(probed.Found, ","); got != "zed" {
		t.Fatalf("found = %q, want just the installed GUI editor", got)
	}
}

func TestProbeOffersTheShellLinesWithoutADuplicate(t *testing.T) {
	probed := probeFor(nil, "code", "code", "code").Resolve()
	if got := strings.Join(probed.Found, ","); got != "code" {
		t.Fatalf("found = %q, want code once", got)
	}
}

func TestMirrorReadsTheRowTheFrameCaresAbout(t *testing.T) {
	glyphs := Mirror(GlyphModeSetting, string(presentation.GlyphModeAdvanced))
	if glyphs.GlyphMode != presentation.GlyphModeAdvanced || glyphs.Flag != nil {
		t.Fatalf("the symbols row should mirror its mode only: %+v", glyphs)
	}
	editor := Mirror(EditorSetting, "nvim")
	if editor.Editor != "nvim" || editor.Flag != nil {
		t.Fatalf("the editor row should mirror its line only: %+v", editor)
	}
	mouse := Mirror(MouseSetting, "on")
	if mouse.Flag == nil || !*mouse.Flag {
		t.Fatalf("a two-way row stored on should mirror on: %+v", mouse)
	}
	layout := Mirror(SessionLayoutSetting, "full")
	if layout.Flag == nil || !*layout.Flag {
		t.Fatalf("the layout row's full choice should mirror on: %+v", layout)
	}
	if unset := Mirror("theme", "nord"); unset != (Mirrored{}) {
		t.Fatalf("a key the frame never reads should mirror nothing: %+v", unset)
	}
}
