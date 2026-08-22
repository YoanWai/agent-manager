// Package clipboard reads image data off the OS clipboard so the quick
// prompt can attach a pasted screenshot to an agent session.
package clipboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/charmbracelet/x/ansi"
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
	getenv   = os.Getenv
	// emitSeq writes a control sequence to the terminal drawing the app.
	emitSeq = termseq.Emit
	// readNativeImage is set by platform files (darwin/linux) to an
	// in-process pasteboard reader. Nil means "use the shell-tool path".
	// Prefer native: spawning osascript/wl-paste is tens of ms each paste.
	readNativeImage func() ([]byte, error)
)

// pastesDir is the shared temp directory for clipboard image files.
func pastesDir() string {
	return filepath.Join(os.TempDir(), "agent-manager-pastes")
}

// IsPastePath reports whether a word is one of the image paths a pasted
// picture becomes on its way to an agent, so a reader showing the prompt
// back can leave the pictures out of it.
func IsPastePath(word string) bool {
	return filepath.Dir(word) == pastesDir()
}

// StaleAfter is how long a pasted image stays on disk before a sweep can
// take it: long enough for an agent to reopen an image from an earlier
// session, short enough that screenshots do not pile up in temp.
const StaleAfter = 7 * 24 * time.Hour

// SweepStale deletes paste files older than maxAge. A pasted image outlives
// the prompt that names it, so nothing deletes it at send time; age is what
// tells a spent paste from one an agent may still open. A pastes directory
// that does not exist yet is nothing to sweep.
func SweepStale(maxAge time.Duration) error {
	entries, err := os.ReadDir(pastesDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	var failures []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "paste-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			failures = append(failures, err)
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		// Another manager instance sweeping the shared temp directory can
		// win the race; its removal is the outcome we wanted anyway.
		if err := os.Remove(filepath.Join(pastesDir(), entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// SaveImage writes a clipboard image into the pastes directory and returns
// its absolute path. Prefers an in-process native pasteboard read when
// available, then platform shell tools.
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

// jxaReadPNG is the Darwin fallback when the native purego pasteboard
// cannot initialize. Prefer NSPasteboardTypePNG; TIFF only if needed.
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

// writeDarwinPNGJXA dumps the clipboard image as PNG to path using JXA.
func writeDarwinPNGJXA(path string) error {
	if _, err := runCmd("osascript", "-l", "JavaScript", "-e", jxaReadPNG, path); err != nil {
		return ErrNoImage
	}
	return nil
}

// saveDarwinImage prefers the in-process AppKit reader (microseconds to low
// milliseconds). Falls back to JXA osascript only when native is unavailable.
func saveDarwinImage() (string, error) {
	if readNativeImage != nil {
		data, err := readNativeImage()
		if err == nil && len(data) > 0 {
			return SaveToTemp(data, "png")
		}
		// Native initialized but clipboard has no image: do not pay for JXA.
		if errors.Is(err, ErrNoImage) || (err == nil && len(data) == 0) {
			return "", ErrNoImage
		}
		// Native failed to init; fall through to JXA.
	}
	path, err := newPasteFile("png")
	if err != nil {
		return "", err
	}
	if err := writeDarwinPNGJXA(path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if !fileNonEmpty(path) {
		_ = os.Remove(path)
		return "", ErrNoImage
	}
	return path, nil
}

// saveLinuxImage writes a clipboard PNG. Order:
//  1. In-process native pasteboard (X11/Wayland via golang.design/x/clipboard)
//  2. wl-paste (Wayland CLI)
//  3. xclip (X11 CLI)
//  4. On WSL: Windows clipboard via powershell.exe
func saveLinuxImage() (string, error) {
	if readNativeImage != nil {
		data, err := readNativeImage()
		if err == nil && len(data) > 0 {
			return SaveToTemp(data, "png")
		}
	}

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
		if errors.Is(err, ErrNoImage) {
			return "", ErrNoImage
		}
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

// WriteText puts plain text on the OS clipboard. Used by the focused
// preview's own selection, which the host terminal never sees because the
// app holds mouse reporting.
//
// stdout and stderr stay unwired (os/exec points them at /dev/null): X11
// writers like xclip fork a daemon that holds the selection, and any pipe
// it inherits keeps a capturing call waiting for EOF that never comes.
// The timeout covers a writer that hangs before daemonizing.
func WriteText(text string) error {
	name, args, ok := copyCommand()
	if !ok {
		return writeTextOSC52(text)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(clipboardText(name, text))
	if err := cmd.Run(); err != nil {
		// A selected writer can still die at runtime: a stale SSH DISPLAY,
		// a Wayland socket that is gone, a broken install. The terminal's
		// clipboard is still reachable, so try it before reporting failure.
		if osc52Err := writeTextOSC52(text); osc52Err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%s: timed out", name)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func clipboardText(name, text string) []byte {
	if goos != "linux" || !wslProbe() || !strings.EqualFold(filepath.Base(name), "clip.exe") {
		return []byte(text)
	}
	units := utf16.Encode([]rune(text))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	return data
}

// copyCommand picks the platform's clipboard writer. The false return means
// no local writer can reach a clipboard, which is the headless-host case
// WriteText answers with OSC 52.
func copyCommand() (string, []string, bool) {
	switch goos {
	case "darwin":
		return "pbcopy", nil, true
	case "windows":
		return "clip", nil, true
	default:
		if wslProbe() {
			return "clip.exe", nil, true
		}
		// wl-copy, xclip and xsel all talk to a display server, so on a
		// headless box an installed one is still a writer that fails at
		// runtime with "Can't open display".
		if !hasDisplay() {
			return "", nil, false
		}
		if _, err := lookPath("wl-copy"); err == nil {
			return "wl-copy", nil, true
		}
		if _, err := lookPath("xclip"); err == nil {
			return "xclip", []string{"-selection", "clipboard"}, true
		}
		if _, err := lookPath("xsel"); err == nil {
			return "xsel", []string{"--clipboard", "--input"}, true
		}
		return "", nil, false
	}
}

// hasDisplay reports whether a display server this process can reach exists.
func hasDisplay() bool {
	return getenv("WAYLAND_DISPLAY") != "" || getenv("DISPLAY") != ""
}

// osc52Limit caps the base64 payload, matching xterm's own OSC 52 ceiling.
// Past it a terminal drops the sequence, so a copy that would be silently
// lost is reported instead.
const osc52Limit = 74994

// writeTextOSC52 hands the text to the terminal that is drawing us, which
// on a remote headless host is the only process with a real clipboard. The
// app's mouse grab is no obstacle: that only bears on reading a selection.
func writeTextOSC52(text string) error {
	if encodedLen := base64.StdEncoding.EncodedLen(len(text)); encodedLen > osc52Limit {
		return fmt.Errorf("selection is too large to send to the terminal clipboard (%d of %d bytes)", encodedLen, osc52Limit)
	}
	return emitSeq(ansi.SetSystemClipboard(text))
}
