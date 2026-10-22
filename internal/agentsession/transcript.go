package agentsession

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoTranscript reports a tool whose own record of a conversation the
// manager does not read.
var ErrNoTranscript = errors.New("tool keeps no transcript the manager reads")

// KeepsTranscript reports whether a tool, by its session_report style,
// writes a conversation file the manager can read the last exchange from.
func KeepsTranscript(style string) bool {
	switch style {
	case "claude", "pi", "omp":
		return true
	}
	return false
}

// TranscriptPath is the file holding the tool's own record of one
// conversation: the Claude Code transcript, or the session file pi and omp
// write. The error is os.ErrNotExist while the tool has not written it.
func TranscriptPath(style, cwd, sessionID string) (string, error) {
	switch style {
	case "claude":
		return ClaudeTranscriptPath(cwd, sessionID)
	case "pi":
		return piTranscriptPath(piRoot(), cwd, sessionID)
	case "omp":
		return piTranscriptPath(ompRoot(), cwd, sessionID)
	}
	return "", ErrNoTranscript
}

// TranscriptTail reads the newest prompt and reply from the transcript at
// path, in the tool's own format: the last user text cleanUser keeps (it
// returns the text to use, or "" to step past that entry) and the last
// assistant text block. ok is false when the file cannot be read at all.
func TranscriptTail(style, path string, cleanUser func(string) string) (prompt, reply string, ok bool) {
	switch style {
	case "claude":
		return claudeTranscriptTail(path, cleanUser)
	case "pi", "omp":
		return piTranscriptTail(path, cleanUser)
	}
	return "", "", false
}

// piRoot is where pi keeps one directory of session files per working
// directory: $PI_CODING_AGENT_SESSION_DIR, else sessions under
// $PI_CODING_AGENT_DIR or ~/.pi/agent, as pi --help documents them.
func piRoot() string {
	if dir := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); dir != "" {
		return dir
	}
	agentDir := os.Getenv("PI_CODING_AGENT_DIR")
	if agentDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		agentDir = filepath.Join(home, ".pi", "agent")
	}
	return filepath.Join(agentDir, "sessions")
}

// piCwdKey is the directory pi and omp file one working directory under.
// Pi strips one leading separator, then turns /, \ and : into dashes, so a
// Windows drive letter does not stay a colon in the name.
func piCwdKey(cwd string) string {
	resolved := resolvePath(cwd)
	resolved = strings.TrimPrefix(resolved, "/")
	resolved = strings.TrimPrefix(resolved, `\`)
	encoded := strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(resolved)
	return "--" + encoded + "--"
}

// piTranscriptPath is the session file pi or omp wrote for sessionID: under
// root, a directory named for the resolved working directory
// ("--private-tmp-work--"), holding <timestamp>_<id>.jsonl.
func piTranscriptPath(root, cwd, sessionID string) (string, error) {
	if root == "" {
		return "", os.ErrNotExist
	}
	dir := filepath.Join(root, piCwdKey(cwd))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_"+sessionID+".jsonl") {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", os.ErrNotExist
}

// piEntry is one record of the session file pi and omp share: messages
// carry a role and a content list. omp files the steering it injects as
// user messages attributed to the agent, and synthetic ones, which are not
// the prompt anyone typed.
type piEntry struct {
	Type    string `json:"type"`
	Message struct {
		Role        string          `json:"role"`
		Attribution string          `json:"attribution"`
		Synthetic   bool            `json:"synthetic"`
		Content     json.RawMessage `json:"content"`
	} `json:"message"`
}

func piTranscriptTail(path string, cleanUser func(string) string) (prompt, reply string, ok bool) {
	lines, ok := transcriptTailLines(path)
	if !ok {
		return "", "", false
	}
	for _, line := range lines {
		var entry piEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "message" {
			continue
		}
		text := contentText(entry.Message.Content)
		if text == "" {
			continue
		}
		switch entry.Message.Role {
		case "user":
			if entry.Message.Attribution == "agent" || entry.Message.Synthetic {
				continue
			}
			if cleaned := cleanUser(text); cleaned != "" {
				prompt = cleaned
			}
		case "assistant":
			reply = text
		}
	}
	return prompt, reply, true
}
