package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadWritesAndParsesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := writeDefault(path); err != nil {
		t.Fatalf("writeDefault: %v", err)
	}
	var cfg Config
	if err := decodeInto(path, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cfg.applyDefaults()

	if cfg.PollInterval.Duration != 2*time.Second {
		t.Fatalf("poll interval = %v want 2s", cfg.PollInterval.Duration)
	}
	if _, ok := cfg.Tools["claude"]; !ok {
		t.Fatal("expected claude tool in default config")
	}
	if _, ok := cfg.Tools["opencode"]; !ok {
		t.Fatal("expected opencode tool in default config")
	}
	if _, ok := cfg.Tools["codex"]; !ok {
		t.Fatal("expected codex tool in default config")
	}
	if cfg.Tools["codex"].Command != "codex" {
		t.Fatalf("codex command = %q", cfg.Tools["codex"].Command)
	}
	if got := cfg.Tools["codex"].ReviveCommand; got != "codex resume --last" {
		t.Fatalf("codex revive_command = %q want \"codex resume --last\"", got)
	}
	if got := cfg.Tools["codex"].PromptFlag; got != "" {
		t.Fatalf("codex prompt_flag = %q want empty (positional prompt)", got)
	}
	if _, ok := cfg.Tools["grok"]; !ok {
		t.Fatal("expected grok tool in default config")
	}
	if cfg.Tools["grok"].Command != "grok" {
		t.Fatalf("grok command = %q", cfg.Tools["grok"].Command)
	}
	if got := cfg.Tools["grok"].ReviveCommand; got != "grok --continue" {
		t.Fatalf("grok revive_command = %q want \"grok --continue\"", got)
	}
	if got := cfg.Tools["grok"].PromptFlag; got != "" {
		t.Fatalf("grok prompt_flag = %q want empty (positional prompt)", got)
	}
	if cfg.Tools["claude"].Command != "claude" {
		t.Fatalf("claude command = %q", cfg.Tools["claude"].Command)
	}
	if got := cfg.Tools["opencode"].PromptFlag; got != "--prompt" {
		t.Fatalf("opencode prompt_flag = %q want --prompt", got)
	}
	if got := cfg.Tools["claude"].PromptFlag; got != "" {
		t.Fatalf("claude prompt_flag = %q want empty (positional prompt)", got)
	}
}

func TestBackfillToolDefaults(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"opencode": {Command: "opencode", ReviveCommand: "opencode --continue"},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cfg.Tools["opencode"].PromptFlag; got != "--prompt" {
		t.Fatalf("opencode prompt_flag = %q want --prompt (backfilled)", got)
	}
	if got := cfg.Tools["opencode"].ReviveCommand; got != "opencode --continue" {
		t.Fatalf("opencode revive_command = %q want user value kept", got)
	}
	if _, ok := cfg.Tools["claude"]; !ok {
		t.Fatal("expected claude tool added from built-in defaults")
	}
}

func TestApplyDefaults(t *testing.T) {
	var cfg Config
	cfg.applyDefaults()
	if cfg.PollInterval.Duration != 2*time.Second {
		t.Fatalf("poll = %v", cfg.PollInterval.Duration)
	}
	if cfg.Tools == nil {
		t.Fatal("tools should be non-nil after defaults")
	}
}

func TestDefaultResumeByIDFields(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	// Tools that accept a chosen id launch with it and resume by it.
	for _, name := range []string{"claude", "grok"} {
		tool := cfg.Tools[name]
		if tool.SessionIDFlag != "--session-id" {
			t.Fatalf("%s session_id_flag = %q want --session-id", name, tool.SessionIDFlag)
		}
		if tool.ResumeByIDCommand == "" || !strings.Contains(tool.ResumeByIDCommand, "{id}") {
			t.Fatalf("%s resume_by_id_command = %q want an {id} template", name, tool.ResumeByIDCommand)
		}
	}
	// Tools that mint their own id declare a store to capture it from.
	for _, name := range []string{"codex", "opencode"} {
		tool := cfg.Tools[name]
		if tool.SessionStore != name {
			t.Fatalf("%s session_store = %q want %q", name, tool.SessionStore, name)
		}
		if tool.SessionIDFlag != "" {
			t.Fatalf("%s session_id_flag = %q want empty (no launch flag)", name, tool.SessionIDFlag)
		}
		if !strings.Contains(tool.ResumeByIDCommand, "{id}") {
			t.Fatalf("%s resume_by_id_command = %q want an {id} template", name, tool.ResumeByIDCommand)
		}
	}
}

func TestBackfillFillsResumeFields(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"claude": {Command: "claude", ReviveCommand: "claude --continue"},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	tool := cfg.Tools["claude"]
	if tool.SessionIDFlag != "--session-id" {
		t.Fatalf("claude session_id_flag = %q want backfilled --session-id", tool.SessionIDFlag)
	}
	if tool.ResumeByIDCommand != "claude --resume {id}" {
		t.Fatalf("claude resume_by_id_command = %q want backfilled", tool.ResumeByIDCommand)
	}
}
