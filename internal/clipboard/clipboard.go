// Package clipboard reads image data off the OS clipboard so the quick
// prompt can attach a pasted screenshot to an agent session.
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	// runCmdToFile runs a command with stdout directed at outPath.
	runCmdToFile = func(outPath string, name string, args ...string) error {
		file, err := os.OpenFile(outPath, os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer file.Close()
		cmd := exec.Command(name, args...)
		cmd.Stdout = file
		return cmd.Run()
	}
	// wslProbe reports whether we are running under WSL (Windows clipboard
	// bridge needed when Linux tools cannot see the host clipboard).
	wslProbe = detectWSL
)

// pastesDir is the shared temp directory for clipboard image files.
func pastesDir() string {
	return filepath.Join(os.TempDir(), "agent-manager-pastes")
}

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

// SaveImage writes a clipboard image into the pastes directory and returns
// its absolute path. Each platform writes the PNG once (no intermediate
// buffer file when tools can stream to a path).
func SaveImage() (string, error) {
	switch goos {
	case "darwin":
		return saveDarwinImage()
	case "linux":
		return saveLinuxImage()
	default:
		return "", fmt.Errorf("clipboard image paste is not supported on %s", goos)
	}
}

// jxaReadPNG writes the clipboard image as PNG to the path in argv[0].
// Prefers NSPasteboardTypePNG (fast, no conversion). Falls back to TIFF
// only when needed. AppleScript "clipboard as PNGf" is several times
// slower because it re-encodes through the Scripting Bridge.
const jxaReadPNG = `
ObjC.import("AppKit");
ObjC.import("Foundation");
function run(argv) {
  var out = argv[0];
  var pb = $.NSPasteboard.generalPasteboard;
  var data = pb.dataForType($.NSPasteboardTypePNG);
  if (!data || data.length === 0) {
    var tiff = pb.dataForType($.NSPasteboardTypeTIFF);
    if (tiff && tiff.length > 0) {
      var img = $.NSBitmapImageRep.imageRepWithData(tiff);
      if (img) {
        data = img.representationUsingTypeProperties($.NSBitmapImageFileTypePNG, $());
      }
    }
  }
  if (!data || data.length === 0) {
    throw new Error("no image");
  }
  if (!data.writeToFileAtomically($(out), true)) {
    throw new Error("write failed");
  }
  return String(data.length);
}
`

// writeDarwinPNG dumps the clipboard image as PNG to path using JXA
// AppKit. Any failure (empty clipboard, no image type) is ErrNoImage.
func writeDarwinPNG(path string) error {
	if _, err := runCmd("osascript", "-l", "JavaScript", "-e", jxaReadPNG, path); err != nil {
		return ErrNoImage
	}
	return nil
}

// saveDarwinImage writes the clipboard PNG straight into the pastes dir.
func saveDarwinImage() (string, error) {
	path, err := newPasteFile("png")
	if err != nil {
		return "", err
	}
	if err := writeDarwinPNG(path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if !fileNonEmpty(path) {
		_ = os.Remove(path)
		return "", ErrNoImage
	}
	return path, nil
}

// readDarwin pulls PNG bytes for callers that still want the raw payload.
// It writes once into a temp file under pastesDir and reads it back.
func readDarwin() ([]byte, string, error) {
	path, err := saveDarwinImage()
	if err != nil {
		return nil, "", err
	}
	// Caller owns only the bytes; drop the file so we do not leak pastes
	// from ReadImage-only use. SaveImage is the path-returning API.
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", ErrNoImage
	}
	return data, "png", nil
}

