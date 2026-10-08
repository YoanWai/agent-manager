package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparedAttachDoesNotReplaceNewerHelp(t *testing.T) {
	for _, returning := range []bool{false, true} {
		t.Run(map[bool]string{false: "attach", true: "reattach"}[returning], func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "attach-fence", t.TempDir(), "")
			m.selectSessionRow(t, "attach-fence")
			sess, _ := m.selected()
			if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
				t.Fatal(err)
			}
			var command tea.Cmd
			if returning {
				command = m.reattach(sess.ID, m.review.Generation())
			} else {
				command = m.attachCmd(sess.ID)
			}
			request := m.effects.main.active.request.(attachRequest)
			result := command().(effectCompletedMsg)
			if result.err != nil {
				t.Fatal(result.err)
			}
			got, err := m.services.store.Get(sess.ID)
			if err != nil || !got.Acked {
				t.Fatalf("accepted acknowledgement was lost: %+v %v", got, err)
			}
			m.gens.foreground++
			m.openHelp()
			if next := m.applyAttachEffect(request, result.result.(attachEffectResult), result.err); next != nil {
				t.Fatal("prepared attach replaced newer Help")
			}
			if m.mode != modeHelp {
				t.Fatal("Help was replaced")
			}
		})
	}
}

func TestAttachReportsTransportFailureWithoutCallingPaneDead(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	stub := filepath.Join(dir, "tmux")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'attach liveness unavailable' >&2\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	services := effectServices{store: m.services.store, driver: driver}
	_, err = services.runAttach(attachRequest{id: "attach-probe"})
	if err == nil || !strings.Contains(err.Error(), "attach liveness unavailable") || strings.Contains(err.Error(), deadSessionHint) {
		t.Fatalf("transport failure mislabeled: %v", err)
	}
}
