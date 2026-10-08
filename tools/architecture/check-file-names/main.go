// Command check-test-names fails when a test file is not named after the
// source beside it, so a feature's tests stay where its code is.
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

func main() {
	orphans, err := orphanTests(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(orphans) > 0 {
		fmt.Fprintf(os.Stderr, "test files with no source of the same name:\n  %s\n", strings.Join(orphans, "\n  "))
		os.Exit(1)
	}
	fmt.Println("PASS: every test file is named after its source")
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
