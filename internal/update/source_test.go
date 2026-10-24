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
			label:      "nix store is updated through nix",
			path:       "/nix/store/0c1s7yb9hv2y1wkgvyd4jp3spdcyv0b0-agent-manager-0.39.0/bin/agent-manager",
			wantName:   "Nix",
			wantAdvice: true,
		},
		{
			label: "another ubi tool is direct",
			path:  "/home/user/.local/share/mise/installs/ubi-other-user-agent-manager/1.0.0/agent-manager",
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

func TestDetectScoopManager(t *testing.T) {
	stubSourceSeams(t, nil, false)
	cases := []struct {
		label string
		path  string
		root  string
		key   string
		want  bool
	}{
		{label: "user version", path: `C:\Users\Jane\scoop\apps\agent-manager\0.43.0\agent-manager.exe`, want: true},
		{label: "current junction", path: `C:\Users\Jane\scoop\apps\agent-manager\current\agent-manager.exe`, want: true},
		{label: "global", path: `C:\ProgramData\scoop\apps\agent-manager\current\agent-manager.exe`, want: true},
		{label: "case and spaces", path: `C:\Users\Jane Doe\SCOOP\APPS\Agent-Manager\current\agent-manager.exe`, want: true},
		{label: "forward slashes", path: "C:/scoop/apps/agent-manager/current/agent-manager.exe", want: true},
		{label: "custom root", key: "SCOOP", root: `D:\My Tools\`, path: `d:\my tools\apps\agent-manager\0.43.0\agent-manager.exe`, want: true},
		{label: "custom global root", key: "SCOOP_GLOBAL", root: "D:/GlobalTools", path: `D:\GlobalTools\apps\agent-manager\current\agent-manager.exe`, want: true},
		{label: "other package", path: `C:\Users\Jane\scoop\apps\another-app\current\agent-manager.exe`},
		{label: "package prefix", path: `C:\Users\Jane\scoop\apps\agent-manager-copy\current\agent-manager.exe`},
		{label: "root prefix", key: "SCOOP", root: `D:\Tools`, path: `D:\Tools-copy\apps\agent-manager\current\agent-manager.exe`},
		{label: "directory suffix", path: `C:\Users\Jane\not-scoop\apps\agent-manager\current\agent-manager.exe`},
		{label: "manual install", path: `C:\Users\Jane\AppData\Local\Programs\agent-manager\agent-manager.exe`},
		{label: "manual install beside a custom root", key: "SCOOP", root: `D:\Tools`, path: `C:\Users\Jane\AppData\Local\Programs\agent-manager\agent-manager.exe`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("SCOOP", "")
			t.Setenv("SCOOP_GLOBAL", "")
			if tc.key != "" {
				t.Setenv(tc.key, tc.root)
			}
			manager := DetectManager(tc.path)
			if !tc.want {
				if manager.Name != "" || manager.Advice != "" || manager.Delegated() {
					t.Fatalf("direct install detected as %+v", manager)
				}
				return
			}
			if manager.Name != "Scoop" || manager.Delegated() {
				t.Fatalf("Scoop install must block the in-place update, got %+v", manager)
			}
			if manager.Advice != "installed with Scoop. Close agent-manager, then run: scoop update agent-manager. For a global install, add --global in an elevated PowerShell." {
				t.Fatalf("Scoop advice = %q", manager.Advice)
			}
		})
	}
}

func TestDetectScoopManagerFollowsSymlink(t *testing.T) {
	stubSourceSeams(t, nil, false)
	dir := t.TempDir()
	version := filepath.Join(dir, "scoop", "apps", "agent-manager", "0.43.0")
	if err := os.MkdirAll(version, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "scoop", "apps", "agent-manager", "current")
	if err := os.Symlink(version, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(version, "agent-manager.exe"), []byte("Scoop build"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := DetectManager(filepath.Join(link, "agent-manager.exe"))
	if manager.Name != "Scoop" || manager.Advice == "" || manager.Delegated() {
		t.Fatalf("symlink into Scoop got %+v", manager)
	}
}

func TestDetectScoopCustomRootFollowsSymlink(t *testing.T) {
	stubSourceSeams(t, nil, false)
	base := t.TempDir()
	real := filepath.Join(base, "My Tools")
	rootLink := filepath.Join(base, "scoop-root")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, rootLink); err != nil {
		t.Fatal(err)
	}
	// SCOOP points at the link. The binary resolves through that link and
	// through current, the way a Windows short path or junction does.
	t.Setenv("SCOOP", rootLink)
	t.Setenv("SCOOP_GLOBAL", "")
	version := filepath.Join(real, "apps", "agent-manager", "0.43.0")
	if err := os.MkdirAll(version, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(real, "apps", "agent-manager", "current")
	if err := os.Symlink(version, current); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(version, "agent-manager.exe"), []byte("Scoop build"), 0o755); err != nil {
		t.Fatal(err)
	}
	launch := filepath.Join(rootLink, "apps", "agent-manager", "current", "agent-manager.exe")
	manager := DetectManager(launch)
	if manager.Name != "Scoop" || manager.Advice == "" || manager.Delegated() {
		t.Fatalf("custom Scoop root symlink got %+v", manager)
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
