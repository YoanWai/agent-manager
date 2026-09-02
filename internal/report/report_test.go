package report

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

// fakeCommands answers the commands a report shells out to, keyed by the
// command line, and records the gh call that would post.
type fakeCommands struct {
	answers map[string]string
	failing map[string]error
	ghPath  error
	ran     [][]string
}

func swapSeams(t *testing.T, fake *fakeCommands) {
	t.Helper()
	oldLookPath, oldRun, oldGoos, oldProc := lookPath, runCommand, goos, procVersion
	lookPath = func(string) (string, error) { return "/usr/bin/gh", fake.ghPath }
	runCommand = func(name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		fake.ran = append(fake.ran, append([]string{name}, args...))
		if err, failed := fake.failing[line]; failed {
			return "", err
		}
		if answer, known := fake.answers[line]; known {
			return answer, nil
		}
		return "", errors.New("no such command: " + line)
	}
	goos = "darwin"
	procVersion = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { lookPath, runCommand, goos, procVersion = oldLookPath, oldRun, oldGoos, oldProc })
}

func loggedIn() *fakeCommands {
	return &fakeCommands{
		answers: map[string]string{
			"tmux -V":                 "tmux 3.5a",
			"claude --version":        "2.4.1 (Claude Code)\nextra line",
			"gh api user --jq .login": "yoan",
			"gh issue create --repo " + Repo + " --title Space lands in the wrong pane --body " + bugBody() + " --label bug": "https://github.com/" + Repo + "/issues/512",
		},
		failing: map[string]error{},
	}
}

func bugBody() string {
	return "### What happened\n\n1. Pressed space\n2. Prompt landed in the first session\n\n" +
		"### agent-manager version\n\n0.31.0\n\n" +
		"### Operating system\n\nmacOS\n\n" +
		"### tmux version\n\ntmux 3.5a\n\n" +
		"### Which agent CLI\n\nClaude Code\n\n" +
		"### Agent CLI version\n\n2.4.1 (Claude Code)\n"
}

func bugDraft() Draft {
	return Draft{Kind: Bug, Title: "Space lands in the wrong pane", Body: "1. Pressed space\n2. Prompt landed in the first session\n"}
}

// workspace seeds a store with a claude session so the report can read the
// calling session's tool the way the real manager stores it.
func workspace(t *testing.T) (string, string) {
	t.Helper()
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	defer st.Close()
	if err := st.CreateSession(store.Session{ID: "cafe0001", Name: "api-worker", Tool: "claude", Cwd: configDir}); err != nil {
		t.Fatalf("create session row: %v", err)
	}
	return configDir, "cafe0001"
}

func TestPreviewComposesTheBugFormAndPostsNothing(t *testing.T) {
	fake := loggedIn()
	swapSeams(t, fake)
	configDir, sessionID := workspace(t)

	preview, err := New(configDir, "0.31.0").Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Body != bugBody() {
		t.Fatalf("body =\n%s\nwant\n%s", preview.Body, bugBody())
	}
	if preview.Route != RouteGH || preview.Account != "yoan" || preview.Reason != "" {
		t.Fatalf("route = %q as %q (%q)", preview.Route, preview.Account, preview.Reason)
	}
	if preview.Labels[0] != "bug" || preview.Title != "Space lands in the wrong pane" {
		t.Fatalf("labels %v title %q", preview.Labels, preview.Title)
	}
	want := Context{Version: "0.31.0", OS: "macOS", Tmux: "tmux 3.5a", Tool: "Claude Code", ToolVersion: "2.4.1 (Claude Code)"}
	if preview.Context != want {
		t.Fatalf("context = %+v, want %+v", preview.Context, want)
	}
	for _, ran := range fake.ran {
		if ran[0] == "gh" && ran[1] == "issue" {
			t.Fatalf("a preview posted: %v", ran)
		}
	}
}

func TestFilePostsThroughGHAndReturnsTheIssue(t *testing.T) {
	fake := loggedIn()
	swapSeams(t, fake)
	configDir, sessionID := workspace(t)
	reporter := New(configDir, "0.31.0")

	preview, err := reporter.Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	filed, err := reporter.File(sessionID, bugDraft(), preview.ID)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if filed.Route != RouteGH || filed.URL != "https://github.com/"+Repo+"/issues/512" {
		t.Fatalf("filed = %+v", filed)
	}
}

