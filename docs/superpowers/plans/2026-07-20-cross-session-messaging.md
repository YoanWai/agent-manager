# Cross-Session Awareness and Messaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let every agent session managed by agent-manager list sibling sessions, read their screens, and hold two-way async conversations with them, with no per-user install.

**Architecture:** Five new subcommands on the existing `agent-manager` binary live in a new `internal/cli` package with injectable dependencies so they unit-test without tmux. Messages persist in a new `messages` table in the existing sqlite database, shared between the manager process and subcommand processes via WAL plus `busy_timeout`. Sending stores a row and then attempts live injection into the target's tmux pane; the manager's poller redelivers anything that missed. Sessions learn the feature exists from a primer line appended to the launch directive that already teaches them `agent-manager rename`.

**Tech Stack:** Go 1.26.5, modernc.org/sqlite, tmux, charmbracelet/x/ansi, stdlib `text/tabwriter`.

## Global Constraints

- Go 1.26.5 (`go.mod`). No new third-party dependencies.
- Injected pane text MUST be a single line. `tmux send-keys -l` followed by `Enter` submits on newline, so a multi-line body would submit partially. All bodies are whitespace-squashed to one line before injection.
- Message bodies are capped at 2000 characters after squashing.
- The manager process remains the sole writer of the `sessions`, `groups`, and `settings` tables. Only the `messages` table is written by both the manager and subcommand processes.
- Zero code comments unless a non-obvious WHY needs recording; the codebase comments sparingly and explains rationale, never mechanics.
- No `Co-Authored-By` or AI-attribution lines in commits.
- Session ids are lowercase hex (`^[0-9a-f]+$`), enforced already by `sessionIDPattern` in `main.go:47`.
- Work in an isolated git worktree. Concurrent agent-manager sessions share this checkout, and edits made directly on `main` get swept into other sessions' PRs.

---

### Task 1: Message storage

**Files:**
- Modify: `internal/store/store.go` (init schema ~line 55-99, `Delete` at line 286)
- Create: `internal/store/message.go`
- Test: `internal/store/message_test.go`

**Interfaces:**
- Consumes: existing `Store`, `encodeTime`, `decodeTime`, `boolToInt` helpers in `internal/store`.
- Produces:
  - `type Message struct { ID int64; From, To, Body string; CreatedAt time.Time; Delivered, Seen bool }`
  - `func (s *Store) InsertMessage(from, to, body string) (int64, error)`
  - `func (s *Store) Message(id int64) (Message, error)`
  - `func (s *Store) UnreadMessages(to string) ([]Message, error)`
  - `func (s *Store) MessagesFor(to string) ([]Message, error)`
  - `func (s *Store) UndeliveredFor(to string) ([]Message, error)`
  - `func (s *Store) MarkDelivered(id int64) error`
  - `func (s *Store) MarkSeen(ids []int64) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/store/message_test.go`:

```go
package store

import "testing"

func TestInsertAndReadMessage(t *testing.T) {
	st := newTestStore(t)
	id, err := st.InsertMessage("aaa", "bbb", "are you done with auth?")
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	if id == 0 {
		t.Fatal("want non-zero message id")
	}
	msg, err := st.Message(id)
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if msg.From != "aaa" || msg.To != "bbb" || msg.Body != "are you done with auth?" {
		t.Fatalf("round trip mismatch: %+v", msg)
	}
	if msg.Delivered || msg.Seen {
		t.Fatalf("new message should be undelivered and unseen: %+v", msg)
	}
	if msg.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}
}

func TestUnreadAndMarkSeen(t *testing.T) {
	st := newTestStore(t)
	first, _ := st.InsertMessage("aaa", "bbb", "one")
	second, _ := st.InsertMessage("aaa", "bbb", "two")
	if _, err := st.InsertMessage("aaa", "ccc", "other inbox"); err != nil {
		t.Fatal(err)
	}

	unread, err := st.UnreadMessages("bbb")
	if err != nil {
		t.Fatalf("UnreadMessages: %v", err)
	}
	if len(unread) != 2 {
		t.Fatalf("want 2 unread, got %d", len(unread))
	}
	if unread[0].ID != first || unread[1].ID != second {
		t.Fatalf("want oldest first, got %d then %d", unread[0].ID, unread[1].ID)
	}

	if err := st.MarkSeen([]int64{first, second}); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	unread, _ = st.UnreadMessages("bbb")
	if len(unread) != 0 {
		t.Fatalf("want 0 unread after MarkSeen, got %d", len(unread))
	}
	all, err := st.MessagesFor("bbb")
	if err != nil {
		t.Fatalf("MessagesFor: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 total, got %d", len(all))
	}
}

func TestUndeliveredForAndMarkDelivered(t *testing.T) {
	st := newTestStore(t)
	id, _ := st.InsertMessage("aaa", "bbb", "hello")

	pending, err := st.UndeliveredFor("bbb")
	if err != nil {
		t.Fatalf("UndeliveredFor: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("want the new message pending, got %+v", pending)
	}

	if err := st.MarkDelivered(id); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	pending, _ = st.UndeliveredFor("bbb")
	if len(pending) != 0 {
		t.Fatalf("want nothing pending after delivery, got %d", len(pending))
	}
	// Delivery must not imply the agent read it.
	unread, _ := st.UnreadMessages("bbb")
	if len(unread) != 1 {
		t.Fatalf("delivered message should still be unread, got %d", len(unread))
	}
}

func TestMarkSeenEmptyIsNoop(t *testing.T) {
	st := newTestStore(t)
	if err := st.MarkSeen(nil); err != nil {
		t.Fatalf("MarkSeen(nil): %v", err)
	}
}

func TestMessageMissingID(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Message(404); err == nil {
		t.Fatal("want error for unknown message id")
	}
}

func TestDeleteSessionRemovesItsMessages(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("aaa", "g1")); err != nil {
		t.Fatal(err)
	}
	sent, _ := st.InsertMessage("aaa", "bbb", "outgoing")
	received, _ := st.InsertMessage("ccc", "aaa", "incoming")

	if err := st.Delete("aaa"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Message(sent); err == nil {
		t.Fatal("sent message should be gone")
	}
	if _, err := st.Message(received); err == nil {
		t.Fatal("received message should be gone")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'Message|Unread|Undelivered|DeleteSessionRemoves' -v`
Expected: FAIL to compile with `st.InsertMessage undefined`.

- [ ] **Step 3: Add the schema**

In `internal/store/store.go`, inside `init()`, append to the `CREATE TABLE` block passed to `s.db.Exec` (after the `settings` table definition, before the closing backtick):

```sql
CREATE TABLE IF NOT EXISTS messages (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	from_session TEXT NOT NULL,
	to_session   TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	delivered    INTEGER NOT NULL DEFAULT 0,
	seen         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_to_idx ON messages (to_session);
```

