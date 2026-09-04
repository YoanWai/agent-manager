//go:build darwin

package notify

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The manager is a bare binary, and macOS only lets an application bundle
// own a notification. So the manager keeps a bundle of itself under the
// config directory, signed ad hoc with the codesign every Mac ships, and
// posts through that copy. Notification Center then shows the manager's
// name and icon, and a click relaunches the copy, which brings the
// terminal that started the manager to the front and asks the manager to
// select the session. The user allows "Agent Manager" once, on the first
// banner, the way any app asks.

//go:embed agent-manager.icns
var helperIcon []byte

const (
	helperBundle     = "Agent Manager.app"
	helperExecutable = "agent-manager"
	helperBundleID   = "dev.agent-manager.notifier"
	// helperTimeout leaves room for the first-run permission prompt,
	// which the helper waits on before it can post anything.
	helperTimeout = 90 * time.Second
	// codesignTimeout bounds the signing step, which runs under
	// materializeMu and would otherwise hold every banner behind it.
	codesignTimeout = 30 * time.Second
	// exitDenied is how the helper reports a refused permission.
	exitDenied = 2
	// versionRetention is how long an unused build stays before it is
	// cleared, which has to outlast a manager that sits idle for days.
	versionRetention = 7 * 24 * time.Hour
)

var helperSounds = []string{"Funk", "Hero", "Basso"}

var materializeMu sync.Mutex

func postThroughHelper(sessionID, subtitle, body, sound string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	helper, err := materializeHelper(dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, helper, "post",
		"agent-manager", subtitle, body, sound+".aiff", getenv("__CFBundleIdentifier"),
		sessionID, strconv.Itoa(os.Getpid()))
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == exitDenied {
		return errDenied
	}
	return err
}

// helperHome keeps the builds of one installed binary apart from every
// other install's. Two managers from different installs would otherwise
// share a path and rebuild it out from under each other on every banner,
// since each would find the other's build there.
func helperHome(configDir, source string) string {
	digest := sha256.Sum256([]byte(source))
	return filepath.Join(configDir, "notifier", hex.EncodeToString(digest[:6]))
}

// LaunchedAsHelper: Launch Services starts the bundle's copy with no
// arguments at all when a banner is clicked, so the path is the only
// marker of what this process is.
func LaunchedAsHelper() bool {
	exe, err := os.Executable()
	return err == nil && strings.HasSuffix(exe, filepath.Join(helperBundle, "Contents", "MacOS", helperExecutable))
}

// materializeHelper returns the path of the bundle's executable, building
// it when this binary has none yet.
func materializeHelper(configDir string) (string, error) {
	materializeMu.Lock()
	defer materializeMu.Unlock()
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	if source, err = filepath.EvalSymlinks(source); err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	stamp := fmt.Sprintf("%s %d %d\n", source, info.Size(), info.ModTime().UnixNano())
	home := helperHome(configDir, source)
	version := filepath.Join(home, versionOf(stamp))
	bundle := filepath.Join(version, helperBundle)
	executable := filepath.Join(bundle, "Contents", "MacOS", helperExecutable)
	if _, err := os.Stat(executable); err == nil {
		touch(version)
		retireOldVersions(home, version)
		return executable, nil
	}
	staged := filepath.Join(home, ".staging."+strconv.Itoa(os.Getpid())+"."+strconv.FormatUint(focusSeq.Add(1), 10))
	if err := buildHelper(filepath.Join(staged, helperBundle), source, stamp); err != nil {
		os.RemoveAll(staged)
		return "", err
	}
	// A version directory is named for the binary inside it, so it is
	// written once and never rewritten: a manager mid-launch cannot have
	// its executable replaced by another manager's upgrade. Losing the
	// rename means someone else published the same build first.
	if err := os.Rename(staged, version); err != nil {
		os.RemoveAll(staged)
		if _, statErr := os.Stat(executable); statErr != nil {
			return "", err
		}
	}
	retireOldVersions(home, version)
	return executable, nil
}

func versionOf(stamp string) string {
	digest := sha256.Sum256([]byte(stamp))
	return hex.EncodeToString(digest[:6])
}

