package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
)

// fetchFake runs the reader kind against the stand-in CLI named fake.
func fetchFake(t *testing.T, kind, fake string) (Catalog, error) {
	t.Helper()
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CATALOG_TESTDATA", testdata)
	t.Setenv("CATALOG_FAKE", fake)
	return Fetch(context.Background(), kind, os.Args[0], t.TempDir())
}

func TestReadersTakeEveryValueFromTheCLI(t *testing.T) {
	claudeLevels := []string{"low", "medium", "high", "xhigh", "max"}
	codexLevels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	for _, tc := range []struct {
		kind, fake string
		want       Catalog
	}{
		{"claude", "claude", Catalog{Models: []Model{
			{ID: "default", Label: "Default (recommended)", Efforts: claudeLevels, Default: true},
			{ID: "opus", Label: "Opus 5.5", Efforts: claudeLevels},
			{ID: "claude-fable-5-1", Label: "Fable 5.1", Efforts: claudeLevels},
			{ID: "sonnet", Label: "Sonnet 5.5", Efforts: claudeLevels},
			{ID: "haiku", Label: "Haiku 4.5"},
		}}},
		// The model the server asked about in the middle of the listing is
		// refused, the hidden one left out, and the second page followed.
		{"codex", "codex", Catalog{Models: []Model{
			{ID: "gpt-6.1-sol", Label: "GPT-6.1-Sol", Efforts: codexLevels, DefaultEffort: "low", Default: true},
			{ID: "gpt-6-astra", Label: "GPT-6-Astra", Efforts: codexLevels, DefaultEffort: "low"},
			{ID: "gpt-6-sol", Label: "GPT-6-Sol", Efforts: codexLevels, DefaultEffort: "medium"},
		}}},
		// grok keeps the session's level current even for a model without it.
		{"acp", "acp-grok", Catalog{Models: []Model{
			{ID: "grok-4.7", Label: "Grok 4.7", Efforts: []string{"xhigh", "high", "medium", "low"}, DefaultEffort: "xhigh", Default: true},
			{ID: "grok-4.5", Label: "Grok 4.5", Efforts: []string{"high", "medium", "low"}},
		}}},
		{"acp", "acp-gemini", Catalog{Models: []Model{
			{ID: "auto", Label: "Auto", Default: true},
			{ID: "gemini-3.1-pro-preview", Label: "gemini-3.1-pro-preview"},
			{ID: "gemini-3.8-flash", Label: "gemini-3.8-flash"},
		}}},
		{"pi", "pi", Catalog{Models: []Model{
			{ID: "anthropic/claude-haiku-4-5", Label: "Claude Haiku 4.5 (latest)", Efforts: []string{"off", "minimal", "low", "medium", "high"}},
			{ID: "anthropic/claude-opus-5", Label: "Claude Opus 5", Efforts: []string{"minimal", "low", "medium", "high", "xhigh", "max"}, Default: true},
			{ID: "anthropic/claude-sonnet-5", Label: "Claude Sonnet 5", Efforts: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}},
		}}},
		{"muse", "muse", Catalog{Models: []Model{
			{ID: "muse-spark-1.3", Label: "Muse Spark 1.3", Efforts: []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
			{ID: "muse-spark-1.2", Label: "Muse Spark 1.2", Default: true},
		}}},
		{"opencode", "opencode", Catalog{Models: []Model{
			{ID: "google/deep-research-max-preview-04-2026", Label: "Deep Research Max Preview (Apr-21-2026)"},
			{ID: "google/deep-research-preview-04-2026", Label: "Deep Research Preview (Apr-21-2026)"},
			{ID: "opencode/big-pickle", Label: "Big Pickle", Default: true},
			{ID: "opencode/ling-3.0-flash-fin-free", Label: "Ling 3.0 Flash Fin Free"},
		}}},
	} {
		t.Run(tc.fake, func(t *testing.T) {
			got, err := fetchFake(t, tc.kind, tc.fake)
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("catalog =\n%s\nwant\n%s", dump(got), dump(tc.want))
			}
		})
	}
}