- [ ] **Step 4: Add busy_timeout so two processes can share the database**

In `internal/store/store.go`, inside `Open`, directly after the existing `PRAGMA journal_mode=WAL` block, add:

```go
	if _, err := db.Exec("PRAGMA busy_timeout=3000"); err != nil {
		db.Close()
		return nil, err
	}
```

- [ ] **Step 5: Create the message accessors**

Create `internal/store/message.go`:

```go
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Message struct {
	ID        int64
	From      string
	To        string
	Body      string
	CreatedAt time.Time
	Delivered bool
	Seen      bool
}

func (s *Store) InsertMessage(from, to, body string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO messages (from_session, to_session, body, created_at)
		 VALUES (?, ?, ?, ?)`,
		from, to, body, encodeTime(time.Now()),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const messageColumns = `id, from_session, to_session, body, created_at, delivered, seen`

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var msg Message
	var created int64
	var delivered, seen int
	if err := row.Scan(&msg.ID, &msg.From, &msg.To, &msg.Body, &created, &delivered, &seen); err != nil {
		return Message{}, err
	}
	msg.CreatedAt = decodeTime(created)
	msg.Delivered = delivered != 0
	msg.Seen = seen != 0
	return msg, nil
}

func (s *Store) Message(id int64) (Message, error) {
	row := s.db.QueryRow(`SELECT `+messageColumns+` FROM messages WHERE id = ?`, id)
	msg, err := scanMessage(row)
	if err == sql.ErrNoRows {
		return Message{}, fmt.Errorf("no message #%d", id)
	}
	return msg, err
}

func (s *Store) queryMessages(where string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+messageColumns+` FROM messages WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *Store) UnreadMessages(to string) ([]Message, error) {
	return s.queryMessages(`to_session = ? AND seen = 0`, to)
}

func (s *Store) MessagesFor(to string) ([]Message, error) {
	return s.queryMessages(`to_session = ?`, to)
}

func (s *Store) UndeliveredFor(to string) ([]Message, error) {
	return s.queryMessages(`to_session = ? AND delivered = 0`, to)
}

func (s *Store) MarkDelivered(id int64) error {
	_, err := s.db.Exec(`UPDATE messages SET delivered = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) MarkSeen(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	_, err := s.db.Exec(`UPDATE messages SET seen = 1 WHERE id IN (`+placeholders+`)`, args...)
	return err
}
```

- [ ] **Step 6: Delete a session's messages with the session**

In `internal/store/store.go`, replace the body of `Delete` (line 286) with:

```go
func (s *Store) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM messages WHERE from_session = ? OR to_session = ?`, id, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS, including the pre-existing store tests.

- [ ] **Step 8: Commit**

```bash
git add internal/store/message.go internal/store/message_test.go internal/store/store.go
git commit -m "feat: store cross-session messages"
```

---

### Task 2: Message text formatting

**Files:**
- Create: `internal/message/message.go`
- Test: `internal/message/message_test.go`

**Interfaces:**
- Consumes: `store.Message` from Task 1.
- Produces:
  - `const MaxBody = 2000`
  - `func Squash(body string) string` — collapses all whitespace runs to single spaces, trims, truncates to `MaxBody` runes.
  - `func Inject(msg store.Message, senderName string) string` — the single-line text injected into a target's pane.

Rationale for a separate package: both `internal/cli` (sending) and `internal/ui` (poller redelivery) need identical injection text, and `internal/ui` must not import `internal/cli`.

- [ ] **Step 1: Write the failing tests**

Create `internal/message/message_test.go`:

```go
package message

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSquashCollapsesWhitespace(t *testing.T) {
	got := Squash("  are you\n\tdone   with auth?  ")
	want := "are you done with auth?"
	if got != want {
		t.Fatalf("Squash = %q, want %q", got, want)
	}
}

func TestSquashTruncates(t *testing.T) {
	got := Squash(strings.Repeat("x", MaxBody+50))
	if len([]rune(got)) != MaxBody {
		t.Fatalf("want %d runes, got %d", MaxBody, len([]rune(got)))
	}
}

func TestInjectIsSingleLineAndSelfDescribing(t *testing.T) {
	msg := store.Message{ID: 7, From: "a1b2c3", To: "d4e5f6", Body: "are you done with auth?"}
	got := Inject(msg, "auth-refactor")

	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("injected text must be one line, got %q", got)
	}
	for _, want := range []string{"#7", "auth-refactor", "a1b2c3", "are you done with auth?", `agent-manager reply 7`, "continue"} {
		if !strings.Contains(got, want) {
			t.Fatalf("injected text %q missing %q", got, want)
		}
	}
}

