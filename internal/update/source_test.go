package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func stubSourceSeams(t *testing.T, helpers map[string]string, owned bool) {
	t.Helper()
	origLook := lookPath
	origOwns := pacmanOwns
	t.Cleanup(func() {
		lookPath = origLook
		pacmanOwns = origOwns
	})
	lookPath = func(name string) (string, error) {
		if path, ok := helpers[name]; ok {
			return path, nil
		}
		return "", errors.New("not found")
	}
	pacmanOwns = func(string) bool { return owned }
}

func TestDetectManager(t *testing.T) {
	cases := []struct {
		label       string
		source      string
		path        string
		helpers     map[string]string
		pacmanOwned bool
		wantName    string
		wantCommand string
		wantAdvice  bool
	}{
		{
			label:       "caskroom upgrades the tap cask",
			path:        "/opt/homebrew/Caskroom/agent-manager/0.19.0/agent-manager",
			wantName:    "Homebrew",
			wantCommand: "brew upgrade --cask yoanwai/tap/agent-manager",
		},
		{
			label:       "cellar upgrades the formula",
			path:        "/opt/homebrew/Cellar/agent-manager/0.19.0/bin/agent-manager",
			wantName:    "Homebrew",
			wantCommand: "brew upgrade agent-manager",
		},
		{
			label:       "linuxbrew opt tree",
			path:        "/home/linuxbrew/.linuxbrew/opt/agent-manager/bin/agent-manager",
			wantName:    "Homebrew",
			wantCommand: "brew upgrade agent-manager",
		},
		{
			label:       "homebrew ldflag without a brew path",
			source:      "Homebrew",
			path:        "/usr/local/bin/agent-manager",
			wantName:    "Homebrew",
			wantCommand: "brew upgrade agent-manager",
		},
		{
			label:       "mise install dir",
			path:        "/home/user/.local/share/mise/installs/ubi-yoan-wai-agent-manager/0.19.0/agent-manager",
			wantName:    "mise",
			wantCommand: "mise upgrade --bump ubi:YoanWai/agent-manager",
		},
		{
			label:       "mise custom data dir",
			path:        "/data/tools/installs/ubi-yoan-wai-agent-manager/0.20.0/agent-manager",
			wantName:    "mise",
			wantCommand: "mise upgrade --bump ubi:YoanWai/agent-manager",
		},
		{
			label:       "pacman owned with yay",
			path:        "/usr/bin/agent-manager",
			helpers:     map[string]string{"yay": "/usr/bin/yay"},
			pacmanOwned: true,
			wantName:    "yay",
			wantCommand: "yay -S agent-manager-bin",
		},
		{
			label:       "pacman owned with paru only",
			path:        "/usr/bin/agent-manager",
			helpers:     map[string]string{"paru": "/usr/bin/paru"},
			pacmanOwned: true,
			wantName:    "paru",
			wantCommand: "paru -S agent-manager-bin",
		},
		{
			label:       "pacman owned without a helper",
			path:        "/usr/bin/agent-manager",
			pacmanOwned: true,
			wantName:    "pacman",
			wantAdvice:  true,
		},
		{
			label: "install script dir is direct",
			path:  "/home/user/.local/bin/agent-manager",
		},
		{
			label: "gopath is direct",
			path:  "/home/user/go/bin/agent-manager",
		},
		{
			label: "manual opt prefix is direct",
			path:  "/opt/agent-manager/bin/agent-manager",
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			stubSourceSeams(t, tc.helpers, tc.pacmanOwned)
			origSource := buildSource
			buildSource = tc.source
			t.Cleanup(func() { buildSource = origSource })

			manager := DetectManager(tc.path)
			if manager.Name != tc.wantName {
				t.Fatalf("Name = %q, want %q", manager.Name, tc.wantName)
			}
			if manager.String() != tc.wantCommand {
				t.Fatalf("Command = %q, want %q", manager.String(), tc.wantCommand)
			}
			if (manager.Advice != "") != tc.wantAdvice {
				t.Fatalf("Advice = %q, wantAdvice %v", manager.Advice, tc.wantAdvice)
			}
			if manager.Delegated() != (tc.wantCommand != "") {
				t.Fatalf("Delegated() = %v with command %q", manager.Delegated(), tc.wantCommand)
			}
		})
	}
}

func TestDetectManagerFollowsSymlink(t *testing.T) {
	stubSourceSeams(t, nil, false)
	dir := t.TempDir()
	caskroom := filepath.Join(dir, "Caskroom", "agent-manager", "0.19.0")
	if err := os.MkdirAll(caskroom, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(caskroom, "agent-manager")
	if err := os.WriteFile(real, []byte("cask build"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "bin", "agent-manager")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	manager := DetectManager(link)
	if manager.String() != "brew upgrade --cask yoanwai/tap/agent-manager" {
		t.Fatalf("symlink into Caskroom got %q", manager.String())
	}
}

func TestPacmanOwnsOffLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("exercises the non-linux short circuit")
	}
	if pacmanOwnsPath("/usr/bin/agent-manager") {
		t.Fatal("pacman ownership must be linux-only")
	}
}

func TestRestartTargetPrefersPathEntry(t *testing.T) {
	stubSourceSeams(t, map[string]string{"agent-manager": "/opt/homebrew/bin/agent-manager"}, false)
	got := RestartTarget("/opt/homebrew/Caskroom/agent-manager/0.19.0/agent-manager")
	if got != "/opt/homebrew/bin/agent-manager" {
		t.Fatalf("RestartTarget = %q", got)
	}
}

func TestRestartTargetFallsBackToExecPath(t *testing.T) {
	stubSourceSeams(t, nil, false)
	got := RestartTarget("/weird/place/agent-manager")
	if got != "/weird/place/agent-manager" {
		t.Fatalf("RestartTarget = %q", got)
	}
}
