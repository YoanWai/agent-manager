// Command check-file-sizes fails when a Go source file reaches the line cap,
// so a file stays one a reviewer reads in a single pass.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxLines = 1000

func main() {
	over, err := oversized(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(over) > 0 {
		fmt.Fprintf(os.Stderr, "source files at or over %d lines; split them by concern:\n  %s\n", maxLines, strings.Join(over, "\n  "))
		os.Exit(1)
	}
	fmt.Printf("PASS: every Go source file is under %d lines\n", maxLines)
}

// oversized returns the non-test Go files under root with maxLines or more lines.
func oversized(root string) ([]string, error) {
	var over []string
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
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if lines := bytes.Count(data, []byte("\n")); lines >= maxLines {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			over = append(over, fmt.Sprintf("%s (%d)", filepath.ToSlash(rel), lines))
		}
		return nil
	})
	sort.Strings(over)
	return over, err
}
