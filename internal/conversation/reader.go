package conversation

import (
	"path/filepath"
	"strings"
)

// Polled reports whether a session_report style is read by Reader while the
// agent runs.
func Polled(style string) bool {
	switch style {
	case "muse", "antigravity", "grok", "gemini":
		return true
	}
	return false
}

// Reader answers which conversation a running agent is on. It keeps the
// part of grok's log it has read and the processes it has placed, so a poll
// reads only what is new.
type Reader struct {
	grok      grokLog
	processes map[[2]int]bool
}

// Current names the conversation agent is on for a Polled style, or "" when
// its CLI's state names none. Grok's log outlives the agent, so it still
// names the conversation an agent that has quit was on. done, when not nil,
// consumes what was read once the caller has recorded the answer: gemini's
// telemetry file, which the manager owns, is emptied so it never grows past
// a poll's worth.
func (r *Reader) Current(style string, agent Agent, telemetryFile string) (id string, done func() error, err error) {
	switch style {
	case "muse":
		id, err = heldConversation(agent, museConversation)
	case "antigravity":
		id, err = heldConversation(agent, antigravityConversation)
	case "grok":
		id, err = r.grok.conversation(agent.PID)
	case "gemini":
		return r.geminiConversation(telemetryFile, agent.PID)
	}
	return id, nil, err
}

// heldConversation names the one conversation the agent holds a file open
// for, by what conversationOf reads from each open path.
func heldConversation(agent Agent, conversationOf func(path string) string) (string, error) {
	running, err := agent.Running()
	if err != nil || !running {
		return "", err
	}
	paths, err := openFiles(agent.PID)
	if err != nil {
		return "", err
	}
	held := ""
	for _, path := range paths {
		id := conversationOf(path)
		if id == "" {
			continue
		}
		if held != "" && held != id {
			return "", nil
		}
		held = id
	}
	return held, nil
}

// museConversation reads the files Muse holds open for its current session
// alone: the session's entry in its local registry,
// muse/sessions/<id>.json, or, when that registry is unavailable, the lock
// in the session's own directory, sessions/<date>/<id>/.session.lock.
func museConversation(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(path) == ".session.lock" && strings.Contains(path, "/muse/sessions/") {
		return filepath.Base(dir)
	}
	if filepath.Ext(path) == ".json" && filepath.Base(dir) == "sessions" && filepath.Base(filepath.Dir(dir)) == "muse" {
		return strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return ""
}

// antigravityConversation reads the presence lock Antigravity holds for its
// conversation, antigravity-cli/presence/<id>.lock.
func antigravityConversation(path string) string {
	dir := filepath.Dir(path)
	if filepath.Ext(path) == ".lock" && filepath.Base(dir) == "presence" && filepath.Base(filepath.Dir(dir)) == "antigravity-cli" {
		return strings.TrimSuffix(filepath.Base(path), ".lock")
	}
	return ""
}

// fromAgent places a process once per agent, since gemini's telemetry
// names the same few processes on every poll.
func (r *Reader) fromAgent(agent, process int) (bool, error) {
	key := [2]int{agent, process}
	if placed, seen := r.processes[key]; seen {
		return placed, nil
	}
	placed, err := FromAgent(agent, process, true)
	if err != nil {
		return false, err
	}
	if r.processes == nil {
		r.processes = map[[2]int]bool{}
	}
	r.processes[key] = placed
	return placed, nil
}
