package sessioncmd

import (
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// PromptHeadCommand is the hidden subcommand an attached ← runs. tmux
// passes it the attached session's name.
const PromptHeadCommand = "prompt-head"

// ApplyBindingSettings sets what the in-session bindings are built from:
// the key table, and the ← check while the arrow step is on. Every process
// that installs bindings reads both here, so none of them strips what
// another put on the server.
func ApplyBindingSettings(st *store.Store, driver *tmux.Driver) error {
	keys, err := st.SessionKeys()
	if err != nil {
		return err
	}
	arrowStep, err := st.ArrowStep()
	if err != nil {
		return err
	}
	driver.SetSessionKeys(keys)
	check := ""
	if arrowStep {
		check = tmux.ShellQuote(launch.Executable()) + " " + PromptHeadCommand
	}
	driver.SetPromptHeadCheck(check)
	return nil
}
