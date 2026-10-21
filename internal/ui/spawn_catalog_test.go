package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

// The first need for a CLI's rows reads the kept answer, off the update
// path, and a fresh one is not asked again.
func TestCatalogIsReadFromTheKeptAnswer(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.services.configDir = t.TempDir()
	tool := m.services.cfg.Tools["claude"]
	tool.Catalog, tool.CatalogCommand, tool.ModelArgs = "claude", "sh", "--model {model}"
	m.services.cfg.Tools["claude"] = tool
	if _, err := os.Stat(filepath.Join(m.services.configDir, "catalogs")); err == nil {
		t.Fatal("a fresh config dir holds catalogs")
	}
	if err := catalog.Keep(m.services.configDir, "claude", tool, claudeAnswer); err != nil {
		t.Fatal(err)
	}
	cmd := m.ensureCatalog("claude")
	if cmd == nil || m.ensureCatalog("claude") != nil {
		t.Fatal("the answer should be asked once")
	}
	m.applyCmd(t, cmd)
	openFormOnClaude(t, m)
	if list := m.form.Choice().Suggestions(m.spawnHost(), "claude"); len(list) != 3 {
		t.Fatalf("list from the kept answer = %+v", list)
	}
}

// A CLI that failed to answer says why in the model row, and the session
// launches on its defaults.
func TestFormLaunchesOnDefaultsWhenTheCLIFailsToAnswer(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, catalog.Catalog{})
	m.ledger.catalogs["claude"].err = errors.New("app-server exited (status 1)")
	openFormOnClaude(t, m)
	if body := formBody(m); !strings.Contains(body, "couldn't read models from claude: app-") || !strings.Contains(body, "exited (status 1)") {
		t.Fatalf("form:\n%s", body)
	}
	if slices.Contains(m.form.Fields(m.spawnHost()), spawn.FieldModel) {
		t.Fatal("a failed answer leaves nothing to pick")
	}
	picked, err := m.form.Choice().Launch(m.spawnHost(), "claude", "")
	if err != nil || config.Choice(picked) != (config.Choice{}) {
		t.Fatalf("launch choice = %+v, %v", picked, err)
	}
}

// A CLI that failed to answer is asked again after a short wait, not on
// every pass over its row.
func TestAFailedCatalogIsRetriedAfterAWait(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	answered(m, claudeLike, catalog.Catalog{})
	state := m.ledger.catalogs["claude"]
	state.err, state.checkedAt = errors.New("not logged in"), time.Now()
	if m.ensureCatalog("claude") != nil {
		t.Fatal("a failure just now was asked again")
	}
	state.checkedAt = time.Now().Add(-catalogRetry)
	if m.ensureCatalog("claude") == nil {
		t.Fatal("a failure half a minute old was not asked again")
	}
}

// The model list stops before it reads a kept answer, and the update dialog
// opens without taking a draft prompt's images.
func TestAnOlderCLIOpensTheUpdateDialogInsteadOfListingModels(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	m := buildModel(t)
	m.services.configDir = t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("#!/bin/sh\necho 0.84.2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool := config.Tool{
		Command:        "pi",
		Catalog:        "pi",
		CatalogCommand: "pi --mode rpc --no-session",
		MinimumVersion: "0.84.3",
	}
	m.services.cfg.Tools["pi"] = tool
	if err := catalog.Keep(m.services.configDir, "pi", tool, catalog.Catalog{Models: []catalog.Model{{ID: "kept"}}}); err != nil {
		t.Fatal(err)
	}
	m.form.prompt.attachments = []imageAttachment{{id: 7}}

	m.applyCmd(t, m.ensureCatalog("pi"))

	if m.mode != modeLaunchHint || !m.launchHint.fix.update || m.launchHint.fix.command != deps.Command("pi") {
		t.Fatalf("mode %v fix %+v catalog %+v", m.mode, m.launchHint.fix, m.ledger.catalogs["pi"])
	}
	if len(m.ledger.catalogs["pi"].cat.Models) != 0 {
		t.Fatalf("models = %+v, want the kept answer withheld", m.ledger.catalogs["pi"].cat.Models)
	}
	if len(m.form.prompt.attachments) != 1 || len(m.launchHint.fix.images) != 0 {
		t.Fatalf("draft images moved: form %v dialog %v", m.form.prompt.attachments, m.launchHint.fix.images)
	}
}