// touch dates a version by its last use, since a directory keeps the
// mtime of its creation however long the manager inside it runs.
func touch(version string) {
	now := time.Now()
	_ = os.Chtimes(version, now, now)
}

// retireOldVersions clears builds of binaries this install no longer
// runs. A manager that has not posted a banner in a week may still be
// alive, so its bundle only goes once it is that far out of use, and
// rebuilding it costs one banner.
func retireOldVersions(home, keep string) {
	entries, err := os.ReadDir(home)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(home, entry.Name())
		if path == keep {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < versionRetention {
			continue
		}
		os.RemoveAll(path)
	}
}

// buildHelper signs last, so every file it lays down is inside the seal.
func buildHelper(bundle, source, stamp string) error {
	if err := os.RemoveAll(bundle); err != nil {
		return err
	}
	macos := filepath.Join(bundle, "Contents", "MacOS")
	resources := filepath.Join(bundle, "Contents", "Resources")
	for _, dir := range []string{macos, resources} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := copyFile(source, filepath.Join(macos, helperExecutable), 0o755); err != nil {
		return err
	}
	for _, sound := range helperSounds {
		name := sound + ".aiff"
		if err := copyFile(filepath.Join("/System/Library/Sounds", name), filepath.Join(resources, name), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(resources, "agent-manager.icns"), helperIcon, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(helperInfoPlist), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(resources, "source"), []byte(stamp), 0o644); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), codesignTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "codesign", "-s", "-", "--force", bundle).Run()
}

const helperInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>` + helperBundleID + `</string>
	<key>CFBundleName</key>
	<string>Agent Manager</string>
	<key>CFBundleDisplayName</key>
	<string>Agent Manager</string>
	<key>CFBundleExecutable</key>
	<string>` + helperExecutable + `</string>
	<key>CFBundleIconFile</key>
	<string>agent-manager</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>1.0</string>
	<key>CFBundleVersion</key>
	<string>1</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// HelperMain runs in the bundle's copy of this binary: with "post" and
// the banner fields when the manager posts, and with no arguments at all
// when Launch Services relaunches it for a click. Its exit code is what
// the manager reads.
func HelperMain(args []string) int {
	runtime.LockOSThread()
	for _, lib := range []string{
		"/System/Library/Frameworks/AppKit.framework/AppKit",
		"/System/Library/Frameworks/UserNotifications.framework/UserNotifications",
	} {
		if _, err := purego.Dlopen(lib, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return 1
		}
	}
	app := objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication"))
	center := objc.ID(objc.GetClass("UNUserNotificationCenter")).Send(sel("currentNotificationCenter"))
	delegate, err := newHelperDelegate()
	if err != nil {
		return 1
	}
	center.Send(sel("setDelegate:"), delegate)

	exit := make(chan int, 1)
	if len(args) == 8 && args[0] == "post" {
		title, subtitle, body, sound, terminal, sessionID := args[1], args[2], args[3], args[4], args[5], args[6]
		manager, err := strconv.Atoi(args[7])
		if err != nil {
			return 1
		}
		granted := objc.NewBlock(func(_ objc.Block, ok bool, _ objc.ID) {
			if !ok {
				exit <- exitDenied
				return
			}
			content := objc.ID(objc.GetClass("UNMutableNotificationContent")).Send(sel("alloc")).Send(sel("init"))
			content.Send(sel("setTitle:"), nsString(title))
			content.Send(sel("setSubtitle:"), nsString(subtitle))
			content.Send(sel("setBody:"), nsString(body))
			content.Send(sel("setSound:"), objc.ID(objc.GetClass("UNNotificationSound")).Send(sel("soundNamed:"), nsString(sound)))
			info := objc.ID(objc.GetClass("NSMutableDictionary")).Send(sel("dictionary"))
			info.Send(sel("setObject:forKey:"), nsString(terminal), nsString("terminal"))
			info.Send(sel("setObject:forKey:"), nsString(sessionID), nsString("session"))
			info.Send(sel("setObject:forKey:"), nsString(strconv.Itoa(manager)), nsString("manager"))
			content.Send(sel("setUserInfo:"), info)
			request := objc.ID(objc.GetClass("UNNotificationRequest")).Send(sel("requestWithIdentifier:content:trigger:"),
				nsString(fmt.Sprintf("agent-manager-%d", time.Now().UnixNano())), content, objc.ID(0))
			posted := objc.NewBlock(func(_ objc.Block, nserr objc.ID) {
				if nserr != 0 {
					exit <- 1
					return
				}
				exit <- 0
			})
			center.Send(sel("addNotificationRequest:withCompletionHandler:"), request, posted)
		})
		center.Send(sel("requestAuthorizationWithOptions:completionHandler:"), uint(1|2|4), granted)
	} else {
		// A relaunch for a click gets its response as soon as the run loop
		// starts; anything longer means there was none to deliver.
		go func() {
			time.Sleep(10 * time.Second)
			exit <- 1
		}()
	}
	go func() {
		code := <-exit
		// Let the completion handler that sent the code return first.
		time.Sleep(200 * time.Millisecond)
		os.Exit(code)
	}()
	helperExit = exit
	app.Send(sel("run"))
	return 0
}

