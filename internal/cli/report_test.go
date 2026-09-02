package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/report"
)

// fakeReporter records which drafts were previewed and which were filed, so
// a test can tell a run without --confirm posted nothing.
type fakeReporter struct {
	callerID  string
	approved  string
	previewed []report.Draft
	filed     []report.Draft
	preview   report.Preview
	result    report.Filed
	failWith  error
}

func (f *fakeReporter) Preview(sessionID string, draft report.Draft) (report.Preview, error) {
	f.callerID = sessionID
	f.previewed = append(f.previewed, draft)
	return f.preview, f.failWith
}

func (f *fakeReporter) File(sessionID string, draft report.Draft, previewID string) (report.Filed, error) {
	f.callerID, f.approved = sessionID, previewID
	f.filed = append(f.filed, draft)
	return f.result, f.failWith
}

func sampleReporter() *fakeReporter {
	return &fakeReporter{
		preview: report.Preview{ID: "3f2a91c4", Kind: report.Bug, Title: "Space lands in the wrong pane", Body: "### What happened\n\nsteps\n", Labels: []string{"bug"}, Route: report.RouteGH, Account: "yoan"},
		result:  report.Filed{Route: report.RouteGH, URL: "https://github.com/YoanWai/agent-manager/issues/512"},
	}
}

func TestIssuePreviewsUntilConfirmed(t *testing.T) {
	fake := sampleReporter()
	out := &bytes.Buffer{}
	if err := runIssue(out, fake, []string{"Space lands in the wrong pane", "--body", "steps"}, "cafe0001"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	for _, want := range []string{"bug report for YoanWai/agent-manager, not filed yet", "preview id: 3f2a91c4", "through gh as yoan", "rerun with --confirm 3f2a91c4"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview lacks %q:\n%s", want, out.String())
		}
	}
	want := report.Draft{Kind: report.Bug, Title: "Space lands in the wrong pane", Body: "steps"}
	if fake.callerID != "cafe0001" || len(fake.previewed) != 1 || fake.previewed[0] != want || len(fake.filed) != 0 {
		t.Fatalf("issue without --confirm previewed %v and filed %v as %q", fake.previewed, fake.filed, fake.callerID)
	}

	out.Reset()
	if err := runIssue(out, fake, []string{"Space lands in the wrong pane", "--body", "steps", "--confirm", "3f2a91c4"}, "cafe0001"); err != nil {
		t.Fatalf("issue --confirm: %v", err)
	}
	if out.String() != "filed https://github.com/YoanWai/agent-manager/issues/512\n" {
		t.Fatalf("issue --confirm output = %q", out.String())
	}
	if len(fake.filed) != 1 || fake.filed[0] != want || fake.approved != "3f2a91c4" {
		t.Fatalf("issue --confirm filed %v approved as %q", fake.filed, fake.approved)
	}
}

func TestFeatureDraftsAFeatureRequest(t *testing.T) {
	fake := sampleReporter()
	fake.preview.Kind = report.Feature
	fake.preview.Route, fake.preview.Account, fake.preview.Reason, fake.preview.URL = report.RouteBrowser, "", "gh is not installed", "https://github.com/YoanWai/agent-manager/issues/new?template=feature_request.yml"
	out := &bytes.Buffer{}
	if err := runFeature(out, fake, []string{"--body", "I keep several agents on one repo.", "Show overlapping files"}, "cafe0001"); err != nil {
		t.Fatalf("feature: %v", err)
	}
	if fake.previewed[0] != (report.Draft{Kind: report.Feature, Title: "Show overlapping files", Body: "I keep several agents on one repo."}) {
		t.Fatalf("feature drafted %+v", fake.previewed[0])
	}
	if !strings.Contains(out.String(), "gh is not installed, so filing hands the user this prefilled form") {
		t.Fatalf("browser preview = %q", out.String())
	}
}

func TestReportJSONPrintsTheRecord(t *testing.T) {
	fake := sampleReporter()
	out := &bytes.Buffer{}
	if err := runIssue(out, fake, []string{"t", "--body", "b", "--json"}, "cafe0001"); err != nil {
		t.Fatalf("issue --json: %v", err)
	}
	var preview report.Preview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.Account != "yoan" || preview.Route != report.RouteGH {
		t.Fatalf("issue --json = %q (%v)", out.String(), err)
	}
	out.Reset()
	if err := runIssue(out, fake, []string{"t", "--body", "b", "--json", "--confirm", "3f2a91c4"}, "cafe0001"); err != nil {
		t.Fatalf("issue --json --confirm: %v", err)
	}
	var filed report.Filed
	if err := json.Unmarshal(out.Bytes(), &filed); err != nil || filed != fake.result {
		t.Fatalf("issue --json --confirm = %q (%v)", out.String(), err)
	}
}

func TestReportUsageErrors(t *testing.T) {
	fake := sampleReporter()
	if err := runIssue(&bytes.Buffer{}, fake, []string{"--body", "b"}, "cafe0001"); err == nil || !strings.Contains(err.Error(), "usage: agent-manager "+usageIssue) {
		t.Fatalf("issue without a title = %v", err)
	}
	if err := runFeature(&bytes.Buffer{}, fake, []string{"one", "two"}, "cafe0001"); err == nil || !strings.Contains(err.Error(), "usage: agent-manager "+usageFeature) {
		t.Fatalf("feature with two titles = %v", err)
	}
	out := &bytes.Buffer{}
	if err := runIssue(out, fake, []string{"-h"}, "cafe0001"); !errors.Is(err, ErrUsageShown) || !strings.Contains(out.String(), usageIssue) {
		t.Fatalf("issue -h = %v, %q", err, out.String())
	}
	if len(fake.previewed)+len(fake.filed) != 0 {
		t.Fatalf("a usage error reached the reporter: %v %v", fake.previewed, fake.filed)
	}
}

// The layer refuses a blank body; the front hands its words on and prints
// nothing that would read as a filed issue.
func TestReportLayerFailureReachesTheCaller(t *testing.T) {
	fake := sampleReporter()
	fake.failWith = errors.New("body is empty; say what you did, what you expected and what happened instead")
	for _, args := range [][]string{{"t"}, {"t", "--confirm", "3f2a91c4"}} {
		out := &bytes.Buffer{}
		err := runIssue(out, fake, args, "cafe0001")
		if err == nil || !strings.Contains(err.Error(), "body is empty") || out.Len() != 0 {
			t.Fatalf("issue %v = %v, printed %q", args, err, out.String())
		}
	}
}
