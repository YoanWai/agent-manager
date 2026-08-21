package ui

import (
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
)

// toolBinaries names the binary each configured tool runs, keyed by tool
// name. Shells and blocks without a command hold no binary: a terminal row
// is a shell whatever gets typed into it.
type toolBinaries map[string]string

func newToolBinaries(cfg config.Config) toolBinaries {
	binaries := make(toolBinaries, len(cfg.Tools))
	for name, tool := range cfg.Tools {
		binaries[name] = toolBinary(tool)
	}
	return binaries
}

// interpreters name how a command runs rather than which CLI it is. A tool
// launched through one is unidentifiable from its processes, in either
// direction: the pane of a wrapper tool shows a shell, and a shell in any
// other pane would otherwise be read as that tool.
var interpreters = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true,
	"env": true, "node": true, "npx": true, "deno": true, "bun": true,
	"python": true, "python3": true, "ruby": true, "perl": true,
	"uv": true, "uvx": true,
}

func toolBinary(tool config.Tool) string {
	if tool.Shell {
		return ""
	}
	fields := strings.Fields(tool.Command)
	if len(fields) == 0 {
		return ""
	}
	binary := filepath.Base(fields[0])
	if interpreters[binary] {
		return ""
	}
	return binary
}

// detectRelaunchedTool names the tool a pane is running when that is not
// the tool the session was launched with, which is what quitting the agent
// and starting a different CLI from the pane's shell leaves behind.
// children are the pane shell's own child processes.
//
// It answers only where the answer is certain: the session must have been
// launched as an agent, and exactly one configured tool may claim the
// binary that is running. Everything else keeps the tool the row has,
// since retyping a row on a guess moves it onto the wrong status rules.
func detectRelaunchedTool(current string, children []string, binaries toolBinaries) string {
	currentBinary := binaries[current]
	if currentBinary == "" {
		return ""
	}
	detected := ""
	for _, child := range children {
		running := filepath.Base(child)
		if running == currentBinary {
			return ""
		}
		claimed := claimingTool(running, binaries)
		if claimed == "" || claimed == detected {
			continue
		}
		if detected != "" {
			return ""
		}
		detected = claimed
	}
	return detected
}

// claimingTool names the tool that runs a binary, and nothing when the
// binary says nothing about which tool it is: an interpreter, or a name
// that more than one configured tool runs.
func claimingTool(binary string, binaries toolBinaries) string {
	if interpreters[binary] {
		return ""
	}
	claimed := ""
	for name, candidate := range binaries {
		if candidate != binary {
			continue
		}
		if claimed != "" {
			return ""
		}
		claimed = name
	}
	return claimed
}

// applyRelaunchedTool moves a session onto the tool its pane is actually
// running. The status rules, the revive command and the row's icon all
// follow the stored tool, so a pane running something else reads wrong
// until the row agrees with it.
func (p *poller) applyRelaunchedTool(sess *store.Session, children []string) error {
	detected := detectRelaunchedTool(sess.Tool, children, p.binaries)
	if detected == "" {
		return nil
	}
	// The old tool's leftover status file goes first: it would otherwise
	// speak for a tool that writes none, and a removal that fails leaves the
	// row on its old tool for the next poll to retry.
	if err := p.hooks.Remove(sess.ID); err != nil {
		return err
	}
	if err := ignoreDeletedSession(p.store.UpdateTool(sess.ID, detected)); err != nil {
		return err
	}
	sess.Tool = detected
	// UpdateTool drops the conversation id along with the tool that minted
	// it, which no longer resumes anything.
	sess.AgentSessionID = ""
	return nil
}
