// Package clipboard reads image data off the OS clipboard so the quick
// prompt can attach a pasted screenshot to an agent session.
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"os/exec"
)

// ErrNoImage means the clipboard held no image when it was read.
var ErrNoImage = errors.New("no image in clipboard")

// Overridable seams so tests can drive the platform branches without a
// real clipboard.
var (
	goos     = runtime.GOOS
	lookPath = exec.LookPath
	runCmd   = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}
)

// ReadImage returns the clipboard image as raw bytes plus its file
// extension. It errors when the clipboard holds no image, the platform
// tooling is missing, or the OS is unsupported.
func ReadImage() ([]byte, string, error) {
	switch goos {
	case "darwin":
		return readDarwin()
	case "linux":
		return readLinux()
	default:
		return nil, "", fmt.Errorf("clipboard image paste is not supported on %s", goos)
	}
}

// readDarwin coerces the clipboard to PNG through osascript, which writes
// the bytes to a temp file we read back. A clipboard without image data
// makes the coercion fail, which we report as ErrNoImage.
func readDarwin() ([]byte, string, error) {
	dst, err := os.CreateTemp("", "clip-*.png")
	if err != nil {
		return nil, "", err
	}
	path := dst.Name()
	dst.Close()
	defer os.Remove(path)

	script := `on run argv
	set outFile to (item 1 of argv)
	set pngData to (the clipboard as «class PNGf»)
	set fh to open for access (POSIX file outFile) with write permission
	set eof fh to 0
	write pngData to fh
	close access fh
end run`
	if _, err := runCmd("osascript", "-e", script, path); err != nil {
		return nil, "", ErrNoImage
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", ErrNoImage
	}
	return data, "png", nil
}

// readLinux pulls PNG bytes from whichever clipboard tool is installed,
// preferring the Wayland one.
func readLinux() ([]byte, string, error) {
	if _, err := lookPath("wl-paste"); err == nil {
		data, err := runCmd("wl-paste", "--type", "image/png")
		if err != nil || len(data) == 0 {
			return nil, "", ErrNoImage
		}
		return data, "png", nil
	}
	if _, err := lookPath("xclip"); err == nil {
		data, err := runCmd("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		if err != nil || len(data) == 0 {
			return nil, "", ErrNoImage
		}
		return data, "png", nil
	}
	return nil, "", errors.New("install wl-clipboard or xclip to paste images")
}

// SaveToTemp writes image bytes to a uniquely named file under a shared
// pastes directory and returns its absolute path.
func SaveToTemp(data []byte, ext string) (string, error) {
	dir := filepath.Join(os.TempDir(), "agent-manager-pastes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "paste-*."+ext)
	if err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return file.Name(), nil
}