// An approval covers one issue on one route as one account. Between the two
// calls gh can appear, log in, or log in as somebody else, and each of those
// turns the outcome the user approved into a different one, so the id pins
// all three and filing is refused rather than guessing the user meant it.
func TestFilingRefusesAnApprovalThatNoLongerFitsTheOutcome(t *testing.T) {
	loggedOut := loggedIn()
	loggedOut.ghPath = errors.New("not found")
	swapSeams(t, loggedOut)
	configDir, sessionID := workspace(t)

	browsed, err := New(configDir, "0.31.0").Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if browsed.Route != RouteBrowser {
		t.Fatalf("route = %q", browsed.Route)
	}

	// gh arrives between the approval and the filing: what the user approved
	// posted nothing, so the id no longer fits and nothing is posted.
	installed := loggedIn()
	swapSeams(t, installed)
	_, err = New(configDir, "0.31.0").File(sessionID, bugDraft(), browsed.ID)
	if err == nil || !strings.Contains(err.Error(), "nothing was filed") || !strings.Contains(err.Error(), browsed.ID) {
		t.Fatalf("File on a changed route = %v", err)
	}
	for _, ran := range installed.ran {
		if ran[0] == "gh" && ran[1] == "issue" {
			t.Fatalf("a stale approval posted: %v", ran)
		}
	}

	// Same issue, same route, another account: also refused.
	other := loggedIn()
	other.answers["gh api user --jq .login"] = "someone-else"
	swapSeams(t, other)
	fitting, err := New(configDir, "0.31.0").Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	swapSeams(t, loggedIn())
	if _, err := New(configDir, "0.31.0").File(sessionID, bugDraft(), fitting.ID); err == nil {
		t.Fatal("an approval given for another account filed")
	}

	// And a body edited after the approval never rides an old id in.
	fake := loggedIn()
	swapSeams(t, fake)
	reporter := New(configDir, "0.31.0")
	approved, err := reporter.Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	edited := bugDraft()
	edited.Body += "\nand my token is hunter2"
	if _, err := reporter.File(sessionID, edited, approved.ID); err == nil {
		t.Fatal("an approval given for one body filed another")
	}
}

func TestFilingWithoutAnApprovalIsRefused(t *testing.T) {
	fake := loggedIn()
	swapSeams(t, fake)
	configDir, sessionID := workspace(t)
	if _, err := New(configDir, "0.31.0").File(sessionID, bugDraft(), "  "); err == nil ||
		!strings.Contains(err.Error(), "needs the id of the preview the user approved") {
		t.Fatalf("File without a preview id = %v", err)
	}
	if len(fake.ran) != 0 {
		t.Fatalf("filing without an approval ran commands: %v", fake.ran)
	}
}

func TestFileReportsAGHFailure(t *testing.T) {
	fake := loggedIn()
	for line := range fake.answers {
		if strings.HasPrefix(line, "gh issue create") {
			fake.failing[line] = errors.New("exit status 1: GraphQL: Resource not accessible")
		}
	}
	swapSeams(t, fake)
	configDir, sessionID := workspace(t)

	reporter := New(configDir, "0.31.0")
	preview, err := reporter.Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	_, err = reporter.File(sessionID, bugDraft(), preview.ID)
	if err == nil || !strings.Contains(err.Error(), "gh issue create: ") || !strings.Contains(err.Error(), "Resource not accessible") {
		t.Fatalf("File with a failing gh = %v", err)
	}
}

func TestWithoutGHTheBrowserRouteHandsBackThePrefilledForm(t *testing.T) {
	for _, route := range []struct {
		name   string
		fake   *fakeCommands
		reason string
	}{
		{"not installed", func() *fakeCommands {
			fake := loggedIn()
			fake.ghPath = errors.New("not found")
			return fake
		}(), "gh is not installed"},
		{"not logged in", func() *fakeCommands {
			fake := loggedIn()
			fake.failing["gh api user --jq .login"] = errors.New("exit status 4: You are not logged into any GitHub hosts")
			return fake
		}(), "gh is installed but cannot reach GitHub as a logged-in account: exit status 4: You are not logged into any GitHub hosts"},
	} {
		t.Run(route.name, func(t *testing.T) {
			swapSeams(t, route.fake)
			configDir, sessionID := workspace(t)
			reporter := New(configDir, "0.31.0")

			preview, err := reporter.Preview(sessionID, bugDraft())
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if preview.Route != RouteBrowser || preview.Reason != route.reason || preview.Account != "" {
				t.Fatalf("route = %q (%q) as %q", preview.Route, preview.Reason, preview.Account)
			}
			filed, err := reporter.File(sessionID, bugDraft(), preview.ID)
			if err != nil {
				t.Fatalf("File: %v", err)
			}
			if filed.Route != RouteBrowser || filed.URL != preview.URL {
				t.Fatalf("filed = %+v, preview url %q", filed, preview.URL)
			}
			for _, ran := range route.fake.ran {
				if ran[0] == "gh" && ran[1] == "issue" {
					t.Fatalf("the browser route posted: %v", ran)
				}
			}
		})
	}
}

