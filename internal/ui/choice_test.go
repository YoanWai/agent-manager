package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	bubblekey "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var fiveLevels = []string{"low", "medium", "high", "xhigh", "max"}

// answered gives the test's claude the flags a catalog CLI has and the
// answer it would give, as if the manager had already asked.
func answered(m *Model, tool config.Tool, cat catalog.Catalog) {
	base := m.cfg.Tools["claude"]
	base.Catalog, base.CatalogCommand = tool.Catalog, tool.CatalogCommand
	base.ModelArgs, base.EffortArgs, base.ProfileArgs = tool.ModelArgs, tool.EffortArgs, tool.ProfileArgs
	m.cfg.Tools["claude"] = base
	m.catalogs = map[string]*catalogState{"claude": {cat: cat, loaded: true}}
}

var claudeLike = config.Tool{Catalog: "claude", CatalogCommand: "claude", ModelArgs: "--model {model}", EffortArgs: "--effort {effort}"}

var claudeAnswer = catalog.Catalog{Models: []catalog.Model{
	{ID: "default", Label: "Default (recommended)", Efforts: fiveLevels, Default: true},
	{ID: "opus", Label: "Opus 5.5", Efforts: fiveLevels},
	{ID: "haiku", Label: "Haiku 4.5"},
}}

func openFormOnClaude(t *testing.T, m *Model) {
	t.Helper()
	m.openForm()
	m.form.toolIndex = slices.Index(m.form.toolNames, "claude")
	m.form.choice = m.newChoice("claude")
}

func typeInto(m *Model, text string) {
	for _, r := range text {
		m.handleFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func formBody(m *Model) string { return ansi.Strip(m.viewForm()) }

// The effort row lists what the model in the model row takes, the CLI's
// default model until one is picked, and leaves the form for a model that
// takes none.
func TestFormEffortFollowsTheModel(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	if !slices.Contains(m.formFields(), fieldEffort) {
		t.Fatal("the default model's levels should show before any pick")
	}
	m.focusFormField(fieldModel)
	typeInto(m, "hai")
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.form.choice.model != "haiku" || m.form.choice.filter.Value() != "haiku" {
		t.Fatalf("tab filled in %q / %q", m.form.choice.model, m.form.choice.filter.Value())
	}
	if slices.Contains(m.formFields(), fieldEffort) || strings.Contains(formBody(m), "effort    ") {
		t.Fatalf("haiku takes no effort, the row should go:\n%s", formBody(m))
	}
	m.form.choice.filter.SetValue("")
	m.pickModel("claude", &m.form.choice, "opus")
	m.focusFormField(fieldEffort)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.choiceEffort("claude", &m.form.choice); got != "medium" {
		t.Fatalf("effort = %q, want medium", got)
	}
	if body := formBody(m); !strings.Contains(body, "effort    ◂ medium ▸ ") {
		t.Fatalf("effort row:\n%s", body)
	}
}

// A spawn launches on the picked model and effort, keeps them on its row,
// and remembers the model for the next list; a typed name the CLI does not
// list is refused.
func TestFormSpawnsOnTheChoice(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	m.form.name.SetValue("picked")
	m.form.dir.SetValue(t.TempDir())
	m.form.choice.filter.SetValue("opux")
	if m.submitForm(); m.errBar.text == "" || m.mode != modeForm {
		t.Fatalf("an unlisted model spawned: mode %v err %q", m.mode, m.errBar.text)
	}
	m.form.choice.filter.SetValue("opus")
	m.form.choice.effort = 3
	if _, _ = m.submitForm(); m.mode != modeList {
		t.Fatalf("submit: %q", m.errBar.text)
	}
	stored, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (config.Choice{Model: "opus", Effort: "high"}); stored.Choice != want {
		t.Fatalf("choice = %+v, want %+v", stored.Choice, want)
	}
	if recent := m.recentModels("claude"); !slices.Equal(recent, []string{"opus"}) {
		t.Fatalf("recent = %v", recent)
	}
	m.openForm()
	m.form.toolIndex = slices.Index(m.form.toolNames, "claude")
	m.form.choice = m.newChoice("claude")
	list := m.modelSuggestions("claude", &m.form.choice, "")
	if len(list) == 0 || !list[0].recent || list[0].model.ID != "opus" {
		t.Fatalf("the last pick should lead the list: %+v", list)
	}
}

// A CLI that reports nothing keeps both rows, saying so, and takes no keys
// on them.
func TestFormSaysWhatACLIDoesNotSupport(t *testing.T) {
	m := buildModel(t)
	openFormOnClaude(t, m)
	body := formBody(m)
	if !strings.Contains(body, "model     not supported by claude") || !strings.Contains(body, "effort    not supported by claude") {
		t.Fatalf("form:\n%s", body)
	}
	if fields := m.formFields(); slices.Contains(fields, fieldModel) || slices.Contains(fields, fieldEffort) {
		t.Fatalf("unsupported rows take focus: %v", fields)
	}
}

// Hermes lists profiles with their models, says a model reasons without
// naming levels, and routes each model through its provider.
func TestFormProfileScopesModelsAndTypedEffort(t *testing.T) {
	m := buildModel(t)
	work := []catalog.Model{{ID: "grok-4.6", Provider: "xai-oauth", Label: "xAI", EffortTyped: true}}
	answered(m, config.Tool{Catalog: "hermes", CatalogCommand: "hermes", ModelArgs: "--provider {provider} -m {model}", EffortArgs: "--reasoning {effort}", ProfileArgs: "-p {profile}"},
		catalog.Catalog{
			Models:   []catalog.Model{{ID: "claude-opus-5", Provider: "anthropic", Label: "Anthropic", Default: true}},
			Profiles: []catalog.Profile{{Name: "work", Detail: "grok-4.6 · xai-oauth", Models: work}},
		})
	openFormOnClaude(t, m)
	if slices.Contains(m.formFields(), fieldEffort) {
		t.Fatal("the active profile's default model does not reason, so no effort row")
	}
	m.focusFormField(fieldProfile)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if body := formBody(m); !strings.Contains(body, "profile   ◂ work  grok-4.6 · xai-oauth ▸") {
		t.Fatalf("profile row:\n%s", body)
	}
	m.focusFormField(fieldModel)
	typeInto(m, "grok")
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyTab})
	m.focusFormField(fieldEffort)
	typeInto(m, "high")
	if body := formBody(m); !strings.Contains(body, "typed · claude lists no levels") {
		t.Fatalf("typed effort row:\n%s", body)
	}
	picked, err := m.launchChoice("claude", &m.form.choice, m.form.choice.filter.Value())
	if err != nil {
		t.Fatal(err)
	}
	if want := (config.Choice{Provider: "xai-oauth", Model: "grok-4.6", Effort: "high", Profile: "work"}); picked != want {
		t.Fatalf("choice = %+v, want %+v", picked, want)
	}
}

