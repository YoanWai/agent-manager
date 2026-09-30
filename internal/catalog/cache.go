package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/atomicfile"
	"github.com/YoanWai/agent-manager/internal/config"
)

// freshFor is how long an answer stands before it is asked again. A CLI
// upgrade outdates it at once, since the answer records the binary it came
// from.
const freshFor = 6 * time.Hour

type cached struct {
	ReadAt  time.Time `json:"read_at"`
	Binary  string    `json:"binary"`
	Catalog Catalog   `json:"catalog"`
}

func cacheDir(configDir string) string { return filepath.Join(configDir, "catalogs") }

// Cached returns the last answer tool gave, whether it is still fresh, and
// whether there is one at all.
func Cached(configDir, toolName string, tool config.Tool) (Catalog, bool, bool) {
	raw, err := os.ReadFile(filepath.Join(cacheDir(configDir), toolName+".json"))
	if err != nil {
		return Catalog{}, false, false
	}
	var entry cached
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Catalog{}, false, false
	}
	fresh := time.Since(entry.ReadAt) < freshFor && entry.Binary == binaryStamp(tool.CatalogCommand)
	return entry.Catalog, fresh, true
}

// Refresh asks tool again and keeps the answer for the next caller, the
// manager and the spawn command alike. The CLI runs from a directory of
// the manager's own, so a session an interface opens to answer never lands
// in a directory a real session uses.
func Refresh(ctx context.Context, configDir, toolName string, tool config.Tool) (Catalog, error) {
	if err := config.CheckInstalled(tool.CatalogCommand); err != nil {
		return Catalog{}, err
	}
	work := filepath.Join(cacheDir(configDir), "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return Catalog{}, err
	}
	cat, err := Fetch(ctx, tool.Catalog, tool.CatalogCommand, work)
	if err != nil {
		return Catalog{}, err
	}
	return cat, Keep(configDir, toolName, tool, cat)
}

// Keep stores cat as tool's answer as of now.
func Keep(configDir, toolName string, tool config.Tool, cat Catalog) error {
	raw, err := json.Marshal(cached{ReadAt: time.Now(), Binary: binaryStamp(tool.CatalogCommand), Catalog: cat})
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(cacheDir(configDir), toolName+".json"), raw, 0o644)
}

// Load is the fresh answer when there is one, asked again otherwise.
func Load(ctx context.Context, configDir, toolName string, tool config.Tool) (Catalog, error) {
	if cat, fresh, ok := Cached(configDir, toolName, tool); ok && fresh {
		return cat, nil
	}
	return Refresh(ctx, configDir, toolName, tool)
}

// binaryStamp names the executable command runs and when it last changed,
// so an upgrade in place reads as a different CLI.
func binaryStamp(command string) string {
	name, _, _ := strings.Cut(command, " ")
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s@%d", path, info.ModTime().UnixNano())
}
