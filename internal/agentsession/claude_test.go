package agentsession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeProjectSlug(t *testing.T) {
	if got := claudeProjectSlug("/private/tmp/am_work.dir"); got != "-private-tmp-am-work-dir" {
		t.Fatalf("slug = %q", got)
	}
}

// The tail reader returns the newest kept user text and the newest
// assistant text block, stepping over tool results, sidechains and meta
// entries.
func TestClaudeTranscriptTail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(resolvePath(cwd)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"first prompt"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"First reply."}]}}`,
		`{"type":"user","message":{"role":"user","content":"manager directive to skip"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ignored"}]}}`,
		`{"type":"user","isSidechain":true,"message":{"role":"user","content":"sidechain prompt"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Second reply.\nIt spans lines."}]}}`,
	}
	path := filepath.Join(dir, "abc.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := func(text string) string {
		if strings.HasPrefix(text, "manager directive") {
			return ""
		}
		return text
	}
	prompt, reply, ok := ClaudeTranscriptTail(cwd, "abc", keep)
	if !ok {
		t.Fatal("transcript should be readable")
	}
	if prompt != "first prompt" {
		t.Fatalf("prompt = %q, want the newest kept user text", prompt)
	}
	if reply != "Second reply.\nIt spans lines." {
		t.Fatalf("reply = %q, want the newest text block", reply)
	}
	if _, _, ok := ClaudeTranscriptTail(cwd, "missing", keep); ok {
		t.Fatal("missing transcript should not read ok")
	}
}
