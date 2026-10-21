// Package value holds what a connection is and how its rows are named,
// with no SSH in it, so the rail and the connections UI can use them
// without importing package remote.
package value

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNameBytes        = 40
	maxDestinationBytes = 255
	maxBinaryBytes      = 1024
	refSeparator        = "::"
)

// A destination can never start with a dash, so ssh cannot read it as an
// option.
var destinationPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`)

// Connection is another machine's agent-manager, reached as an SSH
// destination: user@host or an alias from ~/.ssh/config. Binary is where
// agent-manager is installed there, an absolute path or one under ~/;
// empty finds it on the login shell's PATH.
type Connection struct {
	Name        string
	Destination string
	Binary      string
}

// ValidateConnection checks next against the connections it would sit
// beside; an edit passes the others without the connection being edited.
func ValidateConnection(next Connection, others []Connection) error {
	name := next.Name
	switch {
	case name == "":
		return errors.New("a connection needs a name")
	case strings.TrimSpace(name) != name:
		return errors.New("a connection name cannot start or end with a space")
	case strings.Contains(name, refSeparator):
		return errors.New(`a connection name cannot contain "::"`)
	case strings.HasSuffix(name, ":"):
		// <name>::<id> splits at the first "::", so a trailing ":" would
		// move into every id.
		return errors.New(`a connection name cannot end with ":"`)
	case strings.Contains(name, "/"):
		return errors.New("a connection name cannot contain /")
	case !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0:
		return errors.New("a connection name cannot contain control characters")
	case len(name) > maxNameBytes:
		return fmt.Errorf("a connection name can be at most %d bytes long", maxNameBytes)
	}
	for _, other := range others {
		if other.Name == name {
			return fmt.Errorf("a connection named %s already exists", name)
		}
	}
	destination := next.Destination
	switch {
	case destination == "":
		return errors.New("a connection needs an SSH destination, such as user@host or an alias from ~/.ssh/config")
	case len(destination) > maxDestinationBytes:
		return fmt.Errorf("an SSH destination can be at most %d bytes long", maxDestinationBytes)
	case !destinationPattern.MatchString(destination):
		return errors.New("an SSH destination holds only letters, digits and _ . @ : -, and starts with a letter, digit or _")
	}
	binary := next.Binary
	switch {
	case binary == "":
	case len(binary) > maxBinaryBytes:
		return fmt.Errorf("an agent-manager path can be at most %d bytes long", maxBinaryBytes)
	case !utf8.ValidString(binary) || strings.IndexFunc(binary, unicode.IsControl) >= 0:
		return errors.New("an agent-manager path cannot contain control characters")
	case !strings.HasPrefix(binary, "/") && !strings.HasPrefix(binary, "~/"):
		return errors.New("an agent-manager path starts with / or ~/")
	case strings.HasSuffix(binary, "/"):
		return errors.New("an agent-manager path names the binary, not its directory")
	}
	return nil
}

// Ref addresses a row on a connection, written <connection>::<id>.
type Ref struct {
	Host string
	ID   string
}

func ParseRef(s string) (Ref, bool) {
	host, id, ok := strings.Cut(s, refSeparator)
	if !ok || host == "" || id == "" {
		return Ref{}, false
	}
	return Ref{Host: host, ID: id}, true
}

func (r Ref) String() string {
	return r.Host + refSeparator + r.ID
}
