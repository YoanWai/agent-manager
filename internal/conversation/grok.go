package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// grokLog follows grok's unified log, where every line a session writes
// carries the process's pid and the conversation's sid. The last sid a pid
// wrote is the conversation it is on: grok logs a new or cleared
// conversation, a fork and a cold resume as it switches, and a warm resume
// at the next turn.
type grokLog struct {
	path   string
	file   os.FileInfo
	offset int64
	last   map[int]string
}

func grokLogPath() (string, error) {
	home := os.Getenv("GROK_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".grok")
	}
	return filepath.Join(home, "logs", "unified.jsonl"), nil
}

func (g *grokLog) conversation(pid int) (string, error) {
	path, err := grokLogPath()
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if path != g.path || g.file == nil || !os.SameFile(g.file, info) || info.Size() < g.offset {
		*g = grokLog{path: path, last: map[int]string{}}
	}
	g.file = info
	if _, err := file.Seek(g.offset, io.SeekStart); err != nil {
		return "", err
	}
	unread, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	// A line grok is still writing is read whole on a later poll.
	complete := bytes.LastIndexByte(unread, '\n') + 1
	g.offset += int64(complete)
	for line := range bytes.SplitSeq(unread[:complete], []byte("\n")) {
		var entry struct {
			PID int    `json:"pid"`
			SID string `json:"sid"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.PID > 0 && entry.SID != "" {
			g.last[entry.PID] = entry.SID
		}
	}
	return g.last[pid], nil
}
