package session

import (
	"errors"
	"slices"
	"testing"
)

// The line is argv, not a script: a repo that sets EDITOR in an .envrc gets
// no shell to write into, so the operators stay literal text.
func TestCommandIsNeverHandedToAShell(t *testing.T) {
	cmd, ok := Command(`code; touch /tmp/pwned`, "/repo")
	if !ok {
		t.Fatal("Command refused a usable line")
	}
	want := []string{"code;", "touch", "/tmp/pwned", "/repo"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("argv = %v, want %v", cmd.Args, want)
	}
}

func TestSplitLineGroupsOnQuotes(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"code", []string{"code"}},
		{"code -n", []string{"code", "-n"}},
		{`open -a "Visual Studio Code"`, []string{"open", "-a", "Visual Studio Code"}},
		{`'/Applications/My App/bin/edit' -w`, []string{"/Applications/My App/bin/edit", "-w"}},
		{" 'code' ", []string{"code"}},
		{"   ", nil},
		{"", nil},
	} {
		if got := SplitLine(tc.line); !slices.Equal(got, tc.want) {
			t.Errorf("SplitLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestNameIsTheCommandWord(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"code", "code"},
		{"/usr/local/bin/code -n", "code"},
		{`open -a "Visual Studio Code"`, "open"},
		{"", ""},
	} {
		if got := Name(tc.line); got != tc.want {
			t.Errorf("Name(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func safeLookup(installed ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(installed, name) {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

// Each boundary of the resolution order, with both neighbours present and
// the higher one expected.
func TestResolvePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured string
		manager    string
		visual     string
		editor     string
		installed  []string
		want       string
	}{
		{"Settings over environment", "cfg-edit", "env-edit", "", "", []string{"code"}, "cfg-edit"},
		{"environment over PATH", "", "env-edit", "", "", []string{"code"}, "env-edit"},
		{"first GUI editor on PATH wins", "", "", "", "", []string{"zed", "cursor"}, "cursor"},
		{"PATH over $VISUAL", "", "", "vim", "", []string{"code"}, "code"},
		{"$VISUAL over $EDITOR", "", "", "vim", "nano", nil, "vim"},
		{"$EDITOR alone", "", "", "", " nvim ", nil, "nvim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.configured, tc.manager, tc.visual, tc.editor, GUIEditors(), safeLookup(tc.installed...))
			if got != tc.want {
				t.Fatalf("editor resolution = %q, want %q", got, tc.want)
			}
		})
	}
}

// The launch and return policy is decided on captured values, with no model.
func TestLaunchTakesTheScreenOnlyForATerminalEditor(t *testing.T) {
	if (Launch{Name: "code"}).TakesScreen() || !(Launch{Name: "vim"}).TakesScreen() {
		t.Fatal("a windowed editor took the screen, or a terminal one did not")
	}
	if !Detached("open") {
		t.Fatal("an OS opener hands the path off and exits, so it must count as detached")
	}
}
