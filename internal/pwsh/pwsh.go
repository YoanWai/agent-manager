// Package pwsh renders text for PowerShell: quoted literals, encoded
// commands, and script files, plus the lookup of the PowerShell binary.
package pwsh

import (
	"encoding/base64"
	"errors"
	"os/exec"
	"strings"
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

// Path finds PowerShell 7 (pwsh), falling back to Windows PowerShell.
func Path() (string, error) {
	if path, err := exec.LookPath("pwsh"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("powershell"); err == nil {
		return path, nil
	}
	return "", errors.New("PowerShell not found on PATH: install PowerShell 7 (winget install Microsoft.PowerShell)")
}

// ScriptFile renders a script body as file bytes led by a UTF-8 byte order
// mark, without which Windows PowerShell 5.1 reads the file as ANSI and
// garbles non-ASCII text.
func ScriptFile(body string) []byte {
	return []byte("\ufeff" + body)
}
