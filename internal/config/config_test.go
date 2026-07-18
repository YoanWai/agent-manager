package config

import (
	"path/filepath"
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
	if _, ok := cfg.Tools["grok"]; !ok {
		t.Fatal("expected grok tool in default config")
	}
	if cfg.Tools["grok"].Command != "grok" {
		t.Fatalf("grok command = %q", cfg.Tools["grok"].Command)
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
