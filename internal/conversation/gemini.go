package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
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

// geminiConversation reads the newest conversation the agent's own processes
// logged to its telemetry file. The slash command record that switches
// conversation is skipped: gemini deletes a conversation left with no message
// when it exits, so a cleared one counts from the first record after it,
// which is where a resumed one first shows too. A gemini run from shell mode
// inherits the file and runs in a process group of its own, so its records
// are skipped. The file holds one pretty-printed JSON object per record, each
// opening with a "{" line of its own.
func (r *Reader) geminiConversation(path string, agentPID int) (string, func() error, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 {
		return "", nil, nil
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
			return "", nil, err
		}
		if ours {
			id = record.Attributes.SessionID
		}
	}
	if !lastWhole {
		// A record still being written is read whole on a later poll. A
		// broken one before it is skipped for good.
		return id, nil, nil
	}
	return id, func() error { return truncateUnchanged(path, int64(len(data))) }, nil
}

// truncateUnchanged empties a file gemini appends to, unless it wrote more
// since it was read, which a later poll then reads.
func truncateUnchanged(path string, size int64) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && info.Size() != size) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Truncate(path, 0)
}