// The form url fills the issue form's own fields, whose ids come from
// .github/ISSUE_TEMPLATE, so the user opening it finds the preview's
// contents in place rather than a body to paste.
func TestFormURLFillsTheTemplateFields(t *testing.T) {
	gathered := Context{Version: "0.31.0", OS: "macOS", Tmux: "tmux 3.5a", Tool: "Claude Code", ToolVersion: "2.4.1"}
	parsed, err := url.Parse(FormURL(bugDraft(), gathered))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/"+Repo+"/issues/new" {
		t.Fatalf("path = %q", parsed.Path)
	}
	query := parsed.Query()
	for field, want := range map[string]string{
		"template":      "bug_report.yml",
		"title":         "Space lands in the wrong pane",
		"what-happened": "1. Pressed space\n2. Prompt landed in the first session",
		"version":       "0.31.0",
		"os":            "macOS",
		"tmux-version":  "tmux 3.5a",
		"tool":          "Claude Code",
		"tool-version":  "2.4.1",
	} {
		if got := query.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}

	feature := Draft{Kind: Feature, Title: "Show overlapping files", Body: "I keep several agents on one repo."}
	parsed, err = url.Parse(FormURL(feature, Context{Version: "0.31.0", OS: "Linux"}))
	if err != nil {
		t.Fatal(err)
	}
	query = parsed.Query()
	for field, want := range map[string]string{
		"template": "feature_request.yml",
		"title":    "Show overlapping files",
		"problem":  "I keep several agents on one repo.",
		"extra":    "agent-manager 0.31.0 on Linux.",
	} {
		if got := query.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if query.Has("what-happened") || query.Has("version") {
		t.Fatalf("a feature request carries bug fields: %v", query)
	}
}

func TestBlankContextFieldsLeaveNoEmptyHeading(t *testing.T) {
	body := Compose(bugDraft(), Context{Version: "dev", OS: "Linux"})
	if strings.Contains(body, "tmux version") || strings.Contains(body, "Which agent CLI") {
		t.Fatalf("empty context rendered a heading:\n%s", body)
	}
	if !strings.HasSuffix(body, "### Operating system\n\nLinux\n") {
		t.Fatalf("body ends %q", body)
	}
}

func TestAFeatureRequestSaysWhereItCameFrom(t *testing.T) {
	body := Compose(Draft{Kind: Feature, Title: "t", Body: "  I want a panel.  "}, Context{Version: "0.31.0", OS: "macOS", Tool: "Codex"})
	want := "### What are you trying to do\n\nI want a panel.\n\n### Anything else\n\nagent-manager 0.31.0 on macOS, reported from a Codex session.\n"
	if body != want {
		t.Fatalf("body =\n%s\nwant\n%s", body, want)
	}
}

func TestADraftMissingItsPartsIsRefusedBeforeAnythingRuns(t *testing.T) {
	fake := loggedIn()
	swapSeams(t, fake)
	configDir, sessionID := workspace(t)
	reporter := New(configDir, "0.31.0")
	for _, refused := range []struct {
		draft Draft
		want  string
	}{
		{Draft{Kind: "question", Title: "t", Body: "b"}, `kind "question" is not one of bug, feature`},
		{Draft{Kind: Bug, Title: "  ", Body: "b"}, "title is empty"},
		{Draft{Kind: Bug, Title: "t", Body: " "}, "body is empty; say what you did"},
		{Draft{Kind: Feature, Title: "t", Body: ""}, "body is empty; say what you are trying to do"},
	} {
		_, err := reporter.Preview(sessionID, refused.draft)
		if err == nil || !strings.Contains(err.Error(), refused.want) {
			t.Errorf("Preview(%+v) = %v, want %q", refused.draft, err, refused.want)
		}
		if _, err := reporter.File(sessionID, refused.draft, "whatever"); err == nil {
			t.Errorf("File(%+v) filed a refused draft", refused.draft)
		}
	}
	if len(fake.ran) != 0 {
		t.Fatalf("a refused draft ran commands: %v", fake.ran)
	}
}

