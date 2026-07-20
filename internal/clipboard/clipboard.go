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
// its absolute path. On Darwin this is a single write (no intermediate
// buffer file). Linux still reads bytes then writes them once.
func SaveImage() (string, error) {
	switch goos {
	case "darwin":
		return saveDarwinImage()
	case "linux":
		data, ext, err := readLinux()
		if err != nil {
			return "", err
		}
		return SaveToTemp(data, ext)
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
	if err := os.MkdirAll(pastesDir(), 0o755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(pastesDir(), "paste-*.png")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := writeDarwinPNG(path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
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
