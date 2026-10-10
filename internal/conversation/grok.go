package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
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
	last   string
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

func (g *grokLog) conversation(agent Agent, since time.Time) (string, error) {
	if !agent.Ended && Alive(agent.PID) {
		running, err := agent.Running()
		if err != nil || !running {
			return "", err
		}
	}
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
		*g = grokLog{path: path}
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
			Time time.Time `json:"ts"`
			PID  int       `json:"pid"`
			SID  string    `json:"sid"`
		}
		if json.Unmarshal(line, &entry) != nil || entry.PID != agent.PID || entry.SID == "" || entry.Time.IsZero() || entry.Time.Before(since) {
			continue
		}
		if agent.Ended && entry.Time.After(agent.Recorded) {
			continue
		}
		g.last = entry.SID
	}
	return g.last, nil
}
