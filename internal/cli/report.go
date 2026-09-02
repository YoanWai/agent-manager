package cli

import (
	"io"

	"github.com/YoanWai/agent-manager/internal/report"
)

const (
	usageIssue   = `issue "<title>" --body "<text>" [--confirm] [--json]`
	usageFeature = `feature "<title>" --body "<text>" [--confirm] [--json]`
)

type issueReporter interface {
	Preview(sessionID string, draft report.Draft) (report.Preview, error)
	File(sessionID string, draft report.Draft) (report.Filed, error)
}

func reportSection(version string) section {
	open := func(configDir string) issueReporter { return report.New(configDir, version) }
	return section{
		title: "Bugs and ideas",
		commands: []command{
			{name: "issue", usage: usageIssue, about: "file a bug report on agent-manager's GitHub repo; without --confirm it only prints the preview to show the user", run: bind(open, runIssue)},
			{name: "feature", usage: usageFeature, about: "file a feature request the same way; --confirm posts through gh, or prints the prefilled form url when gh is not logged in", run: bind(open, runFeature)},
		},
	}
}

func runIssue(out io.Writer, reporter issueReporter, args []string, sessionID string) error {
	return runReport(out, reporter, report.Bug, usageIssue, args, sessionID)
}

func runFeature(out io.Writer, reporter issueReporter, args []string, sessionID string) error {
	return runReport(out, reporter, report.Feature, usageFeature, args, sessionID)
}

func runReport(out io.Writer, reporter issueReporter, kind report.Kind, usage string, args []string, sessionID string) error {
	set := newFlagSet(usage)
	body := set.String("body", "", "markdown body: for a bug what you did, what you expected and what happened; for a feature what you are trying to do and what you have in mind. It is posted publicly")
	confirm := set.Bool("confirm", false, "file the issue exactly as the preview showed, once the user has approved it; without this flag nothing is posted")
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	draft := report.Draft{Kind: kind, Title: operands[0], Body: *body}
	if !*confirm {
		preview, err := reporter.Preview(sessionID, draft)
		if err != nil {
			return err
		}
		return emit(out, *asJSON, preview, report.FormatPreview(preview, "rerun with --confirm"))
	}
	filed, err := reporter.File(sessionID, draft)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, filed, report.FormatFiled(filed))
}