// saveLinuxImage writes a clipboard PNG in one shot. Order:
//  1. wl-paste (Wayland)
//  2. xclip (X11 / WSLg)
//  3. On WSL: Windows clipboard via powershell.exe (host paste)
//
// Native tools are tried first so a real Linux desktop stays local and
// fast; WSL falls through to Windows when those cannot see the image.
func saveLinuxImage() (string, error) {
	var triedTool bool
	if _, err := lookPath("wl-paste"); err == nil {
		triedTool = true
		path, err := writePasteFromCmd("png", "wl-paste", "--type", "image/png")
		if err == nil {
			return path, nil
		}
	}
	if _, err := lookPath("xclip"); err == nil {
		triedTool = true
		path, err := writePasteFromCmd("png", "xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		if err == nil {
			return path, nil
		}
	}
	if wslProbe() {
		path, err := saveWSLWindowsImage()
		if err == nil {
			return path, nil
		}
		// WSL with no image on either clipboard is still "no image".
		if errors.Is(err, ErrNoImage) {
			return "", ErrNoImage
		}
		// Missing powershell is a real config problem on WSL when native
		// tools also cannot deliver an image.
		if !triedTool {
			return "", err
		}
		return "", ErrNoImage
	}
	if !triedTool {
		return "", errors.New("install wl-clipboard or xclip to paste images")
	}
	return "", ErrNoImage
}

// readLinux pulls PNG bytes for the ReadImage API (in-memory).
func readLinux() ([]byte, string, error) {
	path, err := saveLinuxImage()
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", ErrNoImage
	}
	return data, "png", nil
}

// writePasteFromCmd creates a paste file and streams the command's stdout
// into it. Empty output or a failed command is ErrNoImage.
func writePasteFromCmd(ext string, name string, args ...string) (string, error) {
	path, err := newPasteFile(ext)
	if err != nil {
		return "", err
	}
	if err := runCmdToFile(path, name, args...); err != nil {
		_ = os.Remove(path)
		return "", ErrNoImage
	}
	if !fileNonEmpty(path) {
		_ = os.Remove(path)
		return "", ErrNoImage
	}
	return path, nil
}

// saveWSLWindowsImage reads an image from the Windows host clipboard via
// PowerShell and saves it to a WSL pastes path (converted with wslpath).
func saveWSLWindowsImage() (string, error) {
	ps, err := lookPath("powershell.exe")
	if err != nil {
		ps, err = lookPath("pwsh.exe")
		if err != nil {
			return "", errors.New("WSL image paste needs powershell.exe (Windows clipboard) or wl-clipboard/xclip")
		}
	}
	path, err := newPasteFile("png")
	if err != nil {
		return "", err
	}
	winOut, err := runCmd("wslpath", "-w", path)
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("wslpath: %w", err)
	}
	winPath := strings.TrimSpace(string(winOut))
	// Single-quoted PowerShell string: double any embedded quotes.
	winPath = strings.ReplaceAll(winPath, "'", "''")
	script := "Add-Type -AssemblyName System.Windows.Forms,System.Drawing; " +
		"$img = [System.Windows.Forms.Clipboard]::GetImage(); " +
		"if ($null -eq $img) { exit 1 }; " +
		"$img.Save('" + winPath + "', [System.Drawing.Imaging.ImageFormat]::Png)"
	if _, err := runCmd(ps, "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		_ = os.Remove(path)
		return "", ErrNoImage
	}
	if !fileNonEmpty(path) {
		_ = os.Remove(path)
		return "", ErrNoImage
	}
	return path, nil
}

// detectWSL reports WSL via env or the kernel release string.
func detectWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	release := strings.ToLower(string(data))
	return strings.Contains(release, "microsoft") || strings.Contains(release, "wsl")
}

// newPasteFile creates an empty paste-* file under pastesDir and returns
// its absolute path (caller fills it or removes it).
func newPasteFile(ext string) (string, error) {
	if err := os.MkdirAll(pastesDir(), 0o755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(pastesDir(), "paste-*."+ext)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// SaveToTemp writes image bytes to a uniquely named file under a shared
// pastes directory and returns its absolute path.
func SaveToTemp(data []byte, ext string) (string, error) {
	if err := os.MkdirAll(pastesDir(), 0o755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(pastesDir(), "paste-*."+ext)
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
