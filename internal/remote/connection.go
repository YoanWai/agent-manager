package remote

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/YoanWai/agent-manager/internal/remote/value"
)

// The connection values live in remote/value, so the UI can name hosts and
// rows without importing SSH.
type (
	Connection = value.Connection
	Ref        = value.Ref
)

var (
	ValidateConnection = value.ValidateConnection
	ParseRef           = value.ParseRef
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// program is how the remote command names agent-manager: a path under ~/
// expands against $HOME there, and the rest is quoted as is.
func program(c Connection) string {
	switch {
	case c.Binary == "":
		return quote("agent-manager")
	case strings.HasPrefix(c.Binary, "~/"):
		return `"$HOME"/` + quote(strings.TrimPrefix(c.Binary, "~/"))
	}
	return quote(c.Binary)
}

func validID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid id %q", id)
	}
	return nil
}