// A click focuses a row, a second click on a stepping row steps it, and a
// click on a listed model picks it. Clicks land where the frame shows the
// text, and a click on the backdrop beside the card does nothing.
func TestFormClicks(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	click := func(x, y int) {
		m.handleMousePress(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}
	at := func(text string) (int, int) {
		t.Helper()
		frame := ansi.Strip(m.View())
		for y, line := range strings.Split(frame, "\n") {
			if col := strings.Index(line, text); col >= 0 {
				return ansi.StringWidth(line[:col]), y
			}
		}
		t.Fatalf("no %q in the frame:\n%s", text, frame)
		return 0, 0
	}
	x, y := at("model ")
	click(0, y)
	if m.form.focus == fieldModel {
		t.Fatal("a click on the backdrop focused the model row")
	}
	click(x, y)
	if m.form.focus != fieldModel || !m.form.choice.sugg.open {
		t.Fatalf("focus %d open %v", m.form.focus, m.form.choice.sugg.open)
	}
	click(at("opus  Opus 5.5"))
	if m.form.choice.model != "opus" {
		t.Fatalf("clicked entry picked %q", m.form.choice.model)
	}
	click(at("effort "))
	click(at("effort "))
	if got := m.choiceEffort("claude", &m.form.choice); got != "low" {
		t.Fatalf("second click stepped the effort to %q", got)
	}
}

// The quick prompt picks the model from a list typing narrows, steps the
// effort, and spawns on both.
func TestQuickPromptSpawnsOnItsChoice(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	dir := t.TempDir()
	if err := m.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	m.quick.toolIndex = slices.Index(m.quick.toolNames, "claude")
	m.quick.choice = m.newChoice("claude")
	ctrl := func(key tea.KeyType) { m.handleQuickKey(tea.KeyMsg{Type: key}) }
	ctrl(tea.KeyCtrlL)
	if m.quick.picking != pickModel {
		t.Fatal("ctrl+l should open the model list")
	}
	for _, r := range "opu" {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.quick.input.Value() != "" {
		t.Fatalf("typing into the list reached the prompt: %q", m.quick.input.Value())
	}
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.quick.picking != pickNone || m.quick.choice.model != "opus" {
		t.Fatalf("picking %d model %q", m.quick.picking, m.quick.choice.model)
	}
	ctrl(tea.KeyCtrlX)
	ctrl(tea.KeyCtrlX)
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "model: opus") || !strings.Contains(footer, "effort: medium") {
		t.Fatalf("footer: %s", footer)
	}
	m.quick.input.SetValue("do the work")
	if _, _ = m.submitQuick(); m.errBar.text != "" {
		t.Fatalf("spawn: %q", m.errBar.text)
	}
	var spawned []config.Choice
	for _, sess := range m.sessionRows() {
		stored, err := m.store.Get(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		spawned = append(spawned, stored.Choice)
	}
	if !slices.Contains(spawned, config.Choice{Model: "opus", Effort: "medium"}) {
		t.Fatalf("spawned choices = %+v", spawned)
	}
}

// A click on the bar's model stretch opens the list, and one on an entry
// picks it.
func TestQuickPromptClicks(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	if err := m.store.CreateGroup("work", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	m.quick.toolIndex = slices.Index(m.quick.toolNames, "claude")
	m.quick.choice = m.newChoice("claude")
	click := func(action, entry int) {
		t.Helper()
		m.View()
		for _, hit := range m.quick.hits {
			if hit.action == action && (entry < 0 || hit.entry == entry) {
				m.handleMousePress(tea.MouseMsg{X: m.quick.originX + hit.x0, Y: m.quick.originY + hit.line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				return
			}
		}
		t.Fatalf("no stretch for action %d entry %d in %+v", action, entry, m.quick.hits)
	}
	click(quickClickModel, -1)
	if m.quick.picking != pickModel {
		t.Fatal("the model stretch should open the list")
	}
	click(quickClickEntry, 1)
	if m.quick.picking != pickNone || m.quick.choice.model != "opus" {
		t.Fatalf("picking %d model %q", m.quick.picking, m.quick.choice.model)
	}
}

// The first need for a CLI's rows reads the kept answer, off the update
// path, and a fresh one is not asked again.
func TestCatalogIsReadFromTheKeptAnswer(t *testing.T) {
	m := buildModel(t)
	m.configDir = t.TempDir()
	tool := m.cfg.Tools["claude"]
	tool.Catalog, tool.CatalogCommand, tool.ModelArgs = "claude", "sh", "--model {model}"
	m.cfg.Tools["claude"] = tool
	if _, err := os.Stat(filepath.Join(m.configDir, "catalogs")); err == nil {
		t.Fatal("a fresh config dir holds catalogs")
	}
	if err := catalog.Keep(m.configDir, "claude", tool, claudeAnswer); err != nil {
		t.Fatal(err)
	}
	cmd := m.ensureCatalog("claude")
	if cmd == nil || m.ensureCatalog("claude") != nil {
		t.Fatal("the answer should be asked once")
	}
	m.applyCmd(t, cmd)
	openFormOnClaude(t, m)
	if list := m.modelSuggestions("claude", &m.form.choice, ""); len(list) != 3 {
		t.Fatalf("list from the kept answer = %+v", list)
	}
}

// A CLI that failed to answer says why in the model row, and the session
// launches on its defaults.
func TestFormLaunchesOnDefaultsWhenTheCLIFailsToAnswer(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, catalog.Catalog{})
	m.catalogs["claude"].err = errors.New("app-server exited (status 1)")
	openFormOnClaude(t, m)
	if body := formBody(m); !strings.Contains(body, "couldn't read models from claude: app-server") || !strings.Contains(body, "exited (status 1)") {
		t.Fatalf("form:\n%s", body)
	}
	if slices.Contains(m.formFields(), fieldModel) {
		t.Fatal("a failed answer leaves nothing to pick")
	}
	picked, err := m.launchChoice("claude", &m.form.choice, "")
	if err != nil || picked != (config.Choice{}) {
		t.Fatalf("launch choice = %+v, %v", picked, err)
	}
}

// A CLI that failed to answer is asked again after a short wait, not on
// every pass over its row.
func TestAFailedCatalogIsRetriedAfterAWait(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, catalog.Catalog{})
	state := m.catalogs["claude"]
	state.err, state.checkedAt = errors.New("not logged in"), time.Now()
	if m.ensureCatalog("claude") != nil {
		t.Fatal("a failure just now was asked again")
	}
	state.checkedAt = time.Now().Add(-catalogRetry)
	if m.ensureCatalog("claude") == nil {
		t.Fatal("a failure half a minute old was not asked again")
	}
}

// Answering a session sends no new launch, so the choice keys say so
// rather than opening a list nothing shows.
func TestQuickChoiceKeysNeedAGroupTarget(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	createSession(t, m, "worker", t.TempDir(), "")
	m.selectSessionRow(t, "worker")
	m.openQuickMode()
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.quick.picking != pickNone || m.errBar.text != quickChoiceHint {
		t.Fatalf("picking %d err %q", m.quick.picking, m.errBar.text)
	}
	if footer := ansi.Strip(m.viewFooter()); strings.Contains(footer, quickModelKey) {
		t.Fatalf("footer names a key that does nothing here: %s", footer)
	}
}

// A list that shrinks under the highlight, from a newer answer or another
// profile, leaves enter and tab picking a model it still lists.
func TestModelListShrinkingUnderTheHighlight(t *testing.T) {
	down := tea.KeyMsg{Type: tea.KeyDown}
	t.Run("form, newer answer", func(t *testing.T) {
		m := buildModel(t)
		answered(m, claudeLike, claudeAnswer)
		openFormOnClaude(t, m)
		m.focusFormField(fieldModel)
		for range 3 {
			m.handleFormKey(down)
		}
		m.handleCatalog(catalogMsg{tool: "claude", cat: catalog.Catalog{Models: claudeAnswer.Models[:1]}})
		m.handleFormKey(tea.KeyMsg{Type: tea.KeyTab})
		if m.form.choice.model != "default" {
			t.Fatalf("picked %q", m.form.choice.model)
		}
	})
	t.Run("quick prompt, another profile", func(t *testing.T) {
		m := buildModel(t)
		answered(m, config.Tool{Catalog: "hermes", CatalogCommand: "hermes", ModelArgs: "-m {model}", ProfileArgs: "-p {profile}"},
			catalog.Catalog{
				Models:   claudeAnswer.Models,
				Profiles: []catalog.Profile{{Name: "work", Models: []catalog.Model{{ID: "grok-4.6", Default: true}}}},
			})
		if err := m.store.CreateGroup("work", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		m.applyCmd(t, m.refreshCmd())
		m.selectGroupRow(t, "work")
		m.openQuickMode()
		m.quick.toolIndex = slices.Index(m.quick.toolNames, "claude")
		m.quick.choice = m.newChoice("claude")
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlL})
		for range 3 {
			m.handleQuickKey(down)
		}
		m.stepQuickProfile()
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
		if m.quick.choice.model != "grok-4.6" {
			t.Fatalf("picked %q", m.quick.choice.model)
		}
	})
}

// An open model list taller than the split's column gives up its top rows,
// the way the full layout's does, so the prompt stays on screen.
func TestQuickModelListKeepsThePromptOnScreen(t *testing.T) {
	m := buildModel(t)
	var models []catalog.Model
	for i := range 12 {
		models = append(models, catalog.Model{ID: fmt.Sprintf("model-%02d", i)})
	}
	answered(m, claudeLike, catalog.Catalog{Models: models})
	if err := m.store.SetSetting(recentModelsKey("claude"), `["model-00","model-01","model-02"]`); err != nil {
		t.Fatal(err)
	}
	if err := m.store.CreateGroup("work", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.width, m.height = 80, 20
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	m.quick.toolIndex = slices.Index(m.quick.toolNames, "claude")
	m.quick.choice = m.newChoice("claude")
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.fullRows() {
		t.Fatal("80 columns should give the split")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "type and press enter") {
		t.Fatalf("the prompt fell off the column:\n%s", view)
	}
	var last quickHit
	for _, hit := range m.quick.hits {
		if hit.action == quickClickEntry {
			last = hit
		}
	}
	want := m.modelSuggestions("claude", &m.quick.choice, "")[last.entry].model.ID
	m.handleMousePress(tea.MouseMsg{X: m.quick.originX + last.x0, Y: m.quick.originY + last.line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.quick.choice.model != want {
		t.Fatalf("a click on the list's last entry picked %q, want %q", m.quick.choice.model, want)
	}
}

// The arrows reach every model the CLI lists, scrolling the list under a
// scroll bar, and a filter typed and then deleted brings them all back.
func TestFormModelListScrollsThroughEveryModel(t *testing.T) {
	m := buildModel(t)
	var models []catalog.Model
	for i := range 12 {
		models = append(models, catalog.Model{ID: fmt.Sprintf("model-%02d", i)})
	}
	answered(m, claudeLike, catalog.Catalog{Models: models})
	openFormOnClaude(t, m)
	m.focusFormField(fieldModel)
	for i := range models {
		m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
		body := formBody(m)
		if !strings.Contains(body, "❯ "+models[i].ID) {
			t.Fatalf("down %d: the highlighted %s is off screen:\n%s", i+1, models[i].ID, body)
		}
		if !strings.Contains(body, "┃") {
			t.Fatalf("down %d: no scroll bar:\n%s", i+1, body)
		}
	}
	if m.form.focus != fieldModel {
		t.Fatal("the last model moved focus off the list")
	}
	typeInto(m, "x")
	if body := formBody(m); !strings.Contains(body, "no model matches x") {
		t.Fatalf("filtered:\n%s", body)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyBackspace})
	body := formBody(m)
	if !m.form.choice.sugg.open || !strings.Contains(body, "from claude · 12 models") || !strings.Contains(body, "model-00") {
		t.Fatalf("clearing the filter should show every model again:\n%s", body)
	}
}

// The spawn choice keys are control keys, since alt never arrives from many
// terminals, and none of them is a key the prompt's editor, the model
// filter, the manager's own tables, the tty or a common multiplexer takes.
func TestQuickChoiceKeysOverlapNothing(t *testing.T) {
	taken := map[string]string{}
	takeKeyMap := func(owner string, keyMap any) {
		fields := reflect.ValueOf(keyMap)
		for i := range fields.NumField() {
			if binding, ok := fields.Field(i).Interface().(bubblekey.Binding); ok {
				for _, name := range binding.Keys() {
					taken[name] = owner
				}
			}
		}
	}
	takeKeyMap("the prompt's editor", textarea.DefaultKeyMap)
	takeKeyMap("the model filter", textinput.DefaultKeyMap)
	for _, table := range []keybind.Table{keybind.DefaultList(), keybind.DefaultSession()} {
		for _, action := range table.Actions() {
			for _, bound := range table.Binding(action.Name).Keys() {
				taken[bound.Tea()] = table.Scope() + " " + action.Name
			}
		}
	}
	for owner, names := range map[string][]string{
		"the manager":         {"ctrl+c", "ctrl+v", "ctrl+h", "ctrl+d", "ctrl+u", "ctrl+t", "tab", "shift+tab", "enter", "esc"},
		"the tty":             {"ctrl+z", "ctrl+s", "ctrl+q", `ctrl+\`, "ctrl+i", "ctrl+m", "ctrl+j", "ctrl+[", "ctrl+@"},
		"tmux or screen":      {"ctrl+b", "ctrl+a"},
		"zellij":              {"ctrl+b", "ctrl+c", "ctrl+f", "ctrl+g", "ctrl+h", "ctrl+n", "ctrl+o", "ctrl+p", "ctrl+q", "ctrl+s", "ctrl+t"},
		"the vscode terminal": {"ctrl+p", "ctrl+g"},
	} {
		for _, name := range names {
			taken[name] = owner
		}
	}
	for _, choiceKey := range []string{quickModelKey, quickEffortKey, quickProfileKey} {
		if !strings.HasPrefix(choiceKey, "ctrl+") {
			t.Errorf("%s is not a control key", choiceKey)
		}
		if owner, ok := taken[choiceKey]; ok {
			t.Errorf("%s is already %s's", choiceKey, owner)
		}
	}
}

// A picked model shows in the field without narrowing the list: coming
// back to the row lists every model again, the pick highlighted.
func TestFormModelListOpensOnEveryModelWithThePickHighlighted(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	m.focusFormField(fieldModel)
	typeInto(m, "opus")
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.form.choice.model != "opus" {
		t.Fatalf("picked %q", m.form.choice.model)
	}
	m.focusFormField(fieldDir)
	m.focusFormField(fieldModel)
	body := formBody(m)
	for _, model := range claudeAnswer.Models {
		if !strings.Contains(body, model.ID) {
			t.Fatalf("%s missing from the reopened list:\n%s", model.ID, body)
		}
	}
	if !strings.Contains(body, "❯ opus") {
		t.Fatalf("the pick is not highlighted:\n%s", body)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.form.choice.model != "haiku" {
		t.Fatalf("down from the pick picked %q", m.form.choice.model)
	}
}
