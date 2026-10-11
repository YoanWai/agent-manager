package agentsession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// transcriptTailBytes is how much of a transcript's tail is read for the
// newest prompt and reply; a longer reply than this is quoted from
// wherever the window starts.
const transcriptTailBytes = 256 * 1024

// ClaudeTranscriptPath is the session transcript Claude Code keeps for
// one conversation: unlike the pane, which Claude repaints in place and
// tmux holds no history for, the transcript has the whole exchange.
func ClaudeTranscriptPath(cwd, sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects", claudeProjectSlug(resolvePath(cwd)), sessionID+".jsonl"), nil
}

// claudeProjectSlug names the per-directory folder Claude Code files a
// transcript under: the resolved working directory with every character
// outside [A-Za-z0-9] turned into a dash.
func claudeProjectSlug(path string) string {
	slug := []rune(path)
	for i, r := range slug {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum {
			slug[i] = '-'
		}
	}
	return string(slug)
}

type claudeEntry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// claudeTranscriptTail reads the newest prompt and reply from a session
// transcript: the last user text cleanUser keeps (it returns the text to
// use, or "" to step past that entry) and the last assistant text block.
// ok is false when the transcript cannot be read at all.
func claudeTranscriptTail(path string, cleanUser func(string) string) (prompt, reply string, ok bool) {
	lines, ok := transcriptTailLines(path)
	if !ok {
		return "", "", false
	}
	for _, line := range lines {
		var entry claudeEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.IsSidechain || entry.IsMeta {
			continue
		}
		text := contentText(entry.Message.Content)
		if text == "" {
			continue
		}
		switch entry.Type {
		case "user":
			if cleaned := cleanUser(text); cleaned != "" {
				prompt = cleaned
			}
		case "assistant":
			reply = text
		}
	}
	return prompt, reply, true
}

// transcriptTailLines is the newest transcriptTailBytes of a JSONL file as
// whole lines, the partial first one dropped. ok is false when the file
// cannot be read.
func transcriptTailLines(path string) ([]string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false
	}
	offset := int64(0)
	if info.Size() > transcriptTailBytes {
		offset = info.Size() - transcriptTailBytes
	}
	raw := make([]byte, info.Size()-offset)
	if _, err := file.ReadAt(raw, offset); err != nil {
		return nil, false
	}
	lines := strings.Split(string(raw), "\n")
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	kept := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return kept, true
}

// contentText is the first plain-text block of a message's content: a
// bare string for typed prompts, a text item for replies. Tool results and
// tool calls carry no text block and yield nothing.
func contentText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var direct string
	if err := json.Unmarshal(content, &direct); err == nil {
		return strings.TrimSpace(direct)
	}
	var items []contentItem
	if err := json.Unmarshal(content, &items); err != nil {
		return ""
	}
	for _, item := range items {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text)
		}
	}
	return ""
}