// Hermes lists models per provider, says which reason without naming the
// levels, and scopes the list to each profile.
func TestHermesReadsProfilesAndTypedEfforts(t *testing.T) {
	got, err := fetchFake(t, "hermes", "hermes")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	models := []Model{
		{ID: "claude-fable-5.1", Provider: "anthropic", Label: "Anthropic", EffortTyped: true},
		{ID: "claude-fable-5", Provider: "anthropic", Label: "Anthropic", EffortTyped: true},
		{ID: "claude-opus-5", Provider: "anthropic", Label: "Anthropic", EffortTyped: true},
		{ID: "grok-4.6", Provider: "xai-oauth", Label: "xAI Grok OAuth (SuperGrok / Premium+)", EffortTyped: true, Default: true},
		{ID: "grok-4.7", Provider: "xai-oauth", Label: "xAI Grok OAuth (SuperGrok / Premium+)", EffortTyped: true},
		{ID: "grok-4.20-0309-non-reasoning", Provider: "xai-oauth", Label: "xAI Grok OAuth (SuperGrok / Premium+)"},
	}
	want := Catalog{Models: models, Profiles: []Profile{{Name: "default", Detail: "grok-4.6 · xai-oauth", Models: models}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog =\n%s\nwant\n%s", dump(got), dump(want))
	}
	if key := got.Models[3].Key(); key != "xai-oauth:grok-4.6" {
		t.Fatalf("key = %q", key)
	}
	scoped, err := fetchFake(t, "hermes", "hermes-work")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	work := []Model{
		{ID: "claude-fable-5.1", Provider: "anthropic", Label: "Anthropic", EffortTyped: true},
		{ID: "claude-fable-5", Provider: "anthropic", Label: "Anthropic", EffortTyped: true},
		{ID: "claude-opus-5", Provider: "anthropic", Label: "Anthropic", EffortTyped: true, Default: true},
	}
	if len(scoped.Profiles) != 2 || !reflect.DeepEqual(scoped.Profiles[1], Profile{Name: "work", Detail: "claude-opus-5 · anthropic", Models: work}) {
		t.Fatalf("profiles =\n%s", dump(scoped))
	}
}

func TestAReaderThatFailsSaysWhy(t *testing.T) {
	if _, err := fetchFake(t, "claude", "claude-refuses"); err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("refusal err = %v", err)
	}
	if _, err := fetchFake(t, "pi", "pi-0.84.2"); err == nil || !strings.Contains(err.Error(), "needs pi 0.84.3 or later") {
		t.Fatalf("old pi err = %v", err)
	}
	if _, err := fetchFake(t, "codex", "exit"); err == nil || !strings.Contains(err.Error(), "exited before answering") {
		t.Fatalf("exit err = %v", err)
	}
	if _, err := Fetch(context.Background(), "nope", "true", t.TempDir()); err == nil {
		t.Fatal("an unknown reader answered")
	}
}

// A caller leaving mid-answer stops the CLI it asked, which its own process
// group keeps out of reach of the terminal's signals.
func TestStopAllEndsACLIMidAnswer(t *testing.T) {
	t.Setenv("CATALOG_FAKE", "silent")
	done := make(chan error, 1)
	go func() {
		_, err := Fetch(context.Background(), "codex", os.Args[0], t.TempDir())
		done <- err
	}()
	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the CLI never started")
		}
		running.Lock()
		for proc := range running.procs {
			pid = proc.cmd.Process.Pid
		}
		running.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	StopAll()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a stopped CLI answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch still waits on a stopped CLI")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("process %d outlived StopAll", pid)
	}
}

// An answer stands for a while and for the binary it came from; either
// outdated, it is asked again.
func TestCachedAnswersAgeAndFollowTheBinary(t *testing.T) {
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CATALOG_TESTDATA", testdata)
	t.Setenv("CATALOG_FAKE", "acp-gemini")
	configDir := t.TempDir()
	tool := config.Tool{Catalog: "acp", CatalogCommand: os.Args[0]}
	if _, _, ok := Cached(configDir, "gemini", tool); ok {
		t.Fatal("a cache before any answer")
	}
	cat, err := Load(context.Background(), configDir, "gemini", tool)
	if err != nil || len(cat.Models) != 3 {
		t.Fatalf("Load = %+v, %v", cat, err)
	}
	if got, fresh, ok := Cached(configDir, "gemini", tool); !ok || !fresh || !reflect.DeepEqual(got, cat) {
		t.Fatalf("Cached = %+v fresh=%v ok=%v", got, fresh, ok)
	}
	path := filepath.Join(configDir, "catalogs", "gemini.json")
	rewrite := func(edit func(*cached)) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var entry cached
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatal(err)
		}
		edit(&entry)
		raw, err = json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rewrite(func(entry *cached) { entry.ReadAt = time.Now().Add(-freshFor - time.Minute) })
	if _, fresh, ok := Cached(configDir, "gemini", tool); !ok || fresh {
		t.Fatalf("an old answer reads fresh=%v ok=%v", fresh, ok)
	}
	rewrite(func(entry *cached) { entry.ReadAt = time.Now(); entry.Binary = "an older build" })
	if _, fresh, _ := Cached(configDir, "gemini", tool); fresh {
		t.Fatal("an answer from another binary reads fresh")
	}
}

func dump(cat Catalog) string {
	raw, _ := json.MarshalIndent(cat, "", " ")
	return string(raw)
}
