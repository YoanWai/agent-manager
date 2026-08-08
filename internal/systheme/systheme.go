// Package systheme resolves the color scheme the user's environment wants:
// the OS-level light/dark preference where a desktop exposes one, and the
// terminal's own background color as the fallback that still answers over
// SSH or on a bare server. Detection runs once, before the TUI takes the
// terminal over.
package systheme

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Scheme int

const (
	SchemeUnknown Scheme = iota
	SchemeDark
	SchemeLight
)

// commandTimeout bounds every detection command. A broken desktop bus must
// cost a beat of startup, not hang it.
const commandTimeout = 500 * time.Millisecond

type runner func(name string, args ...string) ([]byte, error)

func timedRun(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// Detect resolves the environment's preferred scheme: the OS setting is
// authoritative, the terminal background answers where no desktop setting
// is reachable, and SchemeUnknown means the caller keeps its own default.
func Detect() Scheme {
	if scheme := osScheme(timedRun); scheme != SchemeUnknown {
		return scheme
	}
	return terminalScheme(queryTerminalBg, os.Getenv)
}

func osScheme(run runner) Scheme {
	switch runtime.GOOS {
	case "darwin":
		return darwinScheme(run)
	case "linux":
		return linuxScheme(run)
	default:
		return SchemeUnknown
	}
}

// darwinScheme reads the global appearance. The AppleInterfaceStyle key
// only exists in dark mode, so `defaults read` exiting nonzero is the
// light-mode answer, and only a failure to run the binary at all reads as
// unknown.
func darwinScheme(run runner) Scheme {
	out, err := run("defaults", "read", "-g", "AppleInterfaceStyle")
	if err == nil {
		if strings.TrimSpace(string(out)) == "Dark" {
			return SchemeDark
		}
		return SchemeLight
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return SchemeLight
	}
	return SchemeUnknown
}

// linuxScheme walks the desktop's settled sources in order: the XDG
// desktop portal speaks for every modern desktop, gsettings for GNOME
// installs without a portal, and a dark-named GTK theme catches desktops
// from before the color-scheme key existed.
func linuxScheme(run runner) Scheme {
	if scheme := portalScheme(run); scheme != SchemeUnknown {
		return scheme
	}
	return gsettingsScheme(run)
}

func portalScheme(run runner) Scheme {
	out, err := run("dbus-send", "--session", "--print-reply=literal",
		"--dest=org.freedesktop.portal.Desktop",
		"/org/freedesktop/portal/desktop",
		"org.freedesktop.portal.Settings.Read",
		"string:org.freedesktop.appearance", "string:color-scheme")
	if err != nil {
		return SchemeUnknown
	}
	// The reply nests the value in variants; the trailing "uint32 <n>" is
	// the setting: 1 dark, 2 light, 0 no preference.
	reply := string(out)
	switch {
	case strings.Contains(reply, "uint32 1"):
		return SchemeDark
	case strings.Contains(reply, "uint32 2"):
		return SchemeLight
	default:
		return SchemeUnknown
	}
}

func gsettingsScheme(run runner) Scheme {
	out, err := run("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
	if err == nil {
		switch strings.Trim(strings.TrimSpace(string(out)), "'") {
		case "prefer-dark":
			return SchemeDark
		case "prefer-light":
			return SchemeLight
		}
	}
	out, err = run("gsettings", "get", "org.gnome.desktop.interface", "gtk-theme")
	if err == nil && strings.Contains(strings.ToLower(string(out)), "dark") {
		return SchemeDark
	}
	return SchemeUnknown
}

// terminalScheme classifies the terminal's own background: an OSC 11 query
// answered by the terminal (or by tmux on the pane's behalf, from its
// client's terminal), then the COLORFGBG convention some terminals export.
func terminalScheme(queryBg func() (r, g, b int, ok bool), getenv func(string) string) Scheme {
	if r, g, b, ok := queryBg(); ok {
		return luminanceScheme(r, g, b)
	}
	return colorFgBgScheme(getenv("COLORFGBG"))
}

func luminanceScheme(r, g, b int) Scheme {
	if 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) > 128 {
		return SchemeLight
	}
	return SchemeDark
}

// colorFgBgScheme reads the "<fg>;...;<bg>" convention: the last field is
// the background as an ANSI palette index. Only the indices with a settled
// meaning answer; anything else stays unknown.
func colorFgBgScheme(value string) Scheme {
	fields := strings.Split(value, ";")
	if len(fields) < 2 {
		return SchemeUnknown
	}
	index, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return SchemeUnknown
	}
	switch {
	case index == 7 || index == 15:
		return SchemeLight
	case index >= 0 && index <= 8:
		return SchemeDark
	default:
		return SchemeUnknown
	}
}
