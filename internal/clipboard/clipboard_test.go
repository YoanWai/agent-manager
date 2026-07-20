package clipboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restore() func() {
	origGOOS, origLook, origRun, origToFile, origWSL := goos, lookPath, runCmd, runCmdToFile, wslProbe
	return func() {
		goos, lookPath, runCmd, runCmdToFile, wslProbe = origGOOS, origLook, origRun, origToFile, origWSL
	}
}

func TestReadImageDarwinWritesAndReadsBytes(t *testing.T) {
	defer restore()()
	goos = "darwin"
	want := []byte("fake-png-bytes")
	var sawJXA bool
	runCmd = func(name string, args ...string) ([]byte, error) {
		if name != "osascript" {
			t.Fatalf("expected osascript, got %q", name)
		}
		// Prefer JXA AppKit path: osascript -l JavaScript -e <script> <path>
		if len(args) < 4 || args[0] != "-l" || args[1] != "JavaScript" {
			t.Fatalf("want JXA invocation, got %v", args)
		}
		sawJXA = true
		path := args[len(args)-1]
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return []byte("12"), nil
	}
	data, ext, err := ReadImage()
	if err != nil {
		t.Fatal(err)
	}
	if !sawJXA {
		t.Fatal("JXA osascript was not invoked")
	}
	if string(data) != string(want) || ext != "png" {
		t.Fatalf("got %q/%q", data, ext)
	}
}

func TestReadImageDarwinNoImage(t *testing.T) {
	defer restore()()
	goos = "darwin"
	runCmd = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("no image")
	}
	if _, _, err := ReadImage(); !errors.Is(err, ErrNoImage) {
		t.Fatalf("want ErrNoImage, got %v", err)
	}
}

