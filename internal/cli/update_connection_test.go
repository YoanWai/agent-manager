package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/store"
)

// scriptedHost answers the install's calls the way an outdated host
// would, then the way the installed build does.
type scriptedHost struct {
	t        *testing.T
	snapshot string
	uploaded int
	commands []string
}

func (h *scriptedHost) run(_ context.Context, argv []string, stdin io.Reader) ([]byte, []byte, error) {
	words, err := remotetest.Words(argv)
	if err != nil {
		h.t.Fatal(err)
	}
	h.commands = append(h.commands, words[0])
	switch words[0] {
	case "uname":
		return remotetest.Answer([]byte("Linux aarch64\n")), nil, nil
	case "agent-manager":
		return remotetest.Answer([]byte(h.snapshot)), nil, nil
	case "sh":
		sent, _ := io.ReadAll(stdin)
		h.uploaded = len(sent)
		h.snapshot = `{"version":1,"manager_awake":true}`
		return remotetest.Answer([]byte("/home/me/.local/bin/agent-manager\n/home/me/.local/bin/agent-manager\nyes\n")), nil, nil
	}
	h.t.Fatalf("unexpected command %q", words)
	return nil, nil, nil
}

func seedConnection(t *testing.T, host *scriptedHost) string {
	t.Helper()
	configDir := t.TempDir()
	t.Cleanup(func() { os.RemoveAll(remote.ControlDir(configDir)) })
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AddConnection(store.Connection{Name: "box", Destination: "me@box"}); err != nil {
		t.Fatal(err)
	}
	old := updateRemote
	updateRemote = func(dir string) *remote.Client {
		return remote.New(dir, remote.WithInputRunner(host.run), remote.WithFetcher(
			func(_ context.Context, tag, goos, goarch, staging string) (string, error) {
				if tag != "v0.41.0" || goos != "linux" || goarch != "arm64" {
					t.Errorf("fetched %s %s/%s", tag, goos, goarch)
				}
				binary := filepath.Join(staging, "agent-manager")
				return binary, os.WriteFile(binary, []byte("build"), 0o755)
			}))
	}
	t.Cleanup(func() { updateRemote = old })
	return configDir
}

func TestUpdateConnectionInstallsThisBuildOnAnOutdatedHost(t *testing.T) {
	host := &scriptedHost{t: t, snapshot: `{"version":0}`}
	configDir := seedConnection(t, host)
	var out bytes.Buffer
	if err := runUpdate("0.41.0")(&out, []string{"--connection=box", "--json"}, configDir); err != nil {
		t.Fatal(err)
	}
	var report connectionReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("--json printed %q: %v", out.String(), err)
	}
	if report.Connection != "box" || report.Version != "v0.41.0" || report.OS != "linux" || report.Arch != "arm64" ||
		!report.OnPath || !report.Answers || report.Advice != "" {
		t.Fatalf("report = %+v", report)
	}
	if host.uploaded != len("build") {
		t.Fatalf("the host received %d bytes", host.uploaded)
	}
}

func TestUpdateConnectionLeavesANewerHostAlone(t *testing.T) {
	host := &scriptedHost{t: t, snapshot: `{"version":2}`}
	configDir := seedConnection(t, host)
	err := runUpdate("0.41.0")(&bytes.Buffer{}, []string{"--connection=box"}, configDir)
	if err == nil || !strings.Contains(err.Error(), "newer than this one") {
		t.Fatalf("update --connection on a newer host = %v", err)
	}
	if strings.Join(host.commands, " ") != "agent-manager" {
		t.Fatalf("ran %q on a newer host, want only the snapshot", host.commands)
	}
}

func TestUpdateConnectionRefusesADevBuildAndAnUnknownName(t *testing.T) {
	host := &scriptedHost{t: t, snapshot: `{"version":0}`}
	configDir := seedConnection(t, host)
	err := runUpdate("dev")(&bytes.Buffer{}, []string{"--connection=box"}, configDir)
	if err == nil || !strings.Contains(err.Error(), "is not a release") {
		t.Fatalf("dev build = %v", err)
	}
	err = runUpdate("0.41.0")(&bytes.Buffer{}, []string{"--connection=nowhere"}, configDir)
	if err == nil || !strings.Contains(err.Error(), `no SSH connection is named "nowhere"`) {
		t.Fatalf("unknown connection = %v", err)
	}
	if len(host.commands) != 0 {
		t.Fatalf("a refusal still ran %q", host.commands)
	}
}
