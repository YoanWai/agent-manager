package notices

import "net/url"

// DocsURL is the documentation site the Help and Settings cards open.
const DocsURL = "https://agent-manager.dev/docs/"

// BugReportURL opens the bug form with this run's version filled in.
func BugReportURL(version string) string {
	q := url.Values{}
	q.Set("template", "bug_report.yml")
	if version != "" {
		q.Set("version", version)
	}
	return RepoURL + "/issues/new?" + q.Encode()
}

// FeatureRequestURL opens the feature form.
func FeatureRequestURL() string {
	return RepoURL + "/issues/new?template=feature_request.yml"
}

// SupportRequestURL opens a prefilled feature request for another CLI.
func SupportRequestURL() string {
	body := "**What are you trying to do**\n\n" +
		"I want agent-manager to support another coding CLI.\n\n" +
		"**What you have in mind**\n\n" +
		"CLI name:\nHow to launch it:\nResume / session flags (if any):\n\n" +
		"**Area**\nConfig and tool support\n"
	return RepoURL + "/issues/new?body=" + url.QueryEscape(body)
}
