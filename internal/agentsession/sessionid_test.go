package agentsession

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSessionIDPatternAcceptsTheShapesStoresUse(t *testing.T) {
	for _, id := range []string{
		"019a4b0e-39e1-7261-b801-e64f2d0e97bd",
		"4b53d997-3d0b-4d89-a5d0-39573e4588f1",
		"ses_8QbCd3Ef",
		"ours-uuid",
	} {
		if !sessionIDPattern.MatchString(id) {
			t.Errorf("a real id was refused: %q", id)
		}
	}
	for _, id := range []string{
		"",
		"abc; touch pwned",
		`abc'; touch pwned; echo '`,
		"abc$(touch pwned)",
		"abc\ntouch pwned",
		"../../etc/passwd",
		"-rf",
	} {
		if sessionIDPattern.MatchString(id) {
			t.Errorf("an id that is not a plain token was accepted: %q", id)
		}
	}
}

// A rollout is a file any process can write, so an id spelling a command
// must be left alone. The conversation we really launched still has to
// bind, or one planted file would end id capture for that directory.
func TestCaptureCodexLeavesAnIDThatIsNotAPlainToken(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	writeFile(t, filepath.Join(root, "2026/07/18/rollout-planted.jsonl"),
		codexRollout(`abc; touch pwned`, "/repo"), launch.Add(time.Second))
	writeFile(t, filepath.Join(root, "2026/07/18/rollout-ours.jsonl"),
		codexRollout("ours-uuid", "/repo"), launch.Add(2*time.Second))

	id, ok := captureCodex(root, "/repo", launch, map[string]bool{})
	if !ok || id != "ours-uuid" {
		t.Fatalf("got id=%q ok=%v, want ours-uuid true", id, ok)
	}
}

func TestCaptureGeminiLeavesAnIDThatIsNotAPlainToken(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	hash := geminiProjectHash("/repo")
	writeFile(t, filepath.Join(root, "proj/chats/session-1-planted.jsonl"),
		geminiSessionFixture(`abc; touch pwned`, hash), launch.Add(time.Second))
	writeFile(t, filepath.Join(root, "proj/chats/session-2-ours.jsonl"),
		geminiSessionFixture("ours-uuid", hash), launch.Add(2*time.Second))

	id, ok := captureGemini(root, "/repo", launch, map[string]bool{})
	if !ok || id != "ours-uuid" {
		t.Fatalf("got id=%q ok=%v, want ours-uuid true", id, ok)
	}
}
