package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// telemetryRecord is the part of a gemini telemetry record that names a
// conversation: a log record carries its session's id among its attributes,
// and its resource names the process that wrote it.
type telemetryRecord struct {
	HRTime   json.RawMessage `json:"hrTime"`
	Resource struct {
		Attributes [][]json.RawMessage `json:"_rawAttributes"`
	} `json:"resource"`
	Attributes struct {
		SessionID string `json:"session.id"`
		EventName string `json:"event.name"`
	} `json:"attributes"`
}

func (record telemetryRecord) pid() int {
	for _, pair := range record.Resource.Attributes {
		var key string
		var pid int
		if len(pair) == 2 && json.Unmarshal(pair[0], &key) == nil && key == "process.pid" && json.Unmarshal(pair[1], &pid) == nil {
			return pid
		}
	}
	return 0
}

// FollowTelemetry holds the file lock through reporting and cleanup, across manager processes.
func (r *Reader) FollowTelemetry(path string, agentPID int, report func(string) (bool, error)) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	id, whole, err := r.geminiConversation(data, agentPID)
	if err != nil {
		return err
	}
	if id != "" {
		consumed, err := report(id)
		if err != nil || !consumed {
			return err
		}
	}
	if !whole {
		return nil
	}
	info, err := file.Stat()
	if err != nil || info.Size() != int64(len(data)) {
		return err
	}
	return file.Truncate(0)
}

// A cleared conversation counts after its first message because Gemini deletes empty ones on exit.
func (r *Reader) geminiConversation(data []byte, agentPID int) (string, bool, error) {
	if len(data) == 0 {
		return "", false, nil
	}
	id, lastWhole := "", false
	for chunk := range bytes.SplitSeq(data, []byte("\n{\n")) {
		if !bytes.HasPrefix(chunk, []byte("{\n")) {
			chunk = append([]byte("{\n"), chunk...)
		}
		var record telemetryRecord
		lastWhole = json.Unmarshal(chunk, &record) == nil
		if !lastWhole {
			continue
		}
		if record.HRTime == nil || record.Attributes.SessionID == "" || record.Attributes.EventName == "" || record.Attributes.EventName == "gemini_cli.slash_command" {
			continue
		}
		ours, err := r.fromAgent(agentPID, record.pid())
		if err != nil {
			return "", false, err
		}
		if ours {
			id = record.Attributes.SessionID
		}
	}
	return id, lastWhole, nil
}
