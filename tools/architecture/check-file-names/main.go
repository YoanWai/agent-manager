// Command check-file-names keeps each feature's files together: every root
// UI source starts with its feature's name, and every test file is named
// after the source beside it.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// allowed lists test files that cover more than one source on purpose.
var allowed = map[string]string{
	"internal/cli/parity_test.go":                       "compares the CLI with the MCP server",
	"internal/execution/two_manager_test.go":            "runs two managers through the runner, poller and delivery",
	"internal/tmux/delivery_test.go":                    "covers context-aware delivery across control, input and tmux",
	"internal/ui/session_lifecycle_integration_test.go": "drives create, archive, restore and delete end to end",
}

// uiRoot holds the root UI package, whose files are named <feature>_<concern>.go.
const uiRoot = "internal/ui"

// features are the prefixes a root UI source may start with.
var features = map[string]string{
	"composer":     "prompt composers, chips and pasted images",
	"confirm":      "the confirm dialog",
	"connection":   "SSH connections: the dialog, remote rows, polling, preview and routed actions",
	"effects":      "the ordered effect lane and its executor",
	"focus":        "the Focus adapter and pane watch",
	"fork":         "the fork dialog",
	"frame":        "frame composition: dialog chrome, header, legend, IME, text layout and control escaping",
	"geometry":     "pane sizing, layout widths and the split ratio",
	"group":        "the New Group form",
	"help":         "the Help adapter",
	"input":        "raw input forwarded to panes",
	"launchhint":   "the launch fix dialog and CLI install",
	"model":        "the root Model, its services and shared host methods",
	"move":         "the move dialog",
	"notices":      "the notices panel and release feed",
	"pathcomplete": "directory completion for path fields",
	"preview":      "the session preview",
	"quick":        "the quick bar",
	"rail":         "the Rail adapter",
	"rename":       "the rename dialog",
	"review":       "the Review adapter and repo picker",
	"session":      "session lifecycle: launch, attach, kill, revive, archive, delete, restart, editor",
	"settings":     "the Settings dialog, key bindings and editor row",
	"spawn":        "the New Session form, model choice, spawn preflight and worktree probe",
	"startup":      "startup greeting and banner",
	"status":       "the status bar and toasts",
	"terminal":     "terminal handoff and link pages",
	"theme":        "themes, styles, surfaces and the stored theme",
	"workspace":    "polling, refresh and what the last poll returned",
}

// dispatch are the root files that route rather than own a feature.
var dispatch = map[string]bool{"keys.go": true, "mouse.go": true, "updates.go": true, "ids.go": true}

func main() {
	failed := false
	strays, err := unfiledSources(uiRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(strays) > 0 {
		fmt.Fprintf(os.Stderr, "root UI sources that start with no feature name:\n  %s\n", strings.Join(strays, "\n  "))
		failed = true
	}
	orphans, err := orphanTests(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(orphans) > 0 {
		fmt.Fprintf(os.Stderr, "test files with no source of the same name:\n  %s\n", strings.Join(orphans, "\n  "))
		failed = true
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS: every root UI source starts with its feature")
	fmt.Println("PASS: every test file is named after its source")
}

// unfiledSources returns the root UI sources whose name starts with no feature.
func unfiledSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var strays []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || dispatch[name] {
			continue
		}
		feature, _, _ := strings.Cut(strings.TrimSuffix(name, ".go"), "_")
		if _, ok := features[feature]; !ok {
			strays = append(strays, name)
		}
	}
	return strays, nil
}

// orphanTests walks root and returns the test files that pair with no source.
func orphanTests(root string) ([]string, error) {
	var orphans []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := allowed[rel]; ok || pairs(filepath.Dir(path), name) {
			return nil
		}
		orphans = append(orphans, rel)
		return nil
	})
	sort.Strings(orphans)
	return orphans, err
}

// pairs reports whether test is <source>_test.go or <source>_<aspect>_test.go
// beside <source>.go, or a shared helper, benchmark or TestMain file.
func pairs(dir, test string) bool {
	stem := strings.TrimSuffix(test, "_test.go")
	if stem == "helpers" || stem == "main" || strings.HasSuffix(stem, "_helpers") || strings.HasSuffix(stem, "_bench") {
		return true
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, stem+".go")); err == nil {
			return true
		}
		cut := strings.LastIndexByte(stem, '_')
		if cut <= 0 {
			return false
		}
		stem = stem[:cut]
	}
}
