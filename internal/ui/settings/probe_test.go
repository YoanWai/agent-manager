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

func TestPrefsMirrorTheRowsTheFrameReads(t *testing.T) {
	prefs := Read(func(key string) string {
		switch key {
		case GlyphModeSetting:
			return string(presentation.GlyphModeAdvanced)
		case EditorSetting:
			return "nvim"
		case MouseSetting:
			return "on"
		case SessionLayoutSetting:
			return "full"
		}
		return ""
	})
	if prefs.GlyphMode != presentation.GlyphModeAdvanced {
		t.Fatalf("glyph mode = %q", prefs.GlyphMode)
	}
	if prefs.Editor != "nvim" {
		t.Fatalf("editor = %q", prefs.Editor)
	}
	if !prefs.Flags[MouseSetting] {
		t.Fatal("a stored on should read on")
	}
	if !prefs.Flags[SessionLayoutSetting] {
		t.Fatal("the layout row's full choice should read on")
	}
}
