package ui

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/composer"
)

func TestPasteSweepMsgSurfacesErrorOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.Update(composer.SweepMsg{Err: errors.New("permission denied")})
	if m.errBar.text == "" {
		t.Fatal("a failed sweep must reach the user")
	}
	m.errBar.text = ""
	m.Update(composer.SweepMsg{})
	if m.errBar.text != "" {
		t.Fatalf("a clean sweep must stay silent, got %q", m.errBar.text)
	}
}
