package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner runs one ssh argv and returns what it printed. A failure that
// exited carries an ExitCode, as *exec.ExitError does.
type Runner func(ctx context.Context, argv []string) (stdout, stderr []byte, err error)

func runSSH(ctx context.Context, argv []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// controlDir is short because OpenSSH caps a control socket path near 104
// bytes, and %C alone expands to 40; macOS's os.TempDir is far too long.
func controlDir(profileDir string) string {
	sum := sha256.Sum256([]byte(profileDir))
	root := os.TempDir()
	if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
		root = "/tmp"
	}
	return filepath.Join(root, "am-ssh-"+hex.EncodeToString(sum[:])[:8])
}

// ensureControlDir runs before every call: the system may sweep /tmp, and
// another user may have planted the path, which Chmod then refuses.
func ensureControlDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create SSH control directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("check SSH control directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("SSH control directory %s is not a directory", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure SSH control directory: %w", err)
	}
	return nil
}

// sshArgv puts the destination right before the remote command. ssh reads
// options again after the destination, but stops at the first word that is
// not one, and the remote command always starts with exec.
func (c *Client) sshArgv(destination string, tty bool, words []string) []string {
	ttyFlag := "-T"
	if tty {
		ttyFlag = "-t"
	}
	return []string{
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "ForwardAgent=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60",
		"-o", "ControlPath=" + filepath.Join(c.controlDir, "%C"),
		ttyFlag,
		destination,
		remoteCommand(words),
	}
}

// remoteCommand runs words through the user's login shell, so the PATH set
// up there finds the binary. sshd hands the string to that same shell.
func remoteCommand(words []string) string {
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = quote(word)
	}
	return `exec "$SHELL" -lc ` + quote(strings.Join(quoted, " "))
}

// quote single-quotes a word for POSIX shells and fish alike. fish still
// reads \' and \\ inside single quotes, so a backslash steps outside them
// the way a quote does.
func quote(word string) string {
	var quoted strings.Builder
	quoted.WriteByte('\'')
	for i := 0; i < len(word); i++ {
		switch word[i] {
		case '\'':
			quoted.WriteString(`'\''`)
		case '\\':
			quoted.WriteString(`'\\'`)
		default:
			quoted.WriteByte(word[i])
		}
	}
	quoted.WriteByte('\'')
	return quoted.String()
}

// AttachCommand opens the row's tmux session in the user's terminal over
// the same control socket the polls use.
func (c *Client) AttachCommand(ref Ref) (*exec.Cmd, error) {
	conn, err := c.connection(ref.Host)
	if err != nil {
		return nil, err
	}
	if err := validID(ref.ID); err != nil {
		return nil, &Error{Host: ref.Host, Err: err}
	}
	if err := ensureControlDir(c.controlDir); err != nil {
		return nil, &Error{Host: ref.Host, Err: err}
	}
	argv := c.sshArgv(conn.Destination, true, []string{"tmux", "-u", "-L", "agentmgr", "attach-session", "-t", "am_" + ref.ID})
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = withoutTMUX(os.Environ())
	return cmd, nil
}

func withoutTMUX(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		if !strings.HasPrefix(entry, "TMUX=") {
			kept = append(kept, entry)
		}
	}
	return kept
}
