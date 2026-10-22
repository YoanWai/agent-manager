//go:build windows

package hooks

import (
	"errors"
	"os"
	"time"

	"github.com/YoanWai/agent-manager/internal/pwsh"
	"golang.org/x/sys/windows"
)

// installScriptExt names the install script PowerShell runs; -File
// accepts nothing but .ps1.
const installScriptExt = ".install.ps1"

// hookPrefix runs an encoded script. Claude Code runs a hook under Git
// Bash when it has one and PowerShell otherwise; this line, with base64
// holding no shell metacharacters, parses the same in bash, PowerShell
// and cmd, and powershell.exe ships with every Windows install.
const hookPrefix = "powershell.exe -NoProfile -NonInteractive -EncodedCommand "

func statusCommand(state string) string {
	return hookPrefix + pwsh.EncodedCommand("[IO.File]::WriteAllText($env:"+EnvStatusFile+", '"+state+"')")
}

func sessionEndCommand() string {
	return hookPrefix + pwsh.EncodedCommand("Remove-Item -LiteralPath $env:"+EnvStatusFile+" -ErrorAction SilentlyContinue")
}

// installScriptContent carries a BOM so Windows PowerShell 5.1 reads a
// non-ASCII command as UTF-8.
func installScriptContent(body string) []byte {
	return pwsh.ScriptFile(body)
}

// mailboxRetry bounds how long a mailbox read or move waits out another
// handle on the file.
const mailboxRetry = 2 * time.Second

// Windows refuses to open a file another process is replacing, and to
// replace or move one another process has open, failing with access
// denied or a sharing violation until that handle closes; a mailbox the
// writer and the poller share retries rather than reading as absent or
// dropping the write.
func retryShared(op func() error) error {
	deadline := time.Now().Add(mailboxRetry)
	for delay := time.Millisecond; ; delay = min(2*delay, 10*time.Millisecond) {
		err := op()
		if err == nil || !transientShare(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
	}
}

func transientShare(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func readMailbox(path string) (raw []byte, err error) {
	err = retryShared(func() error {
		raw, err = os.ReadFile(path)
		return err
	})
	return raw, err
}

func moveMailbox(from, to string) error {
	return retryShared(func() error { return os.Rename(from, to) })
}
