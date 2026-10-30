package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestCreateActiveSessionValidatesResolvedPlacement(t *testing.T) {
	for _, placement := range []string{"group", "ancestor", "parent-group", "parent", "anchor-group", "anchor", "session", "active-parent", "active-anchor"} {
		t.Run(placement, func(t *testing.T) {
			st := newTestStore(t)
			if err := st.CreateGroup("closed", ""); err != nil {
				t.Fatal(err)
			}
			if err := st.SetGroupArchived("closed", true); err != nil {
				t.Fatal(err)
			}
			sess := sample("new", "")
			var anchor string
			switch placement {
			case "group":
				sess.Group = "closed"
			case "ancestor":
				sess.Group = "closed/child"
			case "session":
				sess.Archived = true
			default:
				reference := sample("reference", "active")
				if placement == "parent-group" || placement == "anchor-group" {
					reference.Group = "closed"
				}
				if placement == "parent" || placement == "anchor" {
					reference.Archived = true
				}
				if err := st.CreateSession(reference); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(placement, "parent") {
					sess.ParentID = reference.ID
				} else {
					anchor = reference.ID
				}
			}
			create := func(row Session) error { return st.CreateActiveSession(row) }
			if anchor != "" {
				create = func(row Session) error { return st.CreateActiveSessionBeside(row, anchor) }
			}
			err := create(sess)
			if strings.HasPrefix(placement, "active-") {
				if err != nil {
					t.Fatal(err)
				}
				stored, err := st.Get(sess.ID)
				if err != nil || stored.Group != "active" {
					t.Fatalf("resolved active row=%+v error=%v", stored, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "archived") {
				t.Fatalf("creation error=%v, want archived placement refusal", err)
			}
			if _, err := st.Get(sess.ID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("refused row persisted: %v", err)
			}
		})
	}
}

func TestCreateActiveSessionKeepsAConversationOnOneRow(t *testing.T) {
	st := newTestStore(t)
	first := sample("first", "")
	first.Tool, first.AgentSessionID = "codex", "conv-1"
	if err := st.CreateActiveSession(first); err != nil {
		t.Fatal(err)
	}
	second := sample("second", "")
	second.Tool, second.AgentSessionID = "codex", "conv-1"
	if err := st.CreateActiveSession(second); err == nil || !strings.Contains(err.Error(), "already session first") {
		t.Fatalf("second row on the same conversation = %v", err)
	}
	other := sample("other", "")
	other.Tool, other.AgentSessionID = "claude", "conv-1"
	if err := st.CreateActiveSession(other); err != nil {
		t.Fatalf("another CLI's conversation may share the id: %v", err)
	}
	for _, id := range []string{"fresh-a", "fresh-b"} {
		fresh := sample(id, "")
		fresh.Tool = "codex"
		if err := st.CreateActiveSession(fresh); err != nil {
			t.Fatalf("rows with no conversation yet: %v", err)
		}
	}
}
