// Package pwsh renders text for PowerShell: quoted literals, encoded
// commands, and script files, plus the lookup of the PowerShell binary.
package pwsh

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// quoteReplacer doubles every character PowerShell accepts as a single
// quote, since each one closes a verbatim string.
var quoteReplacer = strings.NewReplacer(
	"'", "''",
	"\u2018", "\u2018\u2018",
	"\u2019", "\u2019\u2019",
	"\u201A", "\u201A\u201A",
	"\u201B", "\u201B\u201B",
)

// Quote renders s as a PowerShell verbatim single-quoted string, which
// expands nothing.
func Quote(s string) string {
	return "'" + quoteReplacer.Replace(s) + "'"
}

// EncodedCommand renders a script as -EncodedCommand takes it: UTF-16LE,
// then base64.
func EncodedCommand(script string) string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		raw = append(raw, byte(unit), byte(unit>>8))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// Path finds PowerShell 7.3 or newer (pwsh). Windows PowerShell 5.1 and
// pwsh before 7.3 hand a native program an argument holding a double quote
// without escaping it, so a prompt like `x " --flag " y` would reach the
// agent CLI as several arguments, the flag among them. A checked path is
// kept; a failure is not, so installing pwsh needs no restart.
func Path() (string, error) {
	pathMu.Lock()
	defer pathMu.Unlock()
	if checkedPath != "" {
		return checkedPath, nil
	}
	path, err := exec.LookPath("pwsh")
	if err != nil {
		return "", errors.New("agent-manager needs PowerShell 7.3 or newer (pwsh), which is missing; install it with: winget install Microsoft.PowerShell")
	}
	version, err := readVersion(path)
	if err != nil {
		return "", fmt.Errorf("reading the version of %s: %w", path, err)
	}
	if err := checkVersion(version); err != nil {
		return "", err
	}
	checkedPath = path
	return path, nil
}

var (
	pathMu      sync.Mutex
	checkedPath string
)

func readVersion(shell string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// ToString prints the bare version; the object itself formats as a
	// table with a header.
	out, err := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", "$PSVersionTable.PSVersion.ToString()").Output()
	return strings.TrimSpace(string(out)), err
}

// checkVersion judges a version as $PSVersionTable.PSVersion.ToString()
// prints it: 7.4.1, 7.5.0-preview.3.
func checkVersion(version string) error {
	major, minor, ok := majorMinor(version)
	if !ok {
		return fmt.Errorf("could not read the PowerShell version from %s; agent-manager needs PowerShell 7.3 or newer: winget install Microsoft.PowerShell", strconv.Quote(version))
	}
	if major > 7 || major == 7 && minor >= 3 {
		return nil
	}
	return fmt.Errorf("PowerShell %d.%d is too old; agent-manager needs 7.3 or newer, which passes a prompt holding \" to the agent CLI whole; upgrade it with: winget upgrade Microsoft.PowerShell", major, minor)
}

// majorMinor reads the first two dot-separated numbers of a version.
func majorMinor(version string) (int, int, bool) {
	first, rest, found := strings.Cut(version, ".")
	if !found {
		return 0, 0, false
	}
	second, _, _ := strings.Cut(rest, ".")
	major, err := strconv.Atoi(first)
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(second)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// ScriptFile renders a script body as file bytes led by a UTF-8 byte order
// mark, without which Windows PowerShell 5.1 reads the file as ANSI and
// garbles non-ASCII text.
func ScriptFile(body string) []byte {
	return []byte("\ufeff" + body)
}
