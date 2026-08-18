package config

import (
	"os/exec"
	"strings"

	"github.com/YoanWai/agent-manager/internal/deps"
)

var lookPath = exec.LookPath

type MissingToolError struct {
	Binary string
}

func (e MissingToolError) Error() string {
	return e.Binary + " is not installed; " + deps.Hint(e.Binary)
}

func CheckInstalled(command string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	binary := fields[0]
	if _, err := lookPath(binary); err == nil {
		return nil
	}
	return MissingToolError{Binary: binary}
}