func TestInjectSquashesMultilineBody(t *testing.T) {
	msg := store.Message{ID: 1, From: "aaa", Body: "line one\nline two"}
	got := Inject(msg, "sender")
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("want single line, got %q", got)
	}
	if !strings.Contains(got, "line one line two") {
		t.Fatalf("want squashed body in %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/message/ -v`
Expected: FAIL, `no required module provides package .../internal/message`.

- [ ] **Step 3: Write the implementation**

Create `internal/message/message.go`:

```go
package message

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

const MaxBody = 2000

// Squash flattens a body to one bounded line. tmux send-keys submits on
// every newline, so a multi-line body would reach the target agent as
// several partial messages.
func Squash(body string) string {
	flat := strings.Join(strings.Fields(body), " ")
	if runes := []rune(flat); len(runes) > MaxBody {
		return string(runes[:MaxBody])
	}
	return flat
}

// Inject renders the text typed into a target session's pane. It names
// the sender, carries the reply command so an agent that never saw the
// launch primer still knows how to answer, and tells the agent to resume
// its prior work so a message never derails a turn.
func Inject(msg store.Message, senderName string) string {
	return fmt.Sprintf(
		"[agent-manager message #%d from %s (%s)]: %s | Reply with: agent-manager reply %d \"<answer>\" -- then continue your prior task.",
		msg.ID, senderName, msg.From, Squash(msg.Body), msg.ID,
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/message/ -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/message/
git commit -m "feat: format cross-session message injection text"
```

---

### Task 3: CLI package skeleton, target resolution, and `sessions`

**Files:**
- Create: `internal/cli/cli.go`
- Create: `internal/cli/sessions.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `store.Store`, `store.Session` from Task 1.
- Produces:
  - `type Tmux interface { SendText(id, text string) error; CapturePane(id string) (string, error); Exists(id string) bool }`
  - `type Deps struct { Store *store.Store; Tmux Tmux; Caller string; Out io.Writer }`
  - `func (d Deps) requireCaller() error`
  - `func resolveTarget(sessions []store.Session, query string) (store.Session, error)`
  - `func Sessions(d Deps) error`

`*tmux.Driver` already satisfies `Tmux` with its existing `SendText`, `CapturePane`, and `Exists` methods; no change to `internal/tmux` is needed.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cli_test.go`:

```go
package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

type fakeTmux struct {
	sent    map[string][]string
	panes   map[string]string
	missing map[string]bool
	err     error
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{sent: map[string][]string{}, panes: map[string]string{}, missing: map[string]bool{}}
}

func (f *fakeTmux) SendText(id, text string) error {
	if f.err != nil {
		return f.err
	}
	if f.missing[id] {
		return errNoSession
	}
	f.sent[id] = append(f.sent[id], text)
	return nil
}

func (f *fakeTmux) CapturePane(id string) (string, error) {
	if f.missing[id] {
		return "", errNoSession
	}
	return f.panes[id], nil
}

func (f *fakeTmux) Exists(id string) bool { return !f.missing[id] }

func newTestDeps(t *testing.T, caller string) (Deps, *fakeTmux, *bytes.Buffer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	tm := newFakeTmux()
	out := &bytes.Buffer{}
	return Deps{Store: st, Tmux: tm, Caller: caller, Out: out}, tm, out
}

func seed(t *testing.T, d Deps, id, name string) {
	t.Helper()
	sess := store.Session{ID: id, Name: name, Tool: "claude", Cwd: "/tmp/" + name, Group: "", Status: "idle"}
	if err := d.Store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

func TestResolveTarget(t *testing.T) {
	sessions := []store.Session{
		{ID: "a1b2c3", Name: "auth-refactor"},
		{ID: "a9f000", Name: "API Docs"},
		{ID: "d4e5f6", Name: "auth-refactor"},
	}
	cases := []struct {
		label  string
		query  string
		wantID string
		wantEr bool
	}{
		{"exact id", "a1b2c3", "a1b2c3", false},
		{"exact name case insensitive", "api docs", "a9f000", false},
		{"unique id prefix", "d4", "d4e5f6", false},
		{"ambiguous id prefix", "a", "", true},
		{"ambiguous name", "auth-refactor", "", true},
		{"unknown", "zzz", "", true},
		{"empty", "", "", true},
	}
	for _, c := range cases {
		got, err := resolveTarget(sessions, c.query)
		if c.wantEr {
			if err == nil {
				t.Fatalf("%s: want error, got %q", c.label, got.ID)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.label, err)
		}
		if got.ID != c.wantID {
			t.Fatalf("%s: got %q want %q", c.label, got.ID, c.wantID)
		}
	}
}

func TestSessionsListsAndMarksSelf(t *testing.T) {
	d, _, out := newTestDeps(t, "a1b2c3")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")

	if err := Sessions(d); err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	text := out.String()
	for _, want := range []string{"a1b2c3", "auth-refactor", "d4e5f6", "docs-pass", "claude", "idle"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q missing %q", text, want)
		}
	}
	selfLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "a1b2c3") {
			selfLine = line
		}
	}
	if !strings.Contains(selfLine, "(you)") {
		t.Fatalf("caller's row should be marked, got %q", selfLine)
	}
}

func TestSessionsWorksWithoutCaller(t *testing.T) {
	d, _, out := newTestDeps(t, "")
	seed(t, d, "a1b2c3", "auth-refactor")
	if err := Sessions(d); err != nil {
		t.Fatalf("Sessions outside a managed session should work: %v", err)
	}
	if !strings.Contains(out.String(), "auth-refactor") {
		t.Fatal("want the session listed")
	}
}

func TestRequireCaller(t *testing.T) {
	d, _, _ := newTestDeps(t, "")
	if err := d.requireCaller(); err == nil {
		t.Fatal("want error when AGENT_MANAGER_SESSION_ID is unset")
	}
	d.Caller = "a1b2c3"
	if err := d.requireCaller(); err != nil {
		t.Fatalf("want no error with a caller: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -v`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Write the package skeleton**

Create `internal/cli/cli.go`:

```go
// Package cli implements the agent-manager subcommands an agent runs
// from inside its own managed session to see and talk to sibling
// sessions. Dependencies are injected so every subcommand tests without
// a real tmux server.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
)

var errNoSession = errors.New("tmux session not found")

type Tmux interface {
	SendText(id, text string) error
	CapturePane(id string) (string, error)
	Exists(id string) bool
}

type Deps struct {
	Store  *store.Store
	Tmux   Tmux
	Caller string
	Out    io.Writer
}

func (d Deps) requireCaller() error {
	if d.Caller == "" {
		return fmt.Errorf("not inside an agent-manager session (%s is unset)", hooks.EnvSessionID)
	}
	return nil
}

// resolveTarget accepts an exact id, an exact name, or a unique id
// prefix. Ambiguity is an error listing the candidates rather than a
// guess, because the wrong guess messages the wrong agent.
func resolveTarget(sessions []store.Session, query string) (store.Session, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return store.Session{}, fmt.Errorf("no target session given")
	}
	for _, sess := range sessions {
		if sess.ID == query {
			return sess, nil
		}
	}
	var matches []store.Session
	for _, sess := range sessions {
		if strings.EqualFold(sess.Name, query) {
			matches = append(matches, sess)
		}
	}
	if len(matches) == 0 {
		for _, sess := range sessions {
			if strings.HasPrefix(sess.ID, query) {
				matches = append(matches, sess)
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return store.Session{}, fmt.Errorf("no session matches %q (try: agent-manager sessions)", query)
	default:
		return store.Session{}, fmt.Errorf("%q is ambiguous: %s", query, describe(matches))
	}
}

func describe(sessions []store.Session) string {
	parts := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		parts = append(parts, fmt.Sprintf("%s (%s)", sess.ID, sess.Name))
	}
	return strings.Join(parts, ", ")
}

func (d Deps) activeSessions() ([]store.Session, error) {
	return d.Store.ListSessions(false)
}
```

- [ ] **Step 4: Write the `sessions` subcommand**

Create `internal/cli/sessions.go`:

```go
package cli

import (
	"fmt"
	"text/tabwriter"
)

func Sessions(d Deps) error {
	sessions, err := d.activeSessions()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(d.Out, "no active sessions")
		return err
	}
	writer := tabwriter.NewWriter(d.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tNAME\tTOOL\tSTATUS\tGROUP\tCWD")
	for _, sess := range sessions {
		name := sess.Name
		if sess.ID == d.Caller {
			name += " (you)"
		}
		group := sess.Group
		if group == "" {
			group = "-"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", sess.ID, name, sess.Tool, sess.Status, group, sess.Cwd)
	}
	return writer.Flush()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "feat: add sessions subcommand and target resolution"
```

---

### Task 4: `peek` subcommand

**Files:**
- Create: `internal/cli/peek.go`
- Test: `internal/cli/peek_test.go`

**Interfaces:**
- Consumes: `Deps`, `resolveTarget` from Task 3.
- Produces: `func Peek(d Deps, target string) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/peek_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestPeekPrintsStrippedPane(t *testing.T) {
	d, tm, out := newTestDeps(t, "a1b2c3")
	seed(t, d, "d4e5f6", "docs-pass")
	tm.panes["d4e5f6"] = "\x1b[31mrunning tests\x1b[0m\ndone"

	if err := Peek(d, "docs-pass"); err != nil {
		t.Fatalf("Peek: %v", err)
	}
	text := out.String()
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("ANSI escapes must be stripped, got %q", text)
	}
	if !strings.Contains(text, "running tests") || !strings.Contains(text, "done") {
		t.Fatalf("want pane content, got %q", text)
	}
}

func TestPeekUnknownTarget(t *testing.T) {
	d, _, _ := newTestDeps(t, "a1b2c3")
	if err := Peek(d, "nope"); err == nil {
		t.Fatal("want error for unknown target")
	}
}

func TestPeekDeadSession(t *testing.T) {
	d, tm, _ := newTestDeps(t, "a1b2c3")
	seed(t, d, "d4e5f6", "docs-pass")
	tm.missing["d4e5f6"] = true
	if err := Peek(d, "docs-pass"); err == nil {
		t.Fatal("want error when the tmux session is gone")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run Peek -v`
Expected: FAIL to compile with `undefined: Peek`.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/peek.go`:

```go
package cli

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

func Peek(d Deps, target string) error {
	sessions, err := d.activeSessions()
	if err != nil {
		return err
	}
	sess, err := resolveTarget(sessions, target)
	if err != nil {
		return err
	}
	pane, err := d.Tmux.CapturePane(sess.ID)
	if err != nil {
		return fmt.Errorf("cannot read %s (%s): %w", sess.Name, sess.ID, err)
	}
	_, err = fmt.Fprintf(d.Out, "--- %s (%s) status=%s ---\n%s\n", sess.Name, sess.ID, sess.Status, ansi.Strip(pane))
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/peek.go internal/cli/peek_test.go
git commit -m "feat: add peek subcommand"
```

---

### Task 5: `send` and `reply` subcommands

**Files:**
- Create: `internal/cli/send.go`
- Test: `internal/cli/send_test.go`

**Interfaces:**
- Consumes: `Deps`, `resolveTarget` (Task 3); `message.Inject`, `message.Squash` (Task 2); `store.InsertMessage`, `store.MarkDelivered`, `store.Message` (Task 1).
- Produces:
  - `func Send(d Deps, target, body string) error`
  - `func Reply(d Deps, msgID int64, body string) error`

Both print `delivered` when injection succeeded and `queued` when it did not, so the calling agent knows whether to expect a prompt answer. Neither returns an error for a dead target: the message is safely stored.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/send_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestSendStoresAndInjects(t *testing.T) {
	d, tm, out := newTestDeps(t, "a1b2c3")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")

	if err := Send(d, "docs-pass", "are you done with the docs?"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent := tm.sent["d4e5f6"]
	if len(sent) != 1 {
		t.Fatalf("want 1 injected message, got %d", len(sent))
	}
	for _, want := range []string{"auth-refactor", "a1b2c3", "are you done with the docs?", "agent-manager reply"} {
		if !strings.Contains(sent[0], want) {
			t.Fatalf("injected %q missing %q", sent[0], want)
		}
	}
	if strings.ContainsAny(sent[0], "\n\r") {
		t.Fatalf("injected text must be one line: %q", sent[0])
	}

	stored, err := d.Store.UnreadMessages("d4e5f6")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].From != "a1b2c3" || !stored[0].Delivered {
		t.Fatalf("want one delivered stored message, got %+v", stored)
	}
	if !strings.Contains(out.String(), "delivered") {
		t.Fatalf("want delivered confirmation, got %q", out.String())
	}
}

func TestSendToDeadSessionQueues(t *testing.T) {
	d, tm, out := newTestDeps(t, "a1b2c3")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")
	tm.missing["d4e5f6"] = true

	if err := Send(d, "docs-pass", "ping"); err != nil {
		t.Fatalf("a dead target must not fail the send: %v", err)
	}
	pending, err := d.Store.UndeliveredFor("d4e5f6")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want the message queued, got %d", len(pending))
	}
	if !strings.Contains(out.String(), "queued") {
		t.Fatalf("want queued confirmation, got %q", out.String())
	}
}

func TestSendRequiresCallerAndBody(t *testing.T) {
	d, _, _ := newTestDeps(t, "")
	seed(t, d, "d4e5f6", "docs-pass")
	if err := Send(d, "docs-pass", "hi"); err == nil {
		t.Fatal("want error without AGENT_MANAGER_SESSION_ID")
	}

	d.Caller = "a1b2c3"
	if err := Send(d, "docs-pass", "   "); err == nil {
		t.Fatal("want error for an empty body")
	}
}

func TestSendToSelfRejected(t *testing.T) {
	d, _, _ := newTestDeps(t, "a1b2c3")
	seed(t, d, "a1b2c3", "auth-refactor")
	if err := Send(d, "auth-refactor", "hello me"); err == nil {
		t.Fatal("want error when messaging yourself")
	}
}

func TestReplyGoesBackToTheOriginalSender(t *testing.T) {
	d, tm, _ := newTestDeps(t, "d4e5f6")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")

	id, err := d.Store.InsertMessage("a1b2c3", "d4e5f6", "are you done?")
	if err != nil {
		t.Fatal(err)
	}
	if err := Reply(d, id, "yes, PR #40 is open"); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	sent := tm.sent["a1b2c3"]
	if len(sent) != 1 {
		t.Fatalf("want the reply injected into the original sender, got %d", len(sent))
	}
	for _, want := range []string{"docs-pass", "yes, PR #40 is open", "in reply to #"} {
		if !strings.Contains(sent[0], want) {
			t.Fatalf("reply %q missing %q", sent[0], want)
		}
	}
}

func TestReplyRejectsMessageNotAddressedToCaller(t *testing.T) {
	d, _, _ := newTestDeps(t, "zzz999")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")
	id, _ := d.Store.InsertMessage("a1b2c3", "d4e5f6", "not for you")

	if err := Reply(d, id, "butting in"); err == nil {
		t.Fatal("want error replying to someone else's message")
	}
}

func TestReplyUnknownMessage(t *testing.T) {
	d, _, _ := newTestDeps(t, "d4e5f6")
	if err := Reply(d, 404, "hello"); err == nil {
		t.Fatal("want error for unknown message id")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'Send|Reply' -v`
Expected: FAIL to compile with `undefined: Send`.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/send.go`:

```go
package cli

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/message"
	"github.com/YoanWai/agent-manager/internal/store"
)

func Send(d Deps, target, body string) error {
	if err := d.requireCaller(); err != nil {
		return err
	}
	sessions, err := d.activeSessions()
	if err != nil {
		return err
	}
	sess, err := resolveTarget(sessions, target)
	if err != nil {
		return err
	}
	if sess.ID == d.Caller {
		return fmt.Errorf("cannot message yourself")
	}
	return deliver(d, sess, body, "")
}

func Reply(d Deps, msgID int64, body string) error {
	if err := d.requireCaller(); err != nil {
		return err
	}
	original, err := d.Store.Message(msgID)
	if err != nil {
		return err
	}
	if original.To != d.Caller {
		return fmt.Errorf("message #%d was not addressed to this session", msgID)
	}
	sessions, err := d.activeSessions()
	if err != nil {
		return err
	}
	sess, err := resolveTarget(sessions, original.From)
	if err != nil {
		return fmt.Errorf("cannot reply to #%d: %w", msgID, err)
	}
	return deliver(d, sess, body, fmt.Sprintf("in reply to #%d: ", msgID))
}

// deliver stores the message first so nothing is lost when the target is
// dead, then tries the live injection. A failed injection is reported,
// not returned: the poller redelivers it once the target is ready.
func deliver(d Deps, target store.Session, body, prefix string) error {
	flat := message.Squash(body)
	if flat == "" {
		return fmt.Errorf("message body is empty")
	}
	id, err := d.Store.InsertMessage(d.Caller, target.ID, prefix+flat)
	if err != nil {
		return err
	}
	msg, err := d.Store.Message(id)
	if err != nil {
		return err
	}

	senderName := d.Caller
	sessions, err := d.activeSessions()
	if err == nil {
		for _, sess := range sessions {
			if sess.ID == d.Caller {
				senderName = sess.Name
			}
		}
	}

	if d.Tmux.Exists(target.ID) {
		if err := d.Tmux.SendText(target.ID, message.Inject(msg, senderName)); err == nil {
			if err := d.Store.MarkDelivered(id); err != nil {
				return err
			}
			_, err := fmt.Fprintf(d.Out, "delivered #%d to %s (%s)\n", id, target.Name, target.ID)
			return err
		}
	}
	_, err = fmt.Fprintf(d.Out, "queued #%d for %s (%s): not running, it will be delivered when the session returns\n", id, target.Name, target.ID)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS (14 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/send.go internal/cli/send_test.go
git commit -m "feat: add send and reply subcommands"
```

---

### Task 6: `inbox` subcommand

**Files:**
- Create: `internal/cli/inbox.go`
- Test: `internal/cli/inbox_test.go`

**Interfaces:**
- Consumes: `Deps` (Task 3); `store.UnreadMessages`, `store.MessagesFor`, `store.MarkSeen` (Task 1).
- Produces: `func Inbox(d Deps, all bool) error`

Reading marks the printed unread messages seen, so `inbox` is the agent's read-receipt.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/inbox_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestInboxPrintsUnreadAndMarksSeen(t *testing.T) {
	d, _, out := newTestDeps(t, "d4e5f6")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")
	if _, err := d.Store.InsertMessage("a1b2c3", "d4e5f6", "are you done?"); err != nil {
		t.Fatal(err)
	}

	if err := Inbox(d, false); err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	text := out.String()
	for _, want := range []string{"auth-refactor", "a1b2c3", "are you done?"} {
		if !strings.Contains(text, want) {
			t.Fatalf("inbox %q missing %q", text, want)
		}
	}

	unread, err := d.Store.UnreadMessages("d4e5f6")
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 0 {
		t.Fatalf("reading the inbox should mark messages seen, %d still unread", len(unread))
	}
}

func TestInboxEmpty(t *testing.T) {
	d, _, out := newTestDeps(t, "d4e5f6")
	if err := Inbox(d, false); err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if !strings.Contains(out.String(), "no new messages") {
		t.Fatalf("want empty notice, got %q", out.String())
	}
}

func TestInboxAllIncludesSeen(t *testing.T) {
	d, _, out := newTestDeps(t, "d4e5f6")
	seed(t, d, "a1b2c3", "auth-refactor")
	seed(t, d, "d4e5f6", "docs-pass")
	d.Store.InsertMessage("a1b2c3", "d4e5f6", "first question")

	if err := Inbox(d, false); err != nil {
		t.Fatal(err)
	}
	out.Reset()

	if err := Inbox(d, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "first question") {
		t.Fatal("a seen message should not appear in the default inbox")
	}
	out.Reset()

	if err := Inbox(d, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "first question") {
		t.Fatalf("--all should include seen messages, got %q", out.String())
	}
}

func TestInboxRequiresCaller(t *testing.T) {
	d, _, _ := newTestDeps(t, "")
	if err := Inbox(d, false); err == nil {
		t.Fatal("want error without AGENT_MANAGER_SESSION_ID")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run Inbox -v`
Expected: FAIL to compile with `undefined: Inbox`.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/inbox.go`:

```go
package cli

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/store"
)

func Inbox(d Deps, all bool) error {
	if err := d.requireCaller(); err != nil {
		return err
	}
	var messages []store.Message
	var err error
	if all {
		messages, err = d.Store.MessagesFor(d.Caller)
	} else {
		messages, err = d.Store.UnreadMessages(d.Caller)
	}
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		_, err := fmt.Fprintln(d.Out, "no new messages")
		return err
	}

	names := map[string]string{}
	if sessions, err := d.Store.ListSessions(true); err == nil {
		for _, sess := range sessions {
			names[sess.ID] = sess.Name
		}
	}

	unseen := make([]int64, 0, len(messages))
	for _, msg := range messages {
		sender := names[msg.From]
		if sender == "" {
			sender = "unknown"
		}
		fmt.Fprintf(d.Out, "[#%d] from %s (%s) at %s: %s\n",
			msg.ID, sender, msg.From, msg.CreatedAt.Format("15:04:05"), msg.Body)
		if !msg.Seen {
			unseen = append(unseen, msg.ID)
		}
	}
	fmt.Fprintf(d.Out, "\nReply with: agent-manager reply <id> \"<answer>\"\n")
	return d.Store.MarkSeen(unseen)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS (18 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/inbox.go internal/cli/inbox_test.go
git commit -m "feat: add inbox subcommand"
```

---

### Task 7: Wire the subcommands into `main.go`

**Files:**
- Modify: `main.go` (dispatch at lines 21-45)
- Test: `main_test.go`

**Interfaces:**
- Consumes: `cli.Sessions`, `cli.Peek`, `cli.Send`, `cli.Reply`, `cli.Inbox`, `cli.Deps` (Tasks 3-6).
- Produces: `func runSubcommand(args []string) (handled bool, err error)` — reports whether `args[0]` was one of the new subcommands, so `main` falls through to the TUI otherwise.

- [ ] **Step 1: Write the failing test**

Append to `main_test.go`:

```go
func TestRunSubcommandIgnoresUnknown(t *testing.T) {
	handled, err := runSubcommand([]string{"definitely-not-a-subcommand"})
	if handled {
		t.Fatal("unknown arguments must fall through to the TUI")
	}
	if err != nil {
		t.Fatalf("want no error for unhandled args: %v", err)
	}
}

func TestRunSubcommandRejectsBadUsage(t *testing.T) {
	cases := [][]string{
		{"peek"},
		{"send", "target"},
		{"reply", "not-a-number", "body"},
		{"reply", "7"},
	}
	for _, args := range cases {
		handled, err := runSubcommand(args)
		if !handled {
			t.Fatalf("%v should be handled", args)
		}
		if err == nil {
			t.Fatalf("%v: want a usage error", args)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run RunSubcommand -v`
Expected: FAIL to compile with `undefined: runSubcommand`.

- [ ] **Step 3: Add the dispatch**

In `main.go`, replace the `main` function and add the new helpers. The complete new top of `main.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/cli"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("agent-manager", version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "rename" {
		if err := renameCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "agent-manager:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		handled, err := runSubcommand(os.Args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-manager:", err)
			os.Exit(1)
		}
		if handled {
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-manager:", err)
		os.Exit(1)
	}
}

// runSubcommand handles the session-to-session commands. handled stays
// false for anything it does not recognize, so unknown arguments keep
// launching the TUI as before.
func runSubcommand(args []string) (bool, error) {
	switch args[0] {
	case "sessions", "peek", "send", "reply", "inbox":
	default:
		return false, nil
	}

	deps, closeStore, err := cliDeps()
	if err != nil {
		return true, err
	}
	defer closeStore()

	rest := args[1:]
	switch args[0] {
	case "sessions":
		return true, cli.Sessions(deps)
	case "peek":
		if len(rest) != 1 {
			return true, fmt.Errorf("usage: agent-manager peek <id|name>")
		}
		return true, cli.Peek(deps, rest[0])
	case "send":
		if len(rest) != 2 {
			return true, fmt.Errorf(`usage: agent-manager send <id|name> "<message>"`)
		}
		return true, cli.Send(deps, rest[0], rest[1])
	case "reply":
		if len(rest) != 2 {
			return true, fmt.Errorf(`usage: agent-manager reply <msg-id> "<message>"`)
		}
		id, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil {
			return true, fmt.Errorf("message id must be a number: %q", rest[0])
		}
		return true, cli.Reply(deps, id, rest[1])
	case "inbox":
		all := len(rest) == 1 && rest[0] == "--all"
		if len(rest) > 1 || (len(rest) == 1 && !all) {
			return true, fmt.Errorf("usage: agent-manager inbox [--all]")
		}
		return true, cli.Inbox(deps, all)
	}
	return true, nil
}

func cliDeps() (cli.Deps, func(), error) {
	dir, err := config.Dir()
	if err != nil {
		return cli.Deps{}, nil, err
	}
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return cli.Deps{}, nil, err
	}
	driver, err := tmux.New()
	if err != nil {
		st.Close()
		return cli.Deps{}, nil, err
	}
	deps := cli.Deps{
		Store:  st,
		Tmux:   driver,
		Caller: os.Getenv(hooks.EnvSessionID),
		Out:    os.Stdout,
	}
	return deps, func() { st.Close() }, nil
}
```

The rest of `main.go` (`renameCommand`, `sessionIDPattern`, `runRename`, `run`) is unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -v`
Expected: PASS. Note `TestRunSubcommandRejectsBadUsage` exercises argument validation before any store is opened for the `reply`-parse case; if a case reaches `cliDeps` in a sandbox without a config dir, it still returns an error, which the test accepts.

- [ ] **Step 5: Verify the whole build and type-check**

Run: `go build ./... && go vet ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: dispatch cross-session subcommands"
```

---

### Task 8: Poller redelivery backstop

**Files:**
- Modify: `internal/ui/poller.go` (poll loop ~lines 209-215, next to `maybeSendDirective`)
- Test: `internal/ui/redeliver_test.go`

**Interfaces:**
- Consumes: `store.UndeliveredFor`, `store.MarkDelivered` (Task 1); `message.Inject` (Task 2); existing `p.engine.ActivityRegion`.
- Produces: `func (p *poller) deliverPending(sess store.Session, pane string, agentAlive bool, names map[string]string)`

The gate matches `maybeSendDirective` exactly: the agent must be alive and the tool's input box must be visible, proving the pane can take typed input.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/redeliver_test.go`:

```go
package ui

import (
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

// A queued message must survive until its target can take input, and
// must be injected exactly once.
func TestDeliverPendingMarksDeliveredOnce(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	target := store.Session{ID: "d4e5f6", Name: "docs-pass", Tool: "claude", Cwd: "/tmp", Status: "idle"}
	if err := st.CreateSession(target); err != nil {
		t.Fatal(err)
	}
	id, err := st.InsertMessage("a1b2c3", "d4e5f6", "ping")
	if err != nil {
		t.Fatal(err)
	}

	pending, err := st.UndeliveredFor("d4e5f6")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("want one pending message, got %+v", pending)
	}

	if err := st.MarkDelivered(id); err != nil {
		t.Fatal(err)
	}
	pending, _ = st.UndeliveredFor("d4e5f6")
	if len(pending) != 0 {
		t.Fatalf("delivered message must not be re-queued, got %d", len(pending))
	}
}

// A dead agent must not consume the queue: the message stays pending so
// a later poll delivers it.
func TestDeliverPendingSkipsDeadAgent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	sess := store.Session{ID: "d4e5f6", Name: "docs-pass", Tool: "claude", Cwd: "/tmp", Status: "idle"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertMessage("a1b2c3", "d4e5f6", "ping"); err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st}
	p.deliverPending(sess, "", false, map[string]string{})

	pending, _ := st.UndeliveredFor("d4e5f6")
	if len(pending) != 1 {
		t.Fatalf("a dead agent must leave the message queued, got %d", len(pending))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run DeliverPending -v`
Expected: FAIL to compile with `p.deliverPending undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/ui/poller.go`, add the `message` import to the import block:

```go
	"github.com/YoanWai/agent-manager/internal/message"
```

Add this method directly after `maybeSendDirective` (line 330):

```go
// deliverPending injects messages that missed their target: the sender
// stored them while the session was dead or still booting. The readiness
// gate matches maybeSendDirective, so text only lands once the tool's
// input box can take it. A failed send leaves the row queued for the
// next poll.
func (p *poller) deliverPending(sess store.Session, pane string, agentAlive bool, names map[string]string) {
	if !agentAlive {
		return
	}
	pending, err := p.store.UndeliveredFor(sess.ID)
	if err != nil || len(pending) == 0 {
		return
	}
	if _, ready := p.engine.ActivityRegion(sess.Tool, ansi.Strip(pane)); !ready {
		return
	}
	for _, msg := range pending {
		sender := names[msg.From]
		if sender == "" {
			sender = "unknown"
		}
		if err := p.tmux.SendText(sess.ID, message.Inject(msg, sender)); err != nil {
			return
		}
		if err := p.store.MarkDelivered(msg.ID); err != nil {
			return
		}
	}
}
```

- [ ] **Step 4: Call it from the poll loop**

In `internal/ui/poller.go`, build a name lookup once before the `for i, sess := range sessions` loop (next to the existing `claimed` map around line 186):

```go
	names := make(map[string]string, len(sessions))
	for _, sess := range sessions {
		names[sess.ID] = sess.Name
	}
```

Then inside the loop, immediately after the existing `p.maybeSendDirective(sess, pane, agentAlive)` call (line 211), add:

```go
				p.deliverPending(sess, pane, agentAlive, names)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS, including all pre-existing UI tests.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/poller.go internal/ui/redeliver_test.go
git commit -m "feat: redeliver queued messages from the poller"
```

---

### Task 9: Launch primer so every tool discovers the feature

**Files:**
- Modify: `internal/ui/form.go:356` and `:361`
- Test: `internal/ui/form_primer_test.go`

**Interfaces:**
- Consumes: existing `renameDirective`, `deferredRenameDirective`, `launchPrompt` in `internal/ui/form.go`.
- Produces: `const siblingPrimer` appended to both directives.

This is the discovery mechanism: no skill install, no per-user setup, identical for Claude Code, Codex, OpenCode, Grok Build, and any custom tool.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/form_primer_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func TestBothDirectivesCarryTheSiblingPrimer(t *testing.T) {
	for label, directive := range map[string]string{
		"renameDirective":         renameDirective,
		"deferredRenameDirective": deferredRenameDirective,
	} {
		for _, want := range []string{"agent-manager sessions", "agent-manager inbox", "agent-manager send"} {
			if !strings.Contains(directive, want) {
				t.Fatalf("%s missing %q", label, want)
			}
		}
	}
}

func TestLaunchPromptKeepsPrimerAndUserPrompt(t *testing.T) {
	got := launchPrompt("fix the auth bug", true)
	if !strings.Contains(got, "agent-manager sessions") {
		t.Fatalf("auto-named launch should carry the primer, got %q", got)
	}
	if !strings.HasSuffix(got, "fix the auth bug") {
		t.Fatalf("user prompt must stay last, got %q", got)
	}
}

func TestLaunchPromptCleanWhenNotAutoNamed(t *testing.T) {
	if got := launchPrompt("fix the auth bug", false); got != "fix the auth bug" {
		t.Fatalf("manually named launches stay clean, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run Primer -v`
Expected: FAIL with `renameDirective missing "agent-manager sessions"`.

- [ ] **Step 3: Write the implementation**

In `internal/ui/form.go`, replace the two directive constants (lines 353-361) with:

```go
// siblingPrimer teaches every session, whatever tool it runs, that it can
// see and talk to the other managed sessions. Carried by both rename
// directives because the binary is the only channel every tool shares.
const siblingPrimer = ` You are one of several agent sessions managed by agent-manager: run 'agent-manager sessions' to list the others, 'agent-manager peek <id>' to read one's screen, 'agent-manager send <id> <message>' to ask one a question, and 'agent-manager inbox' to read messages sent to you.`

// renameDirective asks the agent, as the first line of its first prompt,
// to name its own session via the rename subcommand. Injected only for
// auto-named sessions that launch with a prompt, so it fires exactly once.
const renameDirective = `First, run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name describing the task: agent-manager rename "<name>".` + siblingPrimer + ` Then do the task:`

// deferredRenameDirective is the standalone message sent into sessions
// whose first prompt could not carry the directive: slash-command
// prompts (the command must open the message) and promptless launches.
const deferredRenameDirective = `Run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name describing this session's work: agent-manager rename "<name>".` + siblingPrimer + ` Then continue.`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Verify the deferred directive is still a single line**

Run: `go test ./internal/ui/ -run Primer -v && grep -c $'\n' internal/ui/form.go > /dev/null && echo "single-line check: constants contain no literal newlines"`

Confirm by reading the constants: neither contains a literal newline, which matters because `deferredRenameDirective` is sent through `SendText`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/form.go internal/ui/form_primer_test.go
git commit -m "feat: teach every session about its siblings at launch"
```

---

### Task 10: Claude skills and documentation

**Files:**
- Create: `.claude/skills/rename-am/SKILL.md`
- Create: `.claude/skills/am-talk/SKILL.md`
- Modify: `README.md` (add a section after the Keys table)

**Interfaces:**
- Consumes: the five subcommands from Tasks 3-7.
- Produces: user-facing docs. No Go code, no tests.

These skills are convenience for a human driving Claude Code by hand. They are not the discovery mechanism; Task 9 is.

- [ ] **Step 1: Create the rename skill**

Create `.claude/skills/rename-am/SKILL.md`:

```markdown
---
name: rename-am
description: Rename the current agent-manager session from the conversation's context. Use when the user runs /rename-am, asks to rename this session, or says the session name no longer matches the work.
---

# Rename this agent-manager session

Pick a short 2-4 word kebab-case name describing what this session is actually working on, based on the conversation so far. Prefer the concrete task over the repo name.

Run:

```bash
agent-manager rename "<name>"
```

The command writes the name for the manager to apply on its next poll. It fails outside a managed session, which means `AGENT_MANAGER_SESSION_ID` is unset; report that rather than retrying.

Confirm the new name to the user in one line.
```

- [ ] **Step 2: Create the talk skill**

Create `.claude/skills/am-talk/SKILL.md`:

```markdown
---
name: am-talk
description: See, inspect, and message the other agent-manager sessions running on this machine. Use when the user asks what other sessions are doing, wants to check on another session, or wants to ask another session a question.
---

# Talk to sibling agent-manager sessions

## See what is running

```bash
agent-manager sessions
```

Lists every active session with its id, name, tool, status, group, and directory. Your own row is marked `(you)`.

## Read another session's screen

```bash
agent-manager peek <id|name>
```

Prints that session's current terminal content. Use this first when the user asks "what is X doing" — it answers without interrupting the other agent.

## Ask another session a question

```bash
agent-manager send <id|name> "<message>"
```

The message is stored and typed into the target's pane. The target agent finishes its current turn first, then sees your message, so this never aborts its work. Output tells you `delivered` (it is running) or `queued` (it will get it when it returns).

## Read and answer your own messages

```bash
agent-manager inbox          # unread
agent-manager inbox --all    # including already-read
agent-manager reply <msg-id> "<answer>"
```

## Rules

- Peek before sending. Most "check on X" questions need no message at all.
- Keep messages to one specific question. They are flattened to a single line.
- After answering a message that arrived mid-task, continue what you were doing.
```

- [ ] **Step 3: Document in the README**

In `README.md`, add this section immediately after the Keys table:

```markdown
### Talking between sessions

Every session can see and message the others through the same binary, whatever tool it runs:

| Command | Action |
|---------|--------|
| `agent-manager sessions` | List active sessions with status, group, and directory |
| `agent-manager peek <id\|name>` | Print another session's current screen |
| `agent-manager send <id\|name> "<msg>"` | Send a message to another session |
| `agent-manager inbox [--all]` | Read messages addressed to this session |
| `agent-manager reply <msg-id> "<msg>"` | Answer a message |
| `agent-manager rename "<name>"` | Rename this session |

Sessions learn these commands automatically at launch, so no setup is needed. A message is stored first and then typed into the target's pane; the receiving agent finishes its current turn before reading it, and a message sent to a stopped session is delivered when it comes back.
```

- [ ] **Step 4: Verify the docs match the built binary**

Run:
```bash
go build -o /tmp/am . && /tmp/am sessions
```
Expected: either the session table or `no active sessions`. Confirm every command name in the README table matches a case in `runSubcommand`.

- [ ] **Step 5: Commit**

```bash
git add .claude/skills README.md
git commit -m "docs: document cross-session messaging"
```

---

### Task 11: End-to-end verification on an isolated tmux socket

**Files:** none modified. This task proves the feature works in a live system before the branch is called done.

**Interfaces:**
- Consumes: everything from Tasks 1-10.
- Produces: observed evidence for the PR description.

**Critical:** every tmux command here MUST carry `env -u TMUX TMUX_TMPDIR=/tmp/amtest`. `$TMUX` overrides `TMUX_TMPDIR`, so without `-u TMUX` these commands hit the shared default socket and can kill the user's live sessions.

- [ ] **Step 1: Run the full test suite**

```bash
env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./... 2>&1 | tail -20
```
Expected: `ok` for every package, no `FAIL`.

- [ ] **Step 2: Confirm the isolated socket path fits**

```bash
mkdir -p /tmp/amtest && printf "/tmp/amtest/tmux-$(id -u)/default" | wc -c
```
Expected: a number well under 104. A longer path makes tmux silently fall back to the default socket.

- [ ] **Step 3: Verify isolation before touching tmux**

```bash
env -u TMUX TMUX_TMPDIR=/tmp/amtest tmux ls 2>&1
```
Expected: `no server running on ...` or a list that does NOT contain the user's real `am_*` sessions. If real sessions appear, STOP: isolation failed.

- [ ] **Step 4: Stand up two fake sessions and message between them**

```bash
go build -o /tmp/am .
export AMTEST_DIR=$(mktemp -d)

env -u TMUX TMUX_TMPDIR=/tmp/amtest tmux new-session -d -s am_aaa111 -c /tmp
env -u TMUX TMUX_TMPDIR=/tmp/amtest tmux new-session -d -s am_bbb222 -c /tmp
env -u TMUX TMUX_TMPDIR=/tmp/amtest tmux ls
```
Expected: both `am_aaa111` and `am_bbb222` listed, and nothing else.

- [ ] **Step 5: Exercise the commands against the real database**

Use the real config dir so the sessions resolve. Create the two rows through the TUI (`agent-manager`, press `n` twice) or verify against sessions that already exist, then from inside one managed session run:

```bash
agent-manager sessions
agent-manager peek <other-id>
agent-manager send <other-id> "reply with the word pong and then continue"
```
Expected: `sessions` lists both with `(you)` on your row; `peek` prints the other's screen; `send` prints `delivered #N`.

- [ ] **Step 6: Confirm queue-then-resume behavior**

In the other session, confirm the injected line appeared, that the agent answered with `agent-manager reply`, and that it returned to its prior task rather than abandoning it. Then in the original session:

```bash
agent-manager inbox
```
Expected: the reply is listed with the responder's name.

- [ ] **Step 7: Confirm queued delivery**

Kill a target session, send to it, revive it from the manager, and confirm the message arrives on a later poll:

```bash
agent-manager send <target> "queued test"
```
Expected: `queued #N for ...`, then after revival the line appears in the revived pane.

- [ ] **Step 8: Tear down the isolated sockets**

```bash
env -u TMUX TMUX_TMPDIR=/tmp/amtest tmux kill-server 2>/dev/null
env -u TMUX TMUX_TMPDIR=/tmp/amtest tmux ls
```
Expected: `no server running`. Confirm the user's real sessions are untouched: `tmux ls` (default socket) still lists them.

- [ ] **Step 9: Final quality gate**

```bash
go build ./... && go vet ./... && gofmt -l .
```
Expected: no output from any of the three.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| Five subcommands | 3 (`sessions`), 4 (`peek`), 5 (`send`, `reply`), 6 (`inbox`), 7 (dispatch) |
| Caller identity from env, clear error outside a session | 3 (`requireCaller`), enforced in 5 and 6 |
| Target resolution: id, prefix, name; ambiguity errors | 3 (`resolveTarget`) |
| `peek` strips ANSI | 4 |
| `inbox` marks seen, `--all` includes seen | 6 |
| `sessions` marks the caller | 3 |
| Discovery primer on both directives | 9 |
| Optional Claude skills | 10 |
| `messages` table and columns | 1 |
| WAL plus `busy_timeout` for cross-process writes | 1 (Step 4) |
| Message deleted with its session | 1 (Step 6) |
| Store-then-inject delivery | 5 (`deliver`) |
| Self-describing injection with resume instruction | 2 (`Inject`) |
| Poller redelivery gated on alive plus ready pane | 8 |
| `delivered` vs `queued` reporting | 5 |
| Single-line, bounded bodies | 2 (`Squash`), constraint enforced in 5 |
| Error handling cases | 3-7 tests |
| Testing: store, CLI, poller, directive, manual e2e | 1, 3-6, 8, 9, 11 |
| Out of scope items | none implemented |

Two spec details tightened during planning, both recorded above: the spec named `UndeliveredMessages()` globally, but the poller only ever needs one session's queue, so it became `UndeliveredFor(to)`; and the single-line injection constraint, implied by the spec, is now an explicit global constraint with a test enforcing it, because `tmux send-keys` submits on newline.

**Placeholder scan:** no TBD, TODO, or "handle errors appropriately" steps. Every code step carries complete code.

**Type consistency:** `Message` fields (`ID int64`, `From`, `To`, `Body`, `CreatedAt`, `Delivered`, `Seen`) are used identically in Tasks 1, 2, 5, 6, 8. `Deps` fields (`Store`, `Tmux`, `Caller`, `Out`) are consistent across Tasks 3-7. `resolveTarget(sessions, query)` has one signature throughout. `message.Inject(msg, senderName)` is called with the same argument order in Tasks 5 and 8. `UndeliveredFor(to string)` is defined in Task 1 and consumed in Tasks 5 and 8.
