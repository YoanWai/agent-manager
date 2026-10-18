package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// outdatedHost answers like a host whose agent-manager predates the
// snapshot until an upload lands, and like this build afterwards.
type outdatedHost struct {
	mu        sync.Mutex
	installed bool
	uploaded  []byte
	commands  []string
}

func (h *outdatedHost) run(_ context.Context, argv []string, stdin io.Reader) ([]byte, []byte, error) {
	words, _ := remotetest.Words(argv)
	h.mu.Lock()
	defer h.mu.Unlock()
	name := words[0]
	if name == "agent-manager" {
		name = words[1]
	}
	h.commands = append(h.commands, name)
	switch name {
	case "uname":
		return remotetest.Answer([]byte("Linux x86_64\n")), nil, nil
	case "sh":
		h.uploaded, _ = io.ReadAll(stdin)
		h.installed = true
		path := "/home/me/.local/bin/agent-manager"
		return remotetest.Answer([]byte(path + "\n" + path + "\nyes\n")), nil, nil
	case "snapshot":
		snapshot := sessioncmd.Snapshot{Version: 0}
		if h.installed {
			snapshot = boxSnapshot()
		}
		out, err := json.Marshal(snapshot)
		return remotetest.Answer(out), nil, err
	case "serve":
		return remotetest.Answer([]byte(`{"started":false}`)), nil, nil
	}
	return nil, []byte("unexpected " + strings.Join(words, " ")), fakeExit(2)
}

func (h *outdatedHost) ran() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.commands)
}

// outdatedModel is a release build whose one connection, box, answered its
// first refresh with a snapshot too old to read.
func outdatedModel(t *testing.T, version string, fetchErr error) (*Model, *outdatedHost) {
	t.Helper()
	m := buildModel(t)
	m.update.version = version
	host := &outdatedHost{}
	fetch := func(_ context.Context, tag, goos, goarch, dir string) (string, error) {
		if fetchErr != nil {
			return "", fetchErr
		}
		if tag != "v"+version || goos != "linux" || goarch != "amd64" {
			t.Errorf("fetched %s %s/%s", tag, goos, goarch)
		}
		binary := filepath.Join(dir, "agent-manager")
		return binary, os.WriteFile(binary, []byte("release build"), 0o755)
	}
	m.ssh = newConnections(testRemoteClient(t, remote.WithInputRunner(host.run), remote.WithFetcher(fetch)),
		[]store.Connection{{Name: "box", Destination: "me@box"}})
	m.rebuildRows()
	m.applyTestMsg(t, m.pollConnection("box")())
	m.rail.Focus(boxRow)
	return m, host
}

func legendHas(section legendSection, key, label string) bool {
	return slices.Contains(section.pairs, [2]string{key, label})
}

func TestOutdatedConnectionInstallsThroughTheConfirmAndComesBackOnline(t *testing.T) {
	m, host := outdatedModel(t, "0.41.0", nil)
	if state := m.ssh.hosts["box"]; state.OK || !remote.Installable(state.Err) {
		t.Fatalf("outdated host state = %+v", state)
	}
	if rail := m.ssh.railHosts()[0]; !rail.Installable || rail.State != uirail.HostOffline {
		t.Fatalf("rail host = %+v, want an installable offline host", rail)
	}
	if !legendHas(m.remoteRowLegend(), "u", "install agent-manager") {
		t.Fatalf("footer = %+v, want u install agent-manager", m.remoteRowLegend().pairs)
	}
	if fact, _ := m.installFact("box"); fact != "press u to install agent-manager v0.41.0 there" {
		t.Fatalf("detail = %q", fact)
	}

	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Restore, Target: boxRow})
	if m.mode != modeConfirmDelete || !strings.HasPrefix(m.confirm.label, "install agent-manager v0.41.0 on box?") {
		t.Fatalf("mode %v label %q, want the install confirm", m.mode, m.confirm.label)
	}
	if got := m.viewConfirm(); !strings.Contains(got, "Install agent-manager") || !strings.Contains(got, "install") {
		t.Fatalf("confirm card = %q", got)
	}
	m.runRemoteCmd(t, cmd)
	if ran := host.ran(); !slices.Equal(ran, []string{"snapshot"}) {
		t.Fatalf("calls before the answer = %q", ran)
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !m.ssh.installing["box"] || m.ssh.railHosts()[0].Installing != true {
		t.Fatal("the row does not show the install running")
	}
	m.runRemoteCmd(t, cmd)

	if ran := host.ran(); !slices.Equal(ran, []string{"snapshot", "uname", "sh", "snapshot"}) {
		t.Fatalf("calls = %q, want uname, the upload, then a fresh snapshot", ran)
	}
	if string(host.uploaded) != "release build" {
		t.Fatalf("uploaded %q", host.uploaded)
	}
	if state := m.ssh.hosts["box"]; !state.OK || m.ssh.installing["box"] {
		t.Fatalf("after the install: state %+v installing %v", state, m.ssh.installing)
	}
	if m.errBar.text != "installed agent-manager v0.41.0 on box" {
		t.Fatalf("status = %q", m.errBar.text)
	}
	if names := railNames(m); !slices.Contains(names, "api") {
		t.Fatalf("rows = %q, want the host's sessions back", names)
	}
}

func TestInstallFromADevBuildRefusesWithoutAsking(t *testing.T) {
	m, host := outdatedModel(t, "dev", nil)
	if fact, _ := m.installFact("box"); fact != "this build is not a release; install agent-manager there by hand" {
		t.Fatalf("detail = %q", fact)
	}
	m.runRailIntent(uirail.Intent{Kind: uirail.Restore, Target: boxRow})
	if m.mode != modeList || !strings.Contains(m.errBar.text, "is not a release") {
		t.Fatalf("mode %v status %q, want the dev-build refusal", m.mode, m.errBar.text)
	}
	if ran := host.ran(); !slices.Equal(ran, []string{"snapshot"}) {
		t.Fatalf("a dev build still called %q", ran)
	}
}

func TestFailedInstallLeavesTheHostAndSaysWhy(t *testing.T) {
	m, host := outdatedModel(t, "0.41.0", errors.New("update: checksum mismatch: got 00, want ff"))
	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Restore, Target: boxRow})
	m.runRemoteCmd(t, cmd)
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.runRemoteCmd(t, cmd)
	if ran := host.ran(); slices.Contains(ran, "sh") {
		t.Fatalf("a mismatched download was uploaded: %q", ran)
	}
	if !strings.HasPrefix(m.errBar.text, "install failed: box: download v0.41.0 for linux/amd64: ") || !strings.Contains(m.errBar.text, "checksum mismatch") {
		t.Fatalf("status = %q", m.errBar.text)
	}
	if fact, _ := m.installFact("box"); !strings.HasPrefix(fact, "the last install failed (") || !m.ssh.installable("box") {
		t.Fatalf("detail = %q, want the failure and the offer again", fact)
	}
}

func TestRestoreOnAnOnlineConnectionIsStillRefused(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxRow)
	if legendHas(m.remoteRowLegend(), "u", "install agent-manager") {
		t.Fatal("an online connection offers an install")
	}
	m.runRailIntent(uirail.Intent{Kind: uirail.Restore, Target: boxRow})
	if m.mode != modeList || m.errBar.text != uirail.RemoteRefusal("restore") || len(fake.taken()) != 0 {
		t.Fatalf("mode %v status %q calls %q", m.mode, m.errBar.text, fake.taken())
	}
}
