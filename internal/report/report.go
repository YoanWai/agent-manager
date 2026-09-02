// Package report files a bug report or a feature request against the public
// agent-manager repository from an agent's MCP tool or from a shell. It
// composes the issue the way the repository's issue forms lay it out,
// gathers the context those forms ask for, and posts nothing until the
// caller confirms a preview.
package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
)

const Repo = "YoanWai/agent-manager"

const newIssueURL = "https://github.com/" + Repo + "/issues/new"

type Kind string

const (
	Bug     Kind = "bug"
	Feature Kind = "feature"
)

const (
	RouteGH      = "gh"
	RouteBrowser = "browser"
)

// commandBudget bounds every command the report shells out to, so a hung
// CLI never hangs the preview.
const commandBudget = 10 * time.Second

// Test seams: lookPath finds gh, runCommand runs it and the version probes,
// goos and procVersion decide the operating system label.
var (
	lookPath    = exec.LookPath
	runCommand  = run
	goos        = runtime.GOOS
	procVersion = "/proc/version"
)

type Draft struct {
	Kind  Kind
	Title string
	Body  string
}

// Context is what the bug form asks the reporter to type by hand.
type Context struct {
	Version     string `json:"version"`
	OS          string `json:"os"`
	Tmux        string `json:"tmux,omitempty"`
	Tool        string `json:"tool,omitempty"`
	ToolVersion string `json:"tool_version,omitempty"`
}

type Preview struct {
	Kind    Kind     `json:"kind"`
	Title   string   `json:"title"`
	Body    string   `json:"body" jsonschema:"issue body exactly as it will be posted, context included"`
	Labels  []string `json:"labels"`
	Context Context  `json:"context"`
	Route   string   `json:"route" jsonschema:"gh posts through the gh CLI as the user's account; browser hands the user a prefilled form url instead"`
	Account string   `json:"account,omitempty" jsonschema:"GitHub login gh would post as"`
	Reason  string   `json:"reason,omitempty" jsonschema:"why the browser route is used instead of gh"`
	URL     string   `json:"url" jsonschema:"prefilled new-issue form url, the browser route's answer"`
}

type Filed struct {
	Route string `json:"route"`
	URL   string `json:"url" jsonschema:"the filed issue on the gh route, the prefilled form to open on the browser route"`
}

type Reporter struct {
	configDir string
	version   string
}

func New(configDir, version string) *Reporter {
	return &Reporter{configDir: configDir, version: version}
}

// Preview composes the issue and says how filing would go, without posting.
func (r *Reporter) Preview(sessionID string, draft Draft) (Preview, error) {
	if err := draft.validate(); err != nil {
		return Preview{}, err
	}
	gathered, err := r.gather(sessionID)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{
		Kind:    draft.Kind,
		Title:   strings.TrimSpace(draft.Title),
		Body:    Compose(draft, gathered),
		Labels:  labels(draft.Kind),
		Context: gathered,
		URL:     FormURL(draft, gathered),
	}
	preview.Route, preview.Account, preview.Reason = route()
	return preview, nil
}

// File posts the issue the preview showed: through gh when it is installed
// and logged in, otherwise by handing back the prefilled form for the user
// to open. A caller passes here only after the user approved the preview.
func (r *Reporter) File(sessionID string, draft Draft) (Filed, error) {
	preview, err := r.Preview(sessionID, draft)
	if err != nil {
		return Filed{}, err
	}
	if preview.Route == RouteBrowser {
		return Filed{Route: RouteBrowser, URL: preview.URL}, nil
	}
	args := []string{"issue", "create", "--repo", Repo, "--title", preview.Title, "--body", preview.Body}
	for _, label := range preview.Labels {
		args = append(args, "--label", label)
	}
	issueURL, err := runCommand("gh", args...)
	if err != nil {
		return Filed{}, fmt.Errorf("gh issue create: %w", err)
	}
	return Filed{Route: RouteGH, URL: issueURL}, nil
}

func (d Draft) validate() error {
	if d.Kind != Bug && d.Kind != Feature {
		return fmt.Errorf("kind %q is not one of bug, feature", d.Kind)
	}
	if strings.TrimSpace(d.Title) == "" {
		return errors.New("title is empty; give the issue a one-line title")
	}
	if strings.TrimSpace(d.Body) == "" {
		if d.Kind == Bug {
			return errors.New("body is empty; say what you did, what you expected and what happened instead")
		}
		return errors.New("body is empty; say what you are trying to do and what you have in mind")
	}
	return nil
}

func labels(kind Kind) []string {
	if kind == Bug {
		return []string{"bug"}
	}
	return []string{"enhancement"}
}

// Compose renders the body with the headings GitHub gives a form
// submission, so an issue filed through gh reads like one filed through
// the form.
func Compose(draft Draft, gathered Context) string {
	body := strings.TrimSpace(draft.Body)
	if draft.Kind == Feature {
		return sections(
			"What are you trying to do", body,
			"Anything else", gathered.summary(),
		)
	}
	return sections(
		"What happened", body,
		"agent-manager version", gathered.Version,
		"Operating system", gathered.OS,
		"tmux version", gathered.Tmux,
		"Which agent CLI", gathered.Tool,
		"Agent CLI version", gathered.ToolVersion,
	)
}

