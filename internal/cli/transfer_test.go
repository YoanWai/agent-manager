package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

func TestExportImportFilesAndStreams(t *testing.T) {
	for _, fileMode := range []bool{false, true} {
		name := "stream"
		if fileMode {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			source := t.TempDir()
			st := settingsStore(t, source)
			if err := st.SetEditor("editor --token private-value"); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveSessionPreset("", store.SessionPreset{Name: "review", Instructions: "Review carefully"}); err != nil {
				t.Fatal(err)
			}
			if err := st.AddGroup("project", "/source/project", "on", "main", ""); err != nil {
				t.Fatal(err)
			}
			if err := st.AddConnection(store.Connection{Name: "remote", Destination: "user@host"}); err != nil {
				t.Fatal(err)
			}
			var exported, warnings bytes.Buffer
			var args []string
			if fileMode {
				args = []string{filepath.Join(t.TempDir(), "config.json")}
			}
			if err := runExport(&exported, &warnings, sampleSpecs, args, source); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(warnings.String(), "secrets") || !strings.Contains(warnings.String(), "excludes sessions") {
				t.Fatalf("warnings = %s", &warnings)
			}
			if strings.Contains(warnings.String(), "private-value") {
				t.Fatal("warning exposed a value")
			}
			if fileMode {
				info, err := os.Stat(args[0])
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
					t.Fatalf("mode = %v", info.Mode())
				}
				if err := runExport(io.Discard, io.Discard, sampleSpecs, args, source); !errors.Is(err, os.ErrExist) {
					t.Fatalf("overwrite = %v", err)
				}
			} else if !json.Valid(exported.Bytes()) {
				t.Fatalf("stdout is not JSON: %s", &exported)
			}
			target := t.TempDir()
			var imported bytes.Buffer
			if err := runImport(&imported, &exported, sampleSpecs, args, target); err != nil {
				t.Fatal(err)
			}
			restored := settingsStore(t, target)
			editor, err := restored.Editor()
			if err != nil || editor != "editor --token private-value" {
				t.Fatalf("editor = %q, %v", editor, err)
			}
			presets, err := restored.SessionPresets()
			if err != nil || len(presets) != 1 {
				t.Fatalf("presets = %#v, %v", presets, err)
			}
			if !strings.Contains(imported.String(), "1 settings, 1 presets, 1 groups and 1 SSH connections") {
				t.Fatalf("summary = %s", &imported)
			}
		})
	}
}

func TestTransferSectionSelection(t *testing.T) {
	source := t.TempDir()
	st := settingsStore(t, source)
	if err := st.SetEditor("code"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSessionPreset("", store.SessionPreset{Name: "review", Instructions: "Review"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGroup("project", "/project", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.AddConnection(store.Connection{Name: "server", Destination: "host"}); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"settings", "presets", "groups", "connections"} {
		t.Run(section, func(t *testing.T) {
			var out bytes.Buffer
			if err := runExport(&out, io.Discard, sampleSpecs, []string{"--" + section}, source); err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields[section] == nil {
				t.Fatalf("fields = %#v", fields)
			}
			out.Reset()
			if err := runExport(&out, io.Discard, sampleSpecs, nil, source); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			if err := runImport(io.Discard, &out, sampleSpecs, []string{"--" + section}, target); err != nil {
				t.Fatal(err)
			}
			var imported bytes.Buffer
			if err := runExport(&imported, io.Discard, sampleSpecs, nil, target); err != nil {
				t.Fatal(err)
			}
			fields = nil
			if err := json.Unmarshal(imported.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields[section] == nil {
				t.Fatalf("imported = %#v", fields)
			}
		})
	}
}

func TestImportRejectsMalformedDocuments(t *testing.T) {
	for _, input := range []string{`{`, `null`, `{}`, `{"version":1,"sessions":[]}`, `{"version":1} {}`, `{"version":1} trailing`, `{"version":1,"settings":{"pane_size":"1x1"}}`} {
		t.Run(input, func(t *testing.T) {
			dir := t.TempDir()
			st := settingsStore(t, dir)
			if err := st.SetEditor("keep-me"); err != nil {
				t.Fatal(err)
			}
			if err := runImport(io.Discard, strings.NewReader(input), sampleSpecs, nil, dir); err == nil {
				t.Fatal("invalid import succeeded")
			}
			editor, err := st.Editor()
			if err != nil || editor != "keep-me" {
				t.Fatalf("editor = %q, %v", editor, err)
			}
		})
	}
}

func TestTransferHelpDoesNotOpenStore(t *testing.T) {
	for _, name := range []string{"export", "import"} {
		var out bytes.Buffer
		dir := filepath.Join(t.TempDir(), "absent")
		var err error
		if name == "export" {
			err = runExport(&out, io.Discard, sampleSpecs, []string{"--help"}, dir)
		} else {
			err = runImport(&out, strings.NewReader(""), sampleSpecs, []string{"--help"}, dir)
		}
		if !errors.Is(err, ErrUsageShown) || !strings.Contains(out.String(), configurationScope) {
			t.Fatalf("help = %s, %v", &out, err)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("help created the store: %v", err)
		}
	}
}
