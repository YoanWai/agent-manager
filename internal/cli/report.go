package cli

import (
	"io"

	"github.com/YoanWai/agent-manager/internal/report"
)

const (
	usageIssue   = `issue "<title>" --body "<text>" [--confirm <preview-id>] [--json]`
	usageFeature = `feature "<title>" --body "<text>" [--confirm <preview-id>] [--json]`
)

type issueReporter interface {
	Preview(sessionID string, draft report.Draft) (report.Preview, error)
	File(sessionID string, draft report.Draft, previewID string) (report.Filed, error)
}

func reportSection(version string) section {
	open := func(configDir string) issueReporter { return report.New(configDir, version) }
	return section{
		title: "Bugs and ideas",
		commands: []command{
			{name: "issue", usage: usageIssue, about: "file a bug report on agent-manager's GitHub repo; without --confirm it only prints the preview to show the user", run: bind(open, runIssue)},
			{name: "feature", usage: usageFeature, about: "file a feature request the same way; --confirm takes the preview id and posts through gh, or prints the prefilled form url when gh is not logged in", run: bind(open, runFeature)},
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
	confirm := set.String("confirm", "", "the preview id printed above, passed once the user has approved that preview; it files exactly what they saw, and without it nothing is posted")
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	draft := report.Draft{Kind: kind, Title: operands[0], Body: *body}
	if *confirm == "" {
		preview, err := reporter.Preview(sessionID, draft)
		if err != nil {
			return err
		}
		return emit(out, *asJSON, preview, report.FormatPreview(preview, "rerun with --confirm "+preview.ID))
	}
	filed, err := reporter.File(sessionID, draft, *confirm)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, filed, report.FormatFiled(filed))
}
