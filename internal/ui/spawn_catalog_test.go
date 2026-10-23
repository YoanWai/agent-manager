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
	tea "github.com/charmbracelet/bubbletea"
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
	m.openForm()
	m.form.Form = spawn.NewForm(m.spawnHost(), []string{"pi"}, 0)
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

// Closing the dialog and updating the CLI lists models on the next open.
// A fetch failure waits half a minute. An old CLI does not.
func TestAnUpdatedCLIIsAskedAgainRightAway(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	m := buildModel(t)
	m.services.configDir = t.TempDir()
	bin := t.TempDir()
	script := filepath.Join(bin, "pi")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 0.84.2\n"), 0o755); err != nil {
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
	kept := catalog.Catalog{Models: []catalog.Model{{ID: "kept"}}}
	if err := catalog.Keep(m.services.configDir, "pi", tool, kept); err != nil {
		t.Fatal(err)
	}
	m.openForm()
	m.form.Form = spawn.NewForm(m.spawnHost(), []string{"pi"}, 0)
	cmd := m.ensureCatalog("pi")
	if cmd == nil || m.ensureCatalog("pi") != nil {
		t.Fatal("the version probe should start once")
	}
	m.applyCmd(t, cmd)
	if m.mode != modeLaunchHint {
		t.Fatalf("mode %v, want the update dialog", m.mode)
	}
	if cmd := m.launchHint.handleKey(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatalf("esc returned %T", cmd)
	}
	if m.mode != modeForm {
		t.Fatalf("esc left mode %v, want the form it came from", m.mode)
	}

	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 0.85.0 updated\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Keep(m.services.configDir, "pi", tool, kept); err != nil {
		t.Fatal(err)
	}
	cmd = m.ensureCatalog("pi")
	if cmd == nil {
		t.Fatal("an old CLI was not asked again after the dialog closed")
	}
	m.applyCmd(t, cmd)
	if m.mode == modeLaunchHint || m.ledger.catalogs["pi"].err != nil {
		t.Fatalf("mode %v err %v, want the updated CLI listed", m.mode, m.ledger.catalogs["pi"].err)
	}
	if len(m.ledger.catalogs["pi"].cat.Models) != 1 {
		t.Fatalf("models = %+v", m.ledger.catalogs["pi"].cat.Models)
	}
}

// The update dialog opens over the quick bar. Esc leaves the draft there.
func TestTheUpdateDialogLeavesTheQuickBarUp(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.quick.active = true
	m.launchHint.present(m, launchFix{text: "pi 0.84.2 is older", command: "true", binary: "pi", update: true})
	if m.mode != modeLaunchHint || !m.quick.active {
		t.Fatalf("mode %v quick %v, want the dialog over the bar", m.mode, m.quick.active)
	}
	if cmd := m.launchHint.handleKey(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatalf("esc returned %T", cmd)
	}
	if m.mode != modeList || !m.quick.active {
		t.Fatalf("esc left mode %v quick %v", m.mode, m.quick.active)
	}
}

// A press while the update dialog is up is not leaving the list. The draft
// stays, images included, the same as Esc.
func TestAMousePressLeavesTheQuickDraftUnderTheUpdateDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	image := tempImage(t, "draft.png")
	m.quick.active = true
	m.quick.attachments = []imageAttachment{{id: 1, path: image}}
	m.launchHint.present(m, launchFix{text: "pi 0.84.2 is older", command: "true", binary: "pi", update: true})

	m.handleMouse(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if m.mode != modeLaunchHint || !m.quick.active {
		t.Fatalf("press left mode %v quick %v", m.mode, m.quick.active)
	}
	if !imageExists(t, image) || len(m.quick.attachments) != 1 {
		t.Fatal("a press under the update dialog deleted the draft image")
	}
}

// A CLI that is not installed is the install error, not an old version.
func TestAMissingCLIWithAFloorDoesNotOpenTheUpdateDialog(t *testing.T) {
	m := buildModel(t)
	t.Setenv("PATH", t.TempDir())
	m.services.cfg.Tools["pi"] = config.Tool{
		Command:        "pi",
		Catalog:        "pi",
		CatalogCommand: "pi --mode rpc --no-session",
		MinimumVersion: "0.84.3",
	}

	m.applyCmd(t, m.ensureCatalog("pi"))

	if m.mode == modeLaunchHint {
		t.Fatal("a missing CLI opened the update dialog")
	}
	var missing config.MissingToolError
	if !errors.As(m.ledger.catalogs["pi"].err, &missing) || missing.Binary != "pi" {
		t.Fatalf("err = %v, want pi missing", m.ledger.catalogs["pi"].err)
	}
}

func TestOutdatedCatalogDoesNotReplaceAnotherInteraction(t *testing.T) {
	for _, mode := range []mode{modeList, modeSettings, modeForm} {
		m := buildModel(t)
		m.form.Form = spawn.NewForm(m.spawnHost(), []string{"claude"}, 0)
		m.mode = mode
		m.ledger.catalogs = map[string]*catalogState{"pi": {loading: true}}
		m.handleCatalog(catalogMsg{tool: "pi", err: config.OutdatedToolError{Binary: "pi", Installed: "0.84.2", Minimum: "0.84.3"}})
		if m.mode != mode || m.ledger.catalogs["pi"].err == nil {
			t.Fatalf("mode %v became %v or lost the catalog error", mode, m.mode)
		}
	}
}

func TestClosingTheUpdateDialogReturnsToTheFormDraft(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "pi-tool")
	m.form.SetName("draft-name")
	m.form.prompt.attachments = []imageAttachment{{id: 7}}
	m.ledger.catalogs = map[string]*catalogState{"pi-tool": {loading: true}}
	m.handleCatalog(catalogMsg{tool: "pi-tool", err: config.OutdatedToolError{Binary: "pi", Installed: "0.84.2", Minimum: "0.84.3"}})
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want the update dialog", m.mode)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.applyCmd(t, cmd)
	if m.mode != modeForm || m.form.Name() != "draft-name" || len(m.form.prompt.attachments) != 1 {
		t.Fatalf("mode %v name %q attachments %d, want the form draft back", m.mode, m.form.Name(), len(m.form.prompt.attachments))
	}
}
