package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func bindingOf(t *testing.T, specs ...string) keybind.Binding {
	t.Helper()
	keys := make([]keybind.Key, 0, len(specs))
	for _, spec := range specs {
		key, err := keybind.Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}

func sessionOf(t *testing.T, detach, review, editor []string) keybind.Table {
	t.Helper()
	return keybind.DefaultSession().
		With(keybind.Detach, bindingOf(t, detach...)).
		With(keybind.Review, bindingOf(t, review...)).
		With(keybind.Editor, bindingOf(t, editor...))
}

// The welcome card's session-key row is laid out on the card's columns and
// follows the table, so a first run under a remapped config reads right.
func TestWelcomeSessionKeysLineFollowsTheKeyTable(t *testing.T) {
	src := Sources{SessionKeys: keybind.DefaultSession()}
	if got := src.welcomeSessionKeysLine(); got != "ctrl+q back to the manager   ctrl+r review its diff" {
		t.Errorf("default line = %q", got)
	}
	src.SessionKeys = sessionOf(t, []string{"f9"}, nil, []string{"f3"})
	if got := src.welcomeSessionKeysLine(); got != "f9     back to the manager" {
		t.Errorf("review off line = %q", got)
	}
}

func TestWelcomeSessionKeysLineOmitsDisabledActions(t *testing.T) {
	src := Sources{SessionKeys: sessionOf(t, nil, []string{"ctrl+r"}, nil)}
	got := src.welcomeSessionKeysLine()
	if strings.Contains(got, "back to the manager") || !strings.Contains(got, "ctrl+r review its diff") {
		t.Fatalf("detach off line = %q", got)
	}

	src.SessionKeys = sessionOf(t, nil, nil, nil)
	if got := src.welcomeSessionKeysLine(); got != "" {
		t.Fatalf("all session actions off line = %q", got)
	}
}

// The welcome's list keys follow the list table the same way: a moved key
// is named where it moved to, and an action turned off is left out.
func TestWelcomeBodyFollowsTheListTable(t *testing.T) {
	src := Sources{SessionKeys: keybind.DefaultSession(), ListKeys: keybind.DefaultList()}
	body := strings.Join(src.welcomeBody(), "\n")
	for _, want := range []string{"n      new session           space  quick prompt mode", "↵      focus it              A      attach it full screen", "x / v  kill / revive         s      settings", "space on a group row", "Press ? for every key: the map scrolls, and / searches it.", "Settings (s)", "Marks missing? Settings (s), symbols, Compatible."} {
		if !strings.Contains(body, want) {
			t.Errorf("default welcome is missing %q:\n%s", want, body)
		}
	}
	src.ListKeys = src.ListKeys.
		With(keybind.NewSession, bindingOf(t, "N")).
		With(keybind.Prompt, bindingOf(t)).
		With(keybind.Search, bindingOf(t)).
		With(keybind.Settings, bindingOf(t, "S"))
	body = strings.Join(src.welcomeBody(), "\n")
	for _, want := range []string{"N      new session", "Press ? for every key: the map scrolls.", "Settings (S)", "x / v  kill / revive         S      settings", "Marks missing? Settings (S), symbols, Compatible."} {
		if !strings.Contains(body, want) {
			t.Errorf("remapped welcome is missing %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"prompt it", "on a group row", "searches it", "n      new session"} {
		if strings.Contains(body, gone) {
			t.Errorf("remapped welcome should drop %q:\n%s", gone, body)
		}
	}
}
