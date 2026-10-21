// Package remotetest reads the ssh argv a remote.Client runs and writes
// what a host prints back, for a fake that stands in for ssh. The remote
// package's own tests run through it, so it cannot drift from the real
// command.
package remotetest

import (
	"fmt"
	"strings"
)

const (
	loginShell = `exec "$SHELL" -lc `
	marker     = "\x1eagent-manager\x1e"
)

// Words splits the remote command, argv's last element, back into the
// words the host runs: agent-manager and its arguments, or tmux for an
// attach.
func Words(argv []string) ([]string, error) {
	command := argv[len(argv)-1]
	inner, ok := strings.CutPrefix(command, loginShell)
	if !ok {
		return nil, fmt.Errorf("remote command %q does not go through the login shell", command)
	}
	outer, err := split(inner)
	if err != nil {
		return nil, err
	}
	if len(outer) != 1 {
		return nil, fmt.Errorf("login shell gets %d words, want 1", len(outer))
	}
	script := outer[0]
	if words, ok := strings.CutPrefix(script, "printf %s '"+marker+"'; exec "); ok {
		script = words
	}
	return split(script)
}

// Answer is stdout as a host prints it: the marker its login shell prints
// once the profile has run, then agent-manager's own output.
func Answer(stdout []byte) []byte {
	return append([]byte(marker), stdout...)
}

// split reads the subset of POSIX shell quoting the remote package writes.
func split(line string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; ch {
		case ' ':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		case '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated quote in %q", line)
			}
			word.WriteString(line[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case '\\':
			if i+1 == len(line) {
				return nil, fmt.Errorf("trailing backslash in %q", line)
			}
			i++
			word.WriteByte(line[i])
			inWord = true
		default:
			return nil, fmt.Errorf("unquoted %q in %q", ch, line)
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}