// A report from a plain shell carries no session, and one from a session
// the store no longer holds is refused rather than filed without saying
// which CLI it came from.
func TestSessionContextIsOptionalButNeverWrong(t *testing.T) {
	fake := loggedIn()
	swapSeams(t, fake)
	configDir, _ := workspace(t)
	reporter := New(configDir, "0.31.0")

	preview, err := reporter.Preview("", bugDraft())
	if err != nil {
		t.Fatalf("Preview without a session: %v", err)
	}
	if preview.Context.Tool != "" || preview.Context.ToolVersion != "" || preview.Context.Tmux != "tmux 3.5a" {
		t.Fatalf("context without a session = %+v", preview.Context)
	}
	_, err = reporter.Preview("gone0000", bugDraft())
	if err == nil || !strings.Contains(err.Error(), "calling session gone0000 no longer exists") {
		t.Fatalf("Preview from a missing session = %v", err)
	}
}

func TestAToolThatAnswersNoVersionLeavesTheFieldBlank(t *testing.T) {
	fake := loggedIn()
	fake.failing["claude --version"] = errors.New("exit status 2")
	swapSeams(t, fake)
	configDir, sessionID := workspace(t)

	preview, err := New(configDir, "0.31.0").Preview(sessionID, bugDraft())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Context.Tool != "Claude Code" || preview.Context.ToolVersion != "" {
		t.Fatalf("context = %+v", preview.Context)
	}
}

func TestTheOperatingSystemIsSpelledLikeTheFormDropdown(t *testing.T) {
	swapSeams(t, loggedIn())
	wsl := filepath.Join(t.TempDir(), "version")
	if err := os.WriteFile(wsl, []byte("Linux version 5.15.167.4-microsoft-standard-WSL2"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []struct {
		goos, proc, want string
	}{
		{"darwin", "", "macOS"},
		{"linux", "", "Linux"},
		{"linux", wsl, "Windows (WSL2)"},
		{"freebsd", "", "freebsd"},
	} {
		goos = platform.goos
		if platform.proc != "" {
			procVersion = platform.proc
		}
		if got := operatingSystem(); got != platform.want {
			t.Errorf("%s with %q = %q, want %q", platform.goos, platform.proc, got, platform.want)
		}
	}
}

func TestToolLabelsMatchTheFormDropdown(t *testing.T) {
	for tool, want := range map[string]string{
		"claude": "Claude Code", "codex": "Codex", "opencode": "OpenCode", "grok": "Grok Build",
		"gemini": "Gemini CLI", "pi": "Pi", "hermes": "Hermes Agent", "command-code": "Command Code",
		"my-agent": "A custom tool from my config",
	} {
		if got := toolLabel(tool); got != want {
			t.Errorf("toolLabel(%q) = %q, want %q", tool, got, want)
		}
	}
	form, err := os.ReadFile(filepath.Join("..", "..", ".github", "ISSUE_TEMPLATE", "bug_report.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Claude Code", "Codex", "OpenCode", "Grok Build", "Gemini CLI", "Pi", "Hermes Agent", "Command Code", "A custom tool from my config"} {
		if !strings.Contains(string(form), "- "+label+"\n") {
			t.Errorf("the bug form's dropdown has no %q option", label)
		}
	}
}

func TestFormatPreviewSaysWhatFilingWouldDoAndHowToConfirm(t *testing.T) {
	preview := Preview{ID: "3f2a91c4", Kind: Bug, Title: "t", Body: "### What happened\n\nb\n", Labels: []string{"bug"}, Route: RouteGH, Account: "yoan"}
	text := FormatPreview(preview, "call again with preview_id 3f2a91c4")
	for _, want := range []string{
		"bug report for " + Repo + ", not filed yet",
		"title: t",
		"labels: bug",
		"preview id: 3f2a91c4",
		"### What happened",
		"through gh as yoan",
		"Nothing is posted until the user approves this preview; then call again with preview_id 3f2a91c4.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("preview text lacks %q:\n%s", want, text)
		}
	}
	preview.Route, preview.Account, preview.Reason, preview.URL = RouteBrowser, "", "gh is not installed", "https://example.test/new"
	text = FormatPreview(preview, "rerun with --confirm")
	for _, want := range []string{
		"gh is not installed, so filing hands the user this prefilled form to open instead of posting: https://example.test/new",
		"Once the user approves this preview, rerun with --confirm.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("browser preview text lacks %q:\n%s", want, text)
		}
	}
	if got := FormatFiled(Filed{Route: RouteGH, URL: "https://github.com/x/1"}); got != "filed https://github.com/x/1" {
		t.Fatalf("FormatFiled gh = %q", got)
	}
	if got := FormatFiled(Filed{Route: RouteBrowser, URL: "https://example.test/new"}); got != "nothing was posted; open this prefilled form to file it: https://example.test/new" {
		t.Fatalf("FormatFiled browser = %q", got)
	}
}
