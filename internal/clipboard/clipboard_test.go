package clipboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restore() func() {
	origGOOS, origLook, origRun, origToFile, origWSL, origNative := goos, lookPath, runCmd, runCmdToFile, wslProbe, readNativeImage
	return func() {
		goos, lookPath, runCmd, runCmdToFile, wslProbe, readNativeImage = origGOOS, origLook, origRun, origToFile, origWSL, origNative
	}
}

func TestSaveImageDarwinPrefersNative(t *testing.T) {
	defer restore()()
	goos = "darwin"
	want := []byte("native-png-bytes")
	var jxaCalled bool
	readNativeImage = func() ([]byte, error) {
		return want, nil
	}
	runCmd = func(name string, args ...string) ([]byte, error) {
		jxaCalled = true
		return nil, errors.New("jxa should not run")
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if jxaCalled {
		t.Fatal("native path should skip JXA osascript")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("got %q", data)
	}
}

func TestSaveImageDarwinNativeNoImageSkipsJXA(t *testing.T) {
	defer restore()()
	goos = "darwin"
	var jxaCalled bool
	readNativeImage = func() ([]byte, error) {
		return nil, ErrNoImage
	}
	runCmd = func(name string, args ...string) ([]byte, error) {
		jxaCalled = true
		return nil, nil
	}
	if _, err := SaveImage(); !errors.Is(err, ErrNoImage) {
		t.Fatalf("want ErrNoImage, got %v", err)
	}
	if jxaCalled {
		t.Fatal("empty native clipboard must not fall through to JXA")
	}
}

func TestSaveImageDarwinFallsBackToJXAWhenNativeBroken(t *testing.T) {
	defer restore()()
	goos = "darwin"
	want := []byte("jxa-png")
	readNativeImage = func() ([]byte, error) {
		return nil, errors.New("purego init failed")
	}
	runCmd = func(name string, args ...string) ([]byte, error) {
		if name != "osascript" {
			t.Fatalf("expected osascript, got %q", name)
		}
		path := args[len(args)-1]
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return []byte("7"), nil
	}
	path, err := SaveImage()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, _ := os.ReadFile(path)
	if string(data) != string(want) {
		t.Fatalf("got %q", data)
	}
}

func TestReadImageDarwinUsesNative(t *testing.T) {
	defer restore()()
	goos = "darwin"
	want := []byte("fake-png-bytes")
	readNativeImage = func() ([]byte, error) {
		return want, nil
	}
	data, ext, err := ReadImage()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) || ext != "png" {
		t.Fatalf("got %q/%q", data, ext)
	}
}

func TestReadImageDarwinNoImage(t *testing.T) {
	defer restore()()
	goos = "darwin"
	readNativeImage = func() ([]byte, error) {
		return nil, ErrNoImage
	}
	if _, _, err := ReadImage(); !errors.Is(err, ErrNoImage) {
		t.Fatalf("want ErrNoImage, got %v", err)
	}
}

func TestSaveImageLinuxWlPasteStreamsToFile(t *testing.T) {
	defer restore()()
	goos = "linux"
	wslProbe = func() bool { return false }
	readNativeImage = nil
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
	readNativeImage = nil
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
	readNativeImage = nil
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
	t.Setenv("TMPDIR", t.TempDir())
	goos = "linux"
	wslProbe = func() bool { return true }
	readNativeImage = nil
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
	var pastePath string
	runCmd = func(name string, args ...string) ([]byte, error) {
		base := filepath.Base(name)
		if base == "wslpath" || name == "wslpath" {
			sawWslpath = true
			pastePath = args[len(args)-1]
			return []byte(`C:\Users\me\paste.png`), nil
		}
		if strings.Contains(base, "powershell") || strings.Contains(name, "powershell") {
			sawPS = true
			psScript = args[len(args)-1]
			if pastePath == "" {
				t.Fatal("wslpath should run before powershell")
			}
			if err := os.WriteFile(pastePath, []byte("win-png"), 0o644); err != nil {
				t.Fatal(err)
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
	t.Setenv("TMPDIR", t.TempDir())
	goos = "linux"
	wslProbe = func() bool { return true }
	readNativeImage = nil
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
	runCmdToFile = func(outPath string, name string, args ...string) error {
		return errors.New("no image on wayland")
	}
	var pastePath string
	runCmd = func(name string, args ...string) ([]byte, error) {
		base := filepath.Base(name)
		if base == "wslpath" || name == "wslpath" {
			pastePath = args[len(args)-1]
			return []byte(`C:\tmp\out.png`), nil
		}
		if strings.Contains(base, "powershell") || strings.Contains(name, "powershell") {
			if pastePath == "" {
				t.Fatal("wslpath should run before powershell")
			}
			if err := os.WriteFile(pastePath, []byte("from-windows"), 0o644); err != nil {
				t.Fatal(err)
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
	readNativeImage = nil
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
	readNativeImage = nil
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
	readNativeImage = nil
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
