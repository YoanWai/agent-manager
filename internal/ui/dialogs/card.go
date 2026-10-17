// Package dialogs holds the small modal dialogs: confirm, rename, move,
// fork, the New Group form and the launch fix. Each owns its state, keys
// and view, and returns a typed outcome; the root adapters run the store,
// tmux and effect work.
package dialogs

import "github.com/charmbracelet/bubbles/textinput"

// Card is a dialog's content; the root draws the chrome around it.
type Card struct {
	Title string
	Body  string
	Hints [][2]string
}

func newField(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	return in
}

func displayGroup(path string) string {
	if path == "" {
		return "root"
	}
	return path
}
