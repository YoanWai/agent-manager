// Package testfixture stands in for the POSIX tools the pane-driving test
// suites launch, on Windows, which has neither cat nor sh. A suite's
// TestMain calls Run first, so a copy of the test binary started under a
// fixture name runs that fixture instead of the suite, and Install to link
// those copies. Only tests import it.
package testfixture

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The test binary stands in for cat and sh: Install links copies of itself
// named cat.exe and am-test-fixture.exe into Dir, and a copy started under
// one of those names runs the fixture instead of the suite. Each one prints
// what its POSIX counterpart prints, through a line discipline that echoes
// the way a tty in canonical mode does, and runs as the process name the
// tests look for (cat for the cat-based tools).
const fixtureName = "am-test-fixture"

// Dir holds the fixture copies. Commands name them by absolute path: psmux
// spawns panes with the PATH stored in the registry, not the one this
// process carries.
var Dir string

// windowsCommands replaces the test harnesses' POSIX tool commands on
// Windows, keyed by tool name, with {bin} standing for Dir. A prompt
// appended to one lands where it does on the POSIX command: as cat's file
// argument, or slow-take's "$0". claude-hooked is the exception: the
// --settings its launch appends ends a real cat at once, leaving the pane
// on the shell, and the lines a test pastes into PowerShell run as
// commands instead of echoing, so its cat ignores its arguments.
var windowsCommands = map[string]string{
	"claude":         `{bin}\cat.exe`,
	"command-code":   `{bin}\cat.exe`,
	"claude-hooked":  `{bin}\` + fixtureName + `.exe cat-stdin`,
	"quietchat":      `{bin}\cat.exe`,
	"pi-tool":        `{bin}\cat.exe`,
	"control-echo":   `{bin}\cat.exe -v`,
	"ready-tool":     `{bin}\` + fixtureName + `.exe prompt-loop`,
	"send-tool":      `{bin}\` + fixtureName + `.exe prompt-loop`,
	"slow-take-tool": `{bin}\` + fixtureName + `.exe slow-take`,
	"mouse-tool":     `{bin}\` + fixtureName + `.exe print '\033[?1003h\033[?1006h'; {bin}\cat.exe`,
	"x10-tool":       `{bin}\` + fixtureName + `.exe print '\033[?1003h'; {bin}\cat.exe`,
}

// Tools names every test tool with a Windows stand-in.
func Tools() []string {
	tools := make([]string, 0, len(windowsCommands))
	for tool := range windowsCommands {
		tools = append(tools, tool)
	}
	return tools
}

// Command is the Windows command standing in for a test tool's, or "" for
// a tool with no stand-in.
func Command(tool string) string {
	return strings.ReplaceAll(windowsCommands[tool], "{bin}", Dir)
}

// ModeCommand is the Windows command running one fixture mode.
func ModeCommand(mode string) string {
	return Dir + `\` + fixtureName + ".exe " + mode
}

// Cat is a tool command that runs cat, for a test that swaps one in.
func Cat() string {
	if runtime.GOOS == "windows" {
		return Command("claude")
	}
	return "cat"
}

// HookedCatProcess is the process claude-hooked's cat runs as: on Windows
// the fixture stands in for it under its own name.
func HookedCatProcess() string {
	if runtime.GOOS == "windows" {
		return fixtureName
	}
	return "cat"
}

// WaitForReady waits for the file ReadyEnv named.
func WaitForReady(t *testing.T, ready string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the fixture never made its console raw")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Run runs the fixture this binary was started as, reporting false when it
// was started as the test binary.
func Run(args []string) (int, bool) {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(args[0]), filepath.Ext(args[0])))
	defer func() { restoreConsole() }()
	switch name {
	case "cat":
		return fixtureCat(nil, args[1:]), true
	case "tail":
		// tail -f /dev/null: runs, under its own name, until stopped.
		return fixtureCat(nil, nil), true
	case fixtureName:
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, fixtureName+": missing mode")
			return 2, true
		}
		return fixtureMode(args[1], args[2:]), true
	}
	return 0, false
}

// restoreConsole puts back the console modes newTTY changed.
var restoreConsole = func() {}

// Install links this binary into a fresh directory under the fixture names
// and records it in Dir, returning the directory for the caller to remove. The commands carry the path
// unquoted, the only form the tool-installed check reads as a path, so a
// directory with a space in it falls back to its short name.
func Install() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "am-fixture-bin-")
	if err != nil {
		return "", err
	}
	for _, name := range []string{"cat.exe", "tail.exe", fixtureName + ".exe"} {
		if err := linkOrCopy(exe, filepath.Join(dir, name)); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	Dir = shortPath(dir)
	if strings.ContainsAny(Dir, " \t") {
		os.RemoveAll(dir)
		return "", fmt.Errorf("fixture dir %q has a space and no short name", dir)
	}
	return dir, nil
}

func linkOrCopy(src, dst string) error {
	if os.Link(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fixtureMode(mode string, args []string) int {
	switch mode {
	case "prompt-loop":
		// printf FIRST; IFS= read -r line; printf THEN;
		// while IFS= read -r line; do printf "\n❯ "; done
		// FIRST defaults to "❯ " and THEN to the loop's own "\n❯ "; any
		// other argument (a launch prompt) is ignored, as sh -c ignores it.
		first, then := "❯ ", "\n❯ "
		for i := 0; i+1 < len(args); i++ {
			switch args[i] {
			case "--first":
				first = printfText(args[i+1])
			case "--then":
				then = printfText(args[i+1])
			}
		}
		in := newTTY()
		fmt.Print(first)
		lines := 0
		for input := range in.lines {
			if !input.newline {
				continue
			}
			if lines++; lines == 1 {
				fmt.Print(then)
			} else {
				fmt.Print("\n❯ ")
			}
		}
		return 0
	case "slow-take":
		// printf "❯ "; sleep 2; printf "\n❯ %s\n❯ " "$0"; cat. The take has to
		// outlast a whole poll pass, which costs a psmux fork per call on
		// Windows, not the milliseconds tmux runs in.
		in := newTTY()
		take := "sh"
		if len(args) > 0 {
			take = args[0]
		}
		fmt.Print("❯ ")
		time.Sleep(2 * time.Second)
		fmt.Printf("\n❯ %s\n❯ ", take)
		return fixtureCat(in, nil)
	case "cat-stdin":
		// sh -c 'exec cat' --: the terminal copied out, whatever the
		// launch appended.
		return fixtureCat(nil, nil)
	case "capture-args":
		// printf '%s\n' "$@" > FILE; cat
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, fixtureName+": capture-args needs a file")
			return 2
		}
		if err := os.WriteFile(args[0], []byte(strings.Join(args[1:], "\n")+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return fixtureCat(nil, nil)
	case "print":
		// printf '<text>'. conhost passes a mode the text sets (mouse
		// tracking) on to the terminal only from a console in VT input mode.
		restoreConsole = rawConsole()
		if len(args) > 0 {
			fmt.Print(printfText(args[0]))
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, fixtureName+": unknown mode "+mode)
	return 2
}

// printfText expands the two printf escapes the fixtures use: \033 for
// ESC and \n for a line end.
func printfText(format string) string {
	return strings.NewReplacer(`\033`, "\x1b", `\n`, "\n").Replace(format)
}

// fixtureCat is cat [-v] [file...]: file arguments are printed or reported
// missing, and with none (or "-") the terminal's lines are copied out. in
// is the terminal a caller already reads, or nil to open it on first use:
// a cat given only files never reads the keys meant for the shell.
func fixtureCat(in *tty, args []string) int {
	visible := false
	var files []string
	for i, arg := range args {
		if arg == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if arg == "-v" {
			visible = true
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			fmt.Fprintf(os.Stderr, "cat: unrecognized option '%s'\n", arg)
			return 1
		}
		files = append(files, arg)
	}
	if len(files) == 0 {
		files = []string{"-"}
	}
	code := 0
	for _, file := range files {
		if file != "-" {
			data, err := os.ReadFile(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "cat: %s: No such file or directory\n", file)
				code = 1
				continue
			}
			fmt.Print(catText(string(data), visible))
			continue
		}
		if in == nil {
			in = newTTY()
		}
		for input := range in.lines {
			text := catText(input.text, visible)
			if input.newline {
				text += "\r\n"
			}
			fmt.Print(text)
		}
	}
	return code
}

// catText renders text the way cat -v does with visible set: control
// characters in caret notation, line ends kept.
func catText(text string, visible bool) string {
	if !visible {
		return text
	}
	var out strings.Builder
	for _, r := range text {
		if r == '\n' || r == '\t' {
			out.WriteRune(r)
			continue
		}
		out.WriteString(caretNotation(r))
	}
	return out.String()
}

func caretNotation(r rune) string {
	switch {
	case r == 0x7f:
		return "^?"
	case r < 0x20:
		return "^" + string(r+0x40)
	}
	return string(r)
}

// tty is the terminal's canonical-mode line discipline over the raw
// console: input echoes as it is typed (control characters as ^X, the way
// echoctl does) and reaches the program a line at a time, backspace and
// ctrl+u erase, ctrl+d on an empty line is end of input, and ctrl+c ends
// the program the way SIGINT does. A kernel echoes a write to the terminal
// before the program reading it is scheduled, so the echo of everything
// that has arrived goes out before the lines in it are handed on: a pasted
// block echoes whole, and the program's copy of it follows.
type tty struct {
	lines chan ttyInput
}

type ttyInput struct {
	text    string
	newline bool
}

// burstGap is how long input must pause before the echo of what arrived
// goes out and its lines are handed on. ConPTY delivers a paste in pieces,
// where a kernel tty takes the whole write at once.
const burstGap = 20 * time.Millisecond

// ReadyEnv names a file the fixture creates once the console is raw.
// Keys that arrive before then meet the console's own line editing, which
// takes an up arrow as its history key and drops it.
const ReadyEnv = "AM_FIXTURE_READY"

func newTTY() *tty {
	restoreConsole = rawConsole()
	if ready := os.Getenv(ReadyEnv); ready != "" {
		if err := os.WriteFile(ready, nil, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	t := &tty{lines: make(chan ttyInput, 64)}
	runes := make(chan rune, 4096)
	go func() {
		defer close(runes)
		in := bufio.NewReader(os.Stdin)
		for {
			r, _, err := in.ReadRune()
			if err != nil {
				return
			}
			runes <- r
		}
	}()
	go func() {
		defer close(t.lines)
		var line []rune
		var echo strings.Builder
		var ready []ttyInput
		flush := func() {
			fmt.Print(echo.String())
			echo.Reset()
			for _, input := range ready {
				t.lines <- input
			}
			ready = ready[:0]
		}
		for {
			var r rune
			var ok bool
			if echo.Len() > 0 || len(ready) > 0 {
				select {
				case r, ok = <-runes:
				case <-time.After(burstGap):
					flush()
					continue
				}
			} else {
				r, ok = <-runes
			}
			if !ok {
				if len(line) > 0 {
					ready = append(ready, ttyInput{text: string(line)})
				}
				flush()
				return
			}
			switch r {
			case '\r', '\n':
				echo.WriteString("\r\n")
				ready = append(ready, ttyInput{text: string(line), newline: true})
				line = line[:0]
			case 0x04:
				if len(line) == 0 {
					flush()
					return
				}
				ready = append(ready, ttyInput{text: string(line)})
				line = line[:0]
			case 0x03:
				echo.WriteString("^C\r\n")
				fmt.Print(echo.String())
				restoreConsole()
				os.Exit(130)
			case 0x7f, 0x08:
				if len(line) > 0 {
					echo.WriteString(eraseEcho(line[len(line)-1]))
					line = line[:len(line)-1]
				}
			case 0x15:
				for i := len(line) - 1; i >= 0; i-- {
					echo.WriteString(eraseEcho(line[i]))
				}
				line = line[:0]
			default:
				line = append(line, r)
				if r == '\t' {
					echo.WriteRune(r)
				} else {
					echo.WriteString(caretNotation(r))
				}
			}
		}
	}()
	return t
}

// eraseEcho rubs out the echo of one typed rune, two cells for a control
// character echoed as ^X.
func eraseEcho(r rune) string {
	cells := 1
	if r != '\t' && (r < 0x20 || r == 0x7f) {
		cells = 2
	}
	return strings.Repeat("\b \b", cells)
}

// Prepare readies a pane-driving suite on Windows: psmux keeps its servers'
// registry under its data directory, which a fresh one isolates the way a
// private socket does for tmux, and the fixtures go into a fresh Dir. It
// does nothing elsewhere. The returned cleanup removes both.
func Prepare(name string) (cleanup func(), err error) {
	if runtime.GOOS != "windows" {
		return func() {}, nil
	}
	data, err := os.MkdirTemp("", name+"-psmux-")
	if err != nil {
		return nil, fmt.Errorf("psmux data dir: %w", err)
	}
	os.Setenv("PSMUX_DATA_DIR", data)
	bin, err := Install()
	if err != nil {
		os.RemoveAll(data)
		return nil, fmt.Errorf("test tool fixtures: %w", err)
	}
	return func() {
		os.RemoveAll(bin)
		os.RemoveAll(data)
	}, nil
}