func TestSaveImageDarwinWritesOnce(t *testing.T) {
	defer restore()()
	goos = "darwin"
	want := []byte("single-write-png")
	var writePath string
	runCmd = func(name string, args ...string) ([]byte, error) {
		writePath = args[len(args)-1]
		if err := os.WriteFile(writePath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return []byte("16"), nil
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if !strings.Contains(path, "agent-manager-pastes") {
		t.Fatalf("want path under pastes dir, got %q", path)
	}
	if writePath != path {
		t.Fatalf("JXA should write the final path directly; wrote %q, returned %q", writePath, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("got %q", data)
	}
}

func TestSaveImageDarwinNoImage(t *testing.T) {
	defer restore()()
	goos = "darwin"
	runCmd = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("no image")
	}
	if _, err := SaveImage(); !errors.Is(err, ErrNoImage) {
		t.Fatalf("want ErrNoImage, got %v", err)
	}
}

func TestSaveImageLinuxWlPasteStreamsToFile(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	want := []byte("wayland-png")
	lookPath = func(name string) (string, error) {
		if name == "wl-paste" {
			return "/usr/bin/wl-paste", nil
		}
		return "", errors.New("not found")
	}
	var wrote string
	runCmdToFile = func(outPath string, name string, args ...string) error {
		if name != "wl-paste" {
			t.Fatalf("expected wl-paste, got %q", name)
		}
		wrote = outPath
		return os.WriteFile(outPath, want, 0o644)
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if wrote != path {
		t.Fatalf("tool should stream to final path; wrote %q returned %q", wrote, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("got %q", data)
	}
}

func TestSaveImageLinuxXclipFallback(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	lookPath = func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", errors.New("not found")
	}
	var used string
	runCmdToFile = func(outPath string, name string, args ...string) error {
		used = name
		return os.WriteFile(outPath, []byte("x11-png"), 0o644)
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if used != "xclip" {
		t.Fatalf("expected xclip, used %q", used)
	}
}

func TestSaveImageLinuxNoTool(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	lookPath = func(name string) (string, error) { return "", errors.New("not found") }
	_, err := SaveImage()
	if err == nil || errors.Is(err, ErrNoImage) {
		t.Fatalf("want a missing-tool error, got %v", err)
	}
	if !strings.Contains(err.Error(), "wl-clipboard") {
		t.Fatalf("error should name the tools to install, got %v", err)
	}
}

func TestSaveImageWSLUsesWindowsClipboard(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return true }
	// No native Linux clipboard tools: WSL should go to PowerShell.
	lookPath = func(name string) (string, error) {
		switch name {
		case "powershell.exe":
			return "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", nil
		case "wslpath":
			return "/usr/bin/wslpath", nil
		default:
			return "", errors.New("not found")
		}
	}
	var sawPS, sawWslpath bool
	var psScript string
	runCmd = func(name string, args ...string) ([]byte, error) {
		base := filepath.Base(name)
		if base == "wslpath" || name == "wslpath" {
			sawWslpath = true
			return []byte(`C:\Users\me\paste.png`), nil
		}
		if strings.Contains(base, "powershell") || strings.Contains(name, "powershell") {
			sawPS = true
			// last arg is -Command script
			psScript = args[len(args)-1]
			// The path powershell saves is a Windows path; the WSL file is
			// the paste file already created. Simulate a successful save by
			// writing through the wslpath reverse: tests write the WSL file
			// from the script's target. We re-find the empty paste file.
			// Easier: write to every paste-*.png created under pastesDir.
			dir := pastesDir()
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "paste-") {
					_ = os.WriteFile(filepath.Join(dir, entry.Name()), []byte("win-png"), 0o644)
				}
			}
			return nil, nil
		}
		t.Fatalf("unexpected command %q %v", name, args)
		return nil, nil
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if !sawPS || !sawWslpath {
		t.Fatalf("want powershell + wslpath, sawPS=%v sawWslpath=%v", sawPS, sawWslpath)
	}
	if !strings.Contains(psScript, "Clipboard]::GetImage") {
		t.Fatalf("script should read Windows clipboard image, got %q", psScript)
	}
	if !strings.Contains(psScript, `C:\Users\me\paste.png`) {
		t.Fatalf("script should save to wslpath result, got %q", psScript)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "win-png" {
		t.Fatalf("got %q", data)
	}
}

func TestSaveImageWSLFallsBackWhenNativeMisses(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return true }
	lookPath = func(name string) (string, error) {
		switch name {
		case "wl-paste":
			return "/usr/bin/wl-paste", nil
		case "powershell.exe":
			return "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", nil
		case "wslpath":
			return "/usr/bin/wslpath", nil
		default:
			return "", errors.New("not found")
		}
	}
	// wl-paste sees no image; Windows clipboard has one.
	runCmdToFile = func(outPath string, name string, args ...string) error {
		return errors.New("no image on wayland")
	}
	runCmd = func(name string, args ...string) ([]byte, error) {
		base := filepath.Base(name)
		if base == "wslpath" || name == "wslpath" {
			return []byte(`C:\tmp\out.png`), nil
		}
		if strings.Contains(base, "powershell") || strings.Contains(name, "powershell") {
			dir := pastesDir()
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "paste-") {
					_ = os.WriteFile(filepath.Join(dir, entry.Name()), []byte("from-windows"), 0o644)
				}
			}
			return nil, nil
		}
		return nil, errors.New("unexpected")
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, _ := os.ReadFile(path)
	if string(data) != "from-windows" {
		t.Fatalf("want Windows clipboard fallback, got %q", data)
	}
}

func TestReadImageLinuxWayland(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	want := []byte("wayland-png")
	lookPath = func(name string) (string, error) {
		if name == "wl-paste" {
			return "/usr/bin/wl-paste", nil
		}
		return "", errors.New("not found")
	}
	runCmdToFile = func(outPath string, name string, args ...string) error {
		return os.WriteFile(outPath, want, 0o644)
	}
	data, ext, err := ReadImage()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) || ext != "png" {
		t.Fatalf("got %q/%q", data, ext)
	}
}

func TestReadImageLinuxXclipFallback(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	lookPath = func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", errors.New("not found")
	}
	var used string
	runCmdToFile = func(outPath string, name string, args ...string) error {
		used = name
		return os.WriteFile(outPath, []byte("x11-png"), 0o644)
	}
	if _, _, err := ReadImage(); err != nil {
		t.Fatal(err)
	}
	if used != "xclip" {
		t.Fatalf("expected xclip, used %q", used)
	}
}

func TestReadImageLinuxNoTool(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	lookPath = func(name string) (string, error) { return "", errors.New("not found") }
	_, _, err := ReadImage()
	if err == nil || errors.Is(err, ErrNoImage) {
		t.Fatalf("want a missing-tool error, got %v", err)
	}
	if !strings.Contains(err.Error(), "wl-clipboard") {
		t.Fatalf("error should name the tools to install, got %v", err)
	}
}

func TestReadImageUnsupportedOS(t *testing.T) {
	defer restore()()
	goos = "plan9"
	if _, _, err := ReadImage(); err == nil {
		t.Fatal("expected unsupported-OS error")
	}
}

func TestSaveToTempWritesBytes(t *testing.T) {
	path, err := SaveToTemp([]byte("payload"), "png")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if filepath.Ext(path) != ".png" {
		t.Fatalf("want .png, got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("got %q", data)
	}
}

func TestDetectWSLEnv(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !detectWSL() {
		t.Fatal("WSL_DISTRO_NAME should mark WSL")
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "/run/WSL/1_interop")
	if !detectWSL() {
		t.Fatal("WSL_INTEROP should mark WSL")
	}
}
