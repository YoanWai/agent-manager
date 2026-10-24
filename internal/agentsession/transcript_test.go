package agentsession

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi 1.1.0 and omp 18.8.8 session files, captured after two prompts typed
// into each pane. The pi system message carries its sections of the
// system prompt in the live file; the fixture keeps the record without
// them, since its content is empty and it is stepped over either way.
func copyPiFixture(t *testing.T, tool, root, cwd string) string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("testdata", tool, "*.jsonl"))
	if err != nil || len(names) != 1 {
		t.Fatalf("fixture for %s: %v %v", tool, names, err)
	}
	data, err := os.ReadFile(names[0])
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, piCwdKey(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.Base(names[0]))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPiTranscriptPathAndTail(t *testing.T) {
	for _, tc := range []struct{ tool, id string }{
		{"pi", "019a1b2c-3d4e-7f60-8a9b-0c1d2e3f4a5b"},
		{"omp", "01a12b27-3455-75b7-80ac-db738fedb8b6"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			want := copyPiFixture(t, tc.tool, root, cwd)
			path, err := piTranscriptPath(root, cwd, tc.id)
			if err != nil || path != want {
				t.Fatalf("path = %q err=%v, want %q", path, err, want)
			}
			if _, err := piTranscriptPath(root, cwd, "other-id"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a session the directory holds no file for: err=%v, want ErrNotExist", err)
			}
			if _, err := piTranscriptPath(root, t.TempDir(), tc.id); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a directory the tool never ran in: err=%v, want ErrNotExist", err)
			}
			prompt, reply, ok := TranscriptTail(tc.tool, path, func(text string) string { return text })
			if !ok || prompt != "Now reply with exactly the word YES." || reply != "YES" {
				t.Fatalf("tail = %q / %q ok=%v", prompt, reply, ok)
			}
			skipNewest := func(text string) string {
				if strings.HasPrefix(text, "Now reply") {
					return ""
				}
				return text
			}
			if prompt, _, _ := TranscriptTail(tc.tool, path, skipNewest); prompt != "Reply with exactly the word OK." {
				t.Fatalf("prompt = %q, want the newest kept user text", prompt)
			}
		})
	}
}

// omp files the steering it injects as user messages attributed to the
// agent, and marks synthetic ones; neither is a prompt anyone typed.
func TestPiTranscriptTailSkipsAgentAuthoredUserMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"typed prompt"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"the reply"}]}}`,
		`{"type":"message","message":{"role":"user","attribution":"agent","content":[{"type":"text","text":"steering"}]}}`,
		`{"type":"message","message":{"role":"user","synthetic":true,"content":[{"type":"text","text":"synthetic"}]}}`,
		`{"type":"custom","customType":"x","data":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, reply, ok := piTranscriptTail(path, func(text string) string { return text })
	if !ok || prompt != "typed prompt" || reply != "the reply" {
		t.Fatalf("tail = %q / %q ok=%v", prompt, reply, ok)
	}
	if _, _, ok := piTranscriptTail(filepath.Join(t.TempDir(), "missing.jsonl"), func(text string) string { return text }); ok {
		t.Fatal("a missing file should not read ok")
	}
}

func TestTranscriptPathPerStyle(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	for _, style := range []string{"claude", "pi", "omp"} {
		if !KeepsTranscript(style) {
			t.Fatalf("%s keeps a transcript", style)
		}
	}
	if KeepsTranscript("codex") {
		t.Fatal("codex has no transcript reader")
	}
	if _, err := TranscriptPath("codex", cwd, "id"); !errors.Is(err, ErrNoTranscript) {
		t.Fatalf("codex: err=%v, want ErrNoTranscript", err)
	}
	piPath := copyPiFixture(t, "pi", piRoot(), cwd)
	if path, err := TranscriptPath("pi", cwd, "019a1b2c-3d4e-7f60-8a9b-0c1d2e3f4a5b"); err != nil || path != piPath {
		t.Fatalf("pi path = %q err=%v, want %q", path, err, piPath)
	}
	ompPath := copyPiFixture(t, "omp", ompRoot(), cwd)
	if path, err := TranscriptPath("omp", cwd, "01a12b27-3455-75b7-80ac-db738fedb8b6"); err != nil || path != ompPath {
		t.Fatalf("omp path = %q err=%v, want %q", path, err, ompPath)
	}
	if _, err := TranscriptPath("claude", cwd, "abc"); err != nil {
		t.Fatalf("claude path: %v", err)
	}
}
