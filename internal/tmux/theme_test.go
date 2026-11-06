package tmux

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// clearPaneTheme drops the server-global colors a pane-theme test left
// behind, so the rest of the package sees an unstyled server.
func clearPaneTheme(t *testing.T) {
	t.Helper()
	tmuxCmd("set-option", "-gu", "window-style").Run()
	tmuxCmd("set-environment", "-gu", "COLORFGBG").Run()
	tmuxCmd("set-option", "-gu", "pane-colours").Run()
}

func globalWindowStyle(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("show-options", "-gv", "window-style").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options window-style: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// A session created after a theme is published, but before any push has run,
// still opens on that theme: Create applies the recorded value in its own
// command list rather than relying on a separate push having landed.
func TestCreateAppliesPublishedThemeWithoutAPush(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "pub" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg=#1e1e2e"; got != want {
		t.Fatalf("window-style = %q, want %q", got, want)
	}
}

// Concurrent pushes are latest-wins: whichever runs last writes the theme
// published last, not an older one it was spawned for. The lock serializes
// the writes and each push sends the current published value, so the server
// settles on the final publish however the goroutines interleave.
func TestPushPaneThemeIsLatestWins(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })

	// A session with no windows exits at once, so one holds the server up
	// for the global option to stick to.
	id := "race" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	backgrounds := []string{"#111111", "#222222", "#333333", "#444444", "#eff1f5"}
	var wg sync.WaitGroup
	for _, bg := range backgrounds {
		driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: bg, ColorFgBg: "15;0"})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := driver.PushPaneTheme(); err != nil {
				t.Errorf("PushPaneTheme: %v", err)
			}
		}()
	}
	wg.Wait()

	last := backgrounds[len(backgrounds)-1]
	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg="+last; got != want {
		t.Fatalf("window-style = %q, want the last published theme %q", got, want)
	}
}

// Create shares the push lock with PushPaneTheme, so a session opened while a
// newer theme is being pushed cannot reset the server to the theme Create
// loaded. The seam runs while Create holds the lock: it publishes a newer
// theme and starts a push, which blocks on the lock until Create's write
// lands, then writes the newer theme last. Without the shared lock the push
// runs during the seam and Create's stale write clobbers it.
func TestCreateSerializesWithPushPaneTheme(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })

	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	hold := "hold" + stamp
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(hold, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create hold session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(hold) })

	const newer = "#eff1f5"
	var pushed sync.WaitGroup
	afterCreateThemeLoad = func() {
		driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: newer, ColorFgBg: "0;15"})
		pushed.Add(1)
		go func() {
			defer pushed.Done()
			if err := driver.PushPaneTheme(); err != nil {
				t.Errorf("PushPaneTheme: %v", err)
			}
		}()
		// Give the push goroutine time to reach the lock, so the serialization
		// under test is what orders the two writes rather than this timing.
		time.Sleep(100 * time.Millisecond)
	}
	t.Cleanup(func() { afterCreateThemeLoad = nil })

	raced := "raced" + stamp
	if err := driver.Create(raced, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create raced session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(raced) })
	pushed.Wait()

	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg="+newer; got != want {
		t.Fatalf("window-style = %q, want the newer pushed theme %q", got, want)
	}
}

func hostPalette(colors map[int]string) []string {
	palette := make([]string, 16)
	for index, color := range colors {
		palette[index] = color
	}
	return palette
}

// The host's answered colors go into pane-colours after a clear, and a host
// that was never asked leaves the option alone.
func TestPaneThemeArgsDeclaresTheHostPalette(t *testing.T) {
	base := []string{
		"set-option", "-g", "window-style", "fg=#cdd6f4,bg=#1e1e2e", ";",
		"set-environment", "-g", "COLORFGBG", "15;0",
	}
	for _, tt := range []struct {
		name    string
		palette []string
		want    []string
	}{
		{"never asked", nil, base},
		{"no answers", hostPalette(nil), append(slices.Clone(base), ";", "set-option", "-q", "-gu", "pane-colours")},
		{
			"some answers",
			hostPalette(map[int]string{1: "#cc241d", 15: "#fbf1c7"}),
			append(slices.Clone(base),
				";", "set-option", "-q", "-gu", "pane-colours",
				";", "set-option", "-q", "-g", "pane-colours[1]", "#cc241d",
				";", "set-option", "-q", "-g", "pane-colours[15]", "#fbf1c7"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := paneThemeArgs(PaneTheme{Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0", Palette: tt.palette})
			if !slices.Equal(got, tt.want) {
				t.Fatalf("paneThemeArgs = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every supported tmux opens a session with the host palette in the theme,
// including those before 3.3 that have no pane-colours to set.
func TestCreateWithHostPaletteOnAnyTmux(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{
		Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0",
		Palette: hostPalette(map[int]string{1: "#cc241d"}),
	})

	id := "pal" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg=#1e1e2e"; got != want {
		t.Fatalf("window-style = %q, want %q", got, want)
	}
}

// A color the current host leaves unanswered drops the value an earlier
// push declared for it.
func TestPushPaneThemeClearsColorsTheHostNoLongerAnswers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("psmux has no pane-colours option")
	}
	driver := requireTmux(t)
	requireTmuxAtLeast(t, 3, 3)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{
		Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0",
		Palette: hostPalette(map[int]string{1: "#cc241d", 4: "#458588"}),
	})
	id := "palclr" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	driver.PublishPaneTheme(PaneTheme{
		Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0",
		Palette: hostPalette(map[int]string{4: "#458588"}),
	})
	if err := driver.PushPaneTheme(); err != nil {
		t.Fatalf("PushPaneTheme: %v", err)
	}
	out, err := tmuxCmd("show-options", "-g", "pane-colours").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options pane-colours: %v: %s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "pane-colours[4] #458588"; got != want {
		t.Fatalf("pane-colours = %q, want %q", got, want)
	}
}
