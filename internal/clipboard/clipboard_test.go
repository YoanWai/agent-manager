package clipboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restore() func() {
	origGOOS, origLook, origRun := goos, lookPath, runCmd
	return func() {
		goos, lookPath, runCmd = origGOOS, origLook, origRun
	}
}

func TestReadImageDarwinWritesAndReadsBytes(t *testing.T) {
	defer restore()()
	goos = "darwin"
	want := []byte("fake-png-bytes")
	var sawScript bool
	runCmd = func(name string, args ...string) ([]byte, error) {
		if name != "osascript" {
			t.Fatalf("expected osascript, got %q", name)
		}
		sawScript = true
		path := args[len(args)-1]
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}
	data, ext, err := ReadImage()
	if err != nil {
		t.Fatal(err)
	}
	if !sawScript {
		t.Fatal("osascript was not invoked")
	}
	if string(data) != string(want) || ext != "png" {
		t.Fatalf("got %q/%q", data, ext)
	}
}

func TestReadImageDarwinNoImage(t *testing.T) {
	defer restore()()
	goos = "darwin"
	runCmd = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("coercion failed")
	}
	if _, _, err := ReadImage(); !errors.Is(err, ErrNoImage) {
		t.Fatalf("want ErrNoImage, got %v", err)
	}
}

func TestReadImageLinuxWayland(t *testing.T) {
	defer restore()()
	goos = "linux"
	want := []byte("wayland-png")
	lookPath = func(name string) (string, error) {
		if name == "wl-paste" {
			return "/usr/bin/wl-paste", nil
		}
		return "", errors.New("not found")
	}
	runCmd = func(name string, args ...string) ([]byte, error) {
		if name != "wl-paste" {
			t.Fatalf("expected wl-paste, got %q", name)
		}
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

func TestReadImageLinuxXclipFallback(t *testing.T) {
	defer restore()()
	goos = "linux"
	lookPath = func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", errors.New("not found")
	}
	var used string
	runCmd = func(name string, args ...string) ([]byte, error) {
		used = name
		return []byte("x11-png"), nil
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
