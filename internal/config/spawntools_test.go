package config

import (
	"reflect"
	"testing"
)

func TestAgentToolNamesOrder(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"grok":     {Command: "grok"},
		"muse":     {Command: "muse"},
		"gemini":   {Command: "gemini"},
		"codex":    {Command: "codex"},
		"claude":   {Command: "claude"},
		"opencode": {Command: "opencode"},
		"pi":       {Command: "pi"},
		"zephyr":   {Command: "zephyr"},
		"acme":     {Command: "acme"},
		"terminal": {Shell: true},
	}}
	got := cfg.AgentToolNames()
	want := []string{"claude", "opencode", "codex", "grok", "gemini", "pi", "acme", "muse", "zephyr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AgentToolNames = %v want %v", got, want)
	}
}

func TestDefaultAgentToolKeepsAnEnabledChoiceElseTakesTheFirstEnabled(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"claude":   {Command: "claude"},
		"codex":    {Command: "codex"},
		"terminal": {Shell: true},
	}}
	for _, tc := range []struct {
		name   string
		chosen string
		hidden map[string]bool
		want   string
	}{
		{name: "an enabled choice", chosen: "codex", want: "codex"},
		{name: "no choice", want: "claude"},
		{name: "a CLI this build dropped", chosen: "deleted-tool", want: "claude"},
		{name: "a hidden choice", chosen: "codex", hidden: map[string]bool{"codex": true}, want: "claude"},
		{name: "the first one hidden", hidden: map[string]bool{"claude": true}, want: "codex"},
		{name: "the shell", chosen: "terminal", want: "claude"},
		{name: "every CLI hidden", chosen: "codex", hidden: map[string]bool{"claude": true, "codex": true}, want: ""},
	} {
		if got := cfg.DefaultAgentTool(tc.chosen, tc.hidden); got != tc.want {
			t.Errorf("%s: DefaultAgentTool = %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseHiddenTools(t *testing.T) {
	if got := ParseHiddenTools(""); got != nil {
		t.Fatalf("empty parse = %v", got)
	}
	got := ParseHiddenTools("codex, grok")
	if !got["codex"] || !got["grok"] || len(got) != 2 {
		t.Fatalf("parse = %v", got)
	}
}
