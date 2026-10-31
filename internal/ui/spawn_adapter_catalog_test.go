package ui

import (
	"errors"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
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
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
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
	if list := m.form().Choice().Suggestions(m.spawnHost(), "claude"); len(list) != 3 {
		t.Fatalf("list from the kept answer = %+v", list)
	}
}

// A CLI that failed to answer says why in the model row, and the session
// launches on its defaults.
func TestFormLaunchesOnDefaultsWhenTheCLIFailsToAnswer(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, catalog.Catalog{})
	m.ledger.spawner.Catalogs["claude"].Err = errors.New("app-server exited (status 1)")
	openFormOnClaude(t, m)
	if body := formBody(m); !strings.Contains(body, "couldn't read models from claude: app-") || !strings.Contains(body, "exited (status 1)") {
		t.Fatalf("form:\n%s", body)
	}
	if slices.Contains(m.form().Fields(m.spawnHost()), spawn.FieldModel) {
		t.Fatal("a failed answer leaves nothing to pick")
	}
	picked, err := m.form().Choice().Launch(m.spawnHost(), "claude", "")
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
	state := m.ledger.spawner.Catalogs["claude"]
	state.Err, state.CheckedAt = errors.New("not logged in"), time.Now()
	if m.ensureCatalog("claude") != nil {
		t.Fatal("a failure just now was asked again")
	}
	state.CheckedAt = time.Now().Add(-uilaunch.CatalogRetry)
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
	m.form().Form = spawn.NewForm(m.spawnHost(), []string{"pi"}, 0)
	m.formPromptFor().Attachments = []composer.Attachment{{ID: 7}}

	m.applyCmd(t, m.ensureCatalog("pi"))

	if !hintOpen(m) || !hintFix(m).Update || hintFix(m).Command != deps.Command("pi") {
		t.Fatalf("mode %v fix %+v catalog %+v", m.screenID(), hintFix(m), m.ledger.spawner.Catalogs["pi"])
	}
	if len(m.ledger.spawner.Catalogs["pi"].Cat.Models) != 0 {
		t.Fatalf("models = %+v, want the kept answer withheld", m.ledger.spawner.Catalogs["pi"].Cat.Models)
	}
	if len(m.formPromptFor().Attachments) != 1 || len(hintFix(m).Images) != 0 {
		t.Fatalf("draft images moved: form %v dialog %v", m.formPromptFor().Attachments, hintFix(m).Images)
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
	m.form().Form = spawn.NewForm(m.spawnHost(), []string{"pi"}, 0)
	cmd := m.ensureCatalog("pi")
	if cmd == nil || m.ensureCatalog("pi") != nil {
		t.Fatal("the version probe should start once")
	}
	m.applyCmd(t, cmd)
	if !hintOpen(m) {
		t.Fatalf("mode %v, want the update dialog", m.screenID())
	}
	if cmd := hintKey(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatalf("esc returned %T", cmd)
	}
	if !formOpen(m) {
		t.Fatalf("esc left mode %v, want the form it came from", m.screenID())
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
	if hintOpen(m) || m.ledger.spawner.Catalogs["pi"].Err != nil {
		t.Fatalf("mode %v err %v, want the updated CLI listed", m.screenID(), m.ledger.spawner.Catalogs["pi"].Err)
	}
	if len(m.ledger.spawner.Catalogs["pi"].Cat.Models) != 1 {
		t.Fatalf("models = %+v", m.ledger.spawner.Catalogs["pi"].Cat.Models)
	}
}

// The update dialog opens over the quick bar. Esc leaves the draft there.
func TestTheUpdateDialogLeavesTheQuickBarUp(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	pushQuick(m)
	m.presentLaunchHint(uiinstall.Fix{Text: "pi 0.84.2 is older", Command: "true", Binary: "pi", Update: true})
	if !hintOpen(m) || (m.quick() == nil) {
		t.Fatalf("mode %v quick %v, want the dialog over the bar", m.screenID(), (m.quick() != nil))
	}
	if cmd := hintKey(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatalf("esc returned %T", cmd)
	}
	if !m.onList() || (m.quick() == nil) {
		t.Fatalf("esc left mode %v quick %v", m.screenID(), (m.quick() != nil))
	}
}

// A press while the update dialog is up is not leaving the list. The draft
// stays, images included, the same as Esc.
func TestAMousePressLeavesTheQuickDraftUnderTheUpdateDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	image := tempImage(t, "draft.png")
	pushQuick(m)
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
	m.presentLaunchHint(uiinstall.Fix{Text: "pi 0.84.2 is older", Command: "true", Binary: "pi", Update: true})

	m.handleMouse(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if !hintOpen(m) || (m.quick() == nil) {
		t.Fatalf("press left mode %v quick %v", m.screenID(), (m.quick() != nil))
	}
	if !imageExists(t, image) || len(m.quick().Attachments) != 1 {
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

	if hintOpen(m) {
		t.Fatal("a missing CLI opened the update dialog")
	}
	var missing config.MissingToolError
	if !errors.As(m.ledger.spawner.Catalogs["pi"].Err, &missing) || missing.Binary != "pi" {
		t.Fatalf("err = %v, want pi missing", m.ledger.spawner.Catalogs["pi"].Err)
	}
}

func TestOutdatedCatalogDoesNotReplaceAnotherInteraction(t *testing.T) {
	for _, form := range []bool{false, true} {
		m := buildModel(t)
		if form {
			pushForm(m).Form = spawn.NewForm(m.spawnHost(), []string{"claude"}, 0)
		}
		before := len(m.surfaces)
		m.ledger.spawner.Catalogs = map[string]*uilaunch.CatalogState{"pi": {Loading: true}}
		m.handleCatalog(uilaunch.CatalogMsg{Tool: "pi", Err: config.OutdatedToolError{Binary: "pi", Installed: "0.84.2", Minimum: "0.84.3"}})
		if len(m.surfaces) != before || hintOpen(m) || m.ledger.spawner.Catalogs["pi"].Err == nil {
			t.Fatalf("form %v: the stack changed or the catalog error was lost", form)
		}
	}
}

func TestClosingTheUpdateDialogReturnsToTheFormDraft(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "pi-tool")
	m.form().SetName("draft-name")
	m.formPromptFor().Attachments = []composer.Attachment{{ID: 7}}
	m.ledger.spawner.Catalogs = map[string]*uilaunch.CatalogState{"pi-tool": {Loading: true}}
	m.handleCatalog(uilaunch.CatalogMsg{Tool: "pi-tool", Err: config.OutdatedToolError{Binary: "pi", Installed: "0.84.2", Minimum: "0.84.3"}})
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the update dialog", m.screenID())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.applyCmd(t, cmd)
	if !formOpen(m) || m.form().Name() != "draft-name" || len(m.formPromptFor().Attachments) != 1 {
		t.Fatalf("mode %v name %q attachments %d, want the form draft back", m.screenID(), m.form().Name(), len(m.formPromptFor().Attachments))
	}
}
