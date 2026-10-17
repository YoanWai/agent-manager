package remote

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
	refSeparator        = "::"
)

// A destination can never start with a dash, so ssh cannot read it as an
// option.
var (
	destinationPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`)
	idPattern          = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// Connection is another machine's agent-manager, reached as an SSH
// destination: user@host or an alias from ~/.ssh/config.
type Connection struct {
	Name        string
	Destination string
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

func validID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid id %q", id)
	}
	return nil
}
