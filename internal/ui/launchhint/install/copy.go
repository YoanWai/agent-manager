package install

import "strings"

// Tool is a CLI a launch needs, as the copy names it: the binary, the copy a
// WSL distro cannot run, and the versions an old install falls below.
type Tool struct {
	Binary      string
	WindowsPath string
	Installed   string
	Minimum     string
}

// MissingText opens on why the launch stopped, then the command that
// unblocks it. A CLI installed only on Windows is on this PATH through WSL
// interop, which the distro cannot run as the agent, so the text says where
// that copy is rather than claiming nothing is installed.
func MissingText(missing Tool, hint string) string {
	head := missing.Binary + " is not installed."
	if missing.WindowsPath != "" {
		head = missing.Binary + " is installed on Windows, not in this WSL distro.\n\n" +
			"The Windows copy is at " + missing.WindowsPath + "."
	}
	return head + "\n\n" + hint
}

// OutdatedText opens on the version gap, then the command that closes it. An
// empty command means the manager knows no installer for the CLI, so the text
// asks for the update the way the user installed it.
func OutdatedText(outdated Tool, command string) string {
	head := outdated.Binary + " " + outdated.Installed + " is older than " + outdated.Minimum +
		", the oldest version Agent Manager supports."
	if command != "" {
		return head + "\n\nUpdate it with: " + command
	}
	return head + "\n\nUpdate it the way you installed it, then spawn again."
}

// HermesText opens on what a Hermes session carries, then the line that adds
// the package it cannot load.
func HermesText(pipCommand string) string {
	if pipCommand == "" {
		return "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
			"Install the mcp package into the Python that runs Hermes, then spawn again."
	}
	return "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
		"Run `" + pipCommand + "` to add the mcp package to the Python that runs Hermes, then spawn again."
}

type SettleState uint8

const (
	// Running is an install whose script has not written a status yet.
	Running SettleState = iota
	// ShellGone is an install whose shell exited and left no status behind.
	ShellGone
	Exited
	// BinaryMissing is an install whose command succeeded and left nothing
	// for the launch to find on PATH.
	BinaryMissing
	Ready
)

// ReadStatus reads the status an install's script wrote: Running while the
// file is still empty, since the shell creates it before printf fills it.
func ReadStatus(data []byte) (code string, state SettleState) {
	code = strings.TrimSpace(string(data))
	if code == "" {
		return "", Running
	}
	if code != "0" {
		return code, Exited
	}
	return code, Ready
}

// SettleMessage is the status line an install that ended writes, and whether
// it reports a failure. It is empty for an install whose shell left no status
// behind, which the launch it was retrying already spoke for.
func SettleMessage(state SettleState, binary, name, code string) (string, bool) {
	switch state {
	case Exited:
		return binary + " install exited with status " + code + "; its output is in " + name, true
	case BinaryMissing:
		return binary + " installer finished, but " + binary + " is still not on PATH; add its directory to PATH, the installer's output names it", true
	case Ready:
		return binary + " installed", false
	}
	return "", false
}
