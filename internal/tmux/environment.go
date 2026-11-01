package tmux

import "fmt"

// SetServerEnvironment sets a variable every new pane on the manager's
// server starts with, or removes it when value is empty. A server that is
// not running has no panes to reach, so that is not an error.
func (d *Driver) SetServerEnvironment(name, value string) error {
	args := []string{"set-environment", "-g", "-u", name}
	if value != "" {
		args = []string{"set-environment", "-g", name, value}
	}
	return d.eachServer(func(id string) error {
		if _, err := d.runIn(id, args...); err != nil && !noServer(err.Error()) {
			return fmt.Errorf("tmux set-environment %s: %w", name, err)
		}
		return nil
	})
}
