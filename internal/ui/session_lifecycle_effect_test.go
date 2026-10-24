package ui

import (
	"errors"
	"testing"
)

func TestARelaunchBatchKeepsEveryCaveatOnce(t *testing.T) {
	t.Parallel()
	labelErr := errors.New("rename failed")
	timeout := errors.New("grok MCP registration did not finish within 2m")

	var warning error
	for _, caveat := range []error{labelErr, nil, timeout, timeout} {
		warning = addCaveat(warning, caveat)
	}

	if got, want := warning.Error(), "rename failed; grok MCP registration did not finish within 2m"; got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
	if !errors.Is(warning, labelErr) || !errors.Is(warning, timeout) {
		t.Fatalf("warning = %q no longer wraps both caveats", warning)
	}
}
