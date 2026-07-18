package ui

import (
	"reflect"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

func TestSortedToolNamesOrder(t *testing.T) {
	cfg := config.Config{Tools: map[string]config.Tool{
		"grok":     {Command: "grok"},
		"codex":    {Command: "codex"},
		"claude":   {Command: "claude"},
		"opencode": {Command: "opencode"},
		"zephyr":   {Command: "zephyr"},
		"acme":     {Command: "acme"},
	}}
	got := sortedToolNames(cfg)
	want := []string{"claude", "opencode", "codex", "grok", "acme", "zephyr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedToolNames = %v want %v", got, want)
	}
}
