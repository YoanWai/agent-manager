package diff

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
)

func TestFileDiffWirePreservesLoadStateAndFailure(t *testing.T) {
	source := BuildFile([]byte("before\n"), []byte("after\n"), git.ChangedFile{Path: "file"}, git.FileStat{})
	source.statKnown = true
	source.Err = errors.New("file changed while reading")
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var target FileDiff
	if err := json.Unmarshal(data, &target); err != nil {
		t.Fatal(err)
	}
	if !target.Loaded() || !target.StatKnown() || target.Err == nil || target.Err.Error() != source.Err.Error() || len(target.Lines) != len(source.Lines) {
		t.Fatalf("lost state: %+v", target)
	}
}