// helperExit lets the delegate end the run loop HelperMain started.
var helperExit chan<- int

func newHelperDelegate() (objc.ID, error) {
	class, err := objc.RegisterClass("AgentManagerNotifierDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("UNUserNotificationCenterDelegate")}, nil,
		[]objc.MethodDef{
			{
				Cmd: sel("userNotificationCenter:willPresentNotification:withCompletionHandler:"),
				Fn: func(_ objc.ID, _ objc.SEL, _, _ objc.ID, handler objc.Block) {
					const banner, sound, list = 1 << 4, 1 << 1, 1 << 3
					callBlock(handler, banner|sound|list)
				},
			},
			{
				Cmd: sel("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"),
				Fn: func(_ objc.ID, _ objc.SEL, _, response objc.ID, handler objc.Block) {
					info := response.Send(sel("notification")).Send(sel("request")).Send(sel("content")).Send(sel("userInfo"))
					revealTerminal(goString(info.Send(sel("objectForKey:"), nsString("terminal"))))
					manager, convErr := strconv.Atoi(goString(info.Send(sel("objectForKey:"), nsString("manager"))))
					dir, dirErr := configDir()
					if convErr == nil && dirErr == nil {
						session := goString(info.Send(sel("objectForKey:"), nsString("session")))
						// Nothing reads this process's exit code on a
						// click, so a failed handoff can only mean the
						// cursor stays where it was.
						_ = RequestFocus(dir, manager, session)
					}
					callBlock(handler)
					helperExit <- 0
				},
			},
		})
	if err != nil {
		return 0, err
	}
	return objc.ID(class).Send(sel("alloc")).Send(sel("init")), nil
}

// revealTerminal activates the app the manager was launched from. The
// click already made the helper the active app, which is what lets it
// pass activation on.
func revealTerminal(bundleID string) {
	if bundleID == "" {
		return
	}
	apps := objc.ID(objc.GetClass("NSRunningApplication")).Send(sel("runningApplicationsWithBundleIdentifier:"), nsString(bundleID))
	target := apps.Send(sel("firstObject"))
	if target == 0 {
		return
	}
	const allWindows, ignoringOtherApps = 1 << 0, 1 << 1
	target.Send(sel("activateWithOptions:"), uint(allWindows|ignoringOtherApps))
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func nsString(s string) objc.ID {
	raw := append([]byte(s), 0)
	id := objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), unsafe.Pointer(&raw[0]))
	runtime.KeepAlive(raw)
	return id
}

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	const utf8Encoding = 4
	start := objc.Send[*byte](ns, sel("UTF8String"))
	length := objc.Send[uint](ns, sel("lengthOfBytesUsingEncoding:"), uint(utf8Encoding))
	if start == nil || length == 0 {
		return ""
	}
	return strings.Clone(unsafe.String(start, length))
}

// callBlock invokes a block the Objective-C side created. Its function
// pointer sits after the isa, flags and reserved words of the literal.
func callBlock(block objc.Block, args ...uintptr) {
	literal := *(*unsafe.Pointer)(unsafe.Pointer(&block))
	invoke := *(*uintptr)(unsafe.Add(literal, 16))
	purego.SyscallN(invoke, append([]uintptr{uintptr(block)}, args...)...)
}