func sections(pairs ...string) string {
	var out strings.Builder
	for index := 0; index < len(pairs); index += 2 {
		if pairs[index+1] == "" {
			continue
		}
		fmt.Fprintf(&out, "### %s\n\n%s\n\n", pairs[index], pairs[index+1])
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func (c Context) summary() string {
	line := "agent-manager " + c.Version + " on " + c.OS
	if c.Tool != "" {
		line += ", reported from a " + c.Tool + " session"
	}
	return line + "."
}

// FormURL prefills the repository's issue form for the draft, field by
// field, so the user opening it finds the same issue the preview showed.
func FormURL(draft Draft, gathered Context) string {
	query := url.Values{}
	query.Set("title", strings.TrimSpace(draft.Title))
	body := strings.TrimSpace(draft.Body)
	if draft.Kind == Feature {
		query.Set("template", "feature_request.yml")
		query.Set("problem", body)
		query.Set("extra", gathered.summary())
		return newIssueURL + "?" + query.Encode()
	}
	query.Set("template", "bug_report.yml")
	query.Set("what-happened", body)
	query.Set("version", gathered.Version)
	query.Set("os", gathered.OS)
	for field, value := range map[string]string{
		"tmux-version": gathered.Tmux,
		"tool":         gathered.Tool,
		"tool-version": gathered.ToolVersion,
	} {
		if value != "" {
			query.Set(field, value)
		}
	}
	return newIssueURL + "?" + query.Encode()
}

// route decides how filing goes: gh, when it is on PATH and logged in, or
// the browser with the reason gh is not usable.
func route() (name, account, reason string) {
	if _, err := lookPath("gh"); err != nil {
		return RouteBrowser, "", "gh is not installed"
	}
	login, err := runCommand("gh", "api", "user", "--jq", ".login")
	if err != nil {
		return RouteBrowser, "", "gh is installed but cannot reach GitHub as a logged-in account: " + err.Error()
	}
	return RouteGH, login, ""
}

func (r *Reporter) gather(sessionID string) (Context, error) {
	gathered := Context{Version: r.version, OS: operatingSystem()}
	if tmux, err := runCommand("tmux", "-V"); err == nil {
		gathered.Tmux = tmux
	}
	if strings.TrimSpace(sessionID) == "" {
		return gathered, nil
	}
	toolName, err := r.sessionTool(sessionID)
	if err != nil {
		return Context{}, err
	}
	cfg, err := config.LoadDir(r.configDir)
	if err != nil {
		return Context{}, err
	}
	tool := cfg.Tools[toolName]
	if tool.Shell {
		return gathered, nil
	}
	gathered.Tool = toolLabel(toolName)
	// The executable the tool's config block launches is what answers
	// --version; a CLI that answers with an error leaves the field blank.
	if fields := strings.Fields(tool.Command); len(fields) > 0 {
		if probed, err := runCommand(fields[0], "--version"); err == nil {
			gathered.ToolVersion = firstLine(probed)
		}
	}
	return gathered, nil
}

func (r *Reporter) sessionTool(sessionID string) (string, error) {
	st, err := store.Open(filepath.Join(r.configDir, "state.db"))
	if err != nil {
		return "", err
	}
	defer st.Close()
	sess, err := st.Get(sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("calling session %s no longer exists", sessionID)
	}
	if err != nil {
		return "", err
	}
	return sess.Tool, nil
}

// toolLabel spells a configured tool the way the bug form's dropdown does,
// so a prefilled form lands on the right option.
func toolLabel(toolName string) string {
	labels := map[string]string{
		"claude":       "Claude Code",
		"codex":        "Codex",
		"opencode":     "OpenCode",
		"grok":         "Grok Build",
		"gemini":       "Gemini CLI",
		"pi":           "Pi",
		"hermes":       "Hermes Agent",
		"command-code": "Command Code",
	}
	if label, known := labels[toolName]; known {
		return label
	}
	return "A custom tool from my config"
}

// operatingSystem spells the platform the way the bug form's dropdown does.
func operatingSystem() string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		if kernel, err := os.ReadFile(procVersion); err == nil && strings.Contains(strings.ToLower(string(kernel)), "microsoft") {
			return "Windows (WSL2)"
		}
		return "Linux"
	}
	return goos
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	var stderr strings.Builder
	command := exec.CommandContext(ctx, name, args...)
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// FormatPreview is the sentence both fronts print for a preview; confirm
// names how that front files it, since the word differs between them.
func FormatPreview(preview Preview, confirm string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s for %s, not filed yet\n", describe(preview.Kind), Repo)
	fmt.Fprintf(&out, "title: %s\n", preview.Title)
	fmt.Fprintf(&out, "labels: %s\n\n", strings.Join(preview.Labels, ", "))
	out.WriteString(preview.Body)
	out.WriteString("\n")
	if preview.Route == RouteGH {
		fmt.Fprintf(&out, "Filing posts this through gh as %s. Nothing is posted until the user approves this preview; then %s.", preview.Account, confirm)
	} else {
		fmt.Fprintf(&out, "%s, so filing hands the user this prefilled form to open instead of posting: %s\nOnce the user approves this preview, %s.", preview.Reason, preview.URL, confirm)
	}
	return out.String()
}

func FormatFiled(filed Filed) string {
	if filed.Route == RouteGH {
		return "filed " + filed.URL
	}
	return "nothing was posted; open this prefilled form to file it: " + filed.URL
}

func describe(kind Kind) string {
	if kind == Bug {
		return "bug report"
	}
	return "feature request"
}
