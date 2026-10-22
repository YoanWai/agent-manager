package agentsession

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeOpencodeFork puts an opencode on PATH whose api call answers with the
// given body.
func fakeOpencodeFork(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the opencode stand-in is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ntest \"$1\" = api || exit 2\nprintf '%s\\n' '" + body + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestForkOpensTheCopyOpencodeNames(t *testing.T) {
	fakeOpencodeFork(t, `{"data":{"id":"ses_copy1"}}`)
	id, err := Fork("opencode", t.TempDir(), "ses_source")
	if err != nil || id != "ses_copy1" {
		t.Fatalf("Fork = %q, %v; want ses_copy1", id, err)
	}
	if _, err := Fork("codex", t.TempDir(), "source"); err == nil {
		t.Fatal("a store that makes no fork answered one")
	}
}

func TestForkRefusesAnAnswerThatNamesNoSession(t *testing.T) {
	for _, body := range []string{`{"data":{"id":"ses_x; rm -rf /"}}`, `{"data":{}}`, `<!doctype html>`} {
		fakeOpencodeFork(t, body)
		if id, err := Fork("opencode", t.TempDir(), "ses_source"); err == nil || !strings.Contains(err.Error(), "ses_source") {
			t.Fatalf("Fork on %q = %q, %v; want a refusal naming the source", body, id, err)
		}
	}
}
