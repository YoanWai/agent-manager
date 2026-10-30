package notices

import "net/url"

const DocsURL = "https://agent-manager.dev/docs/"

func BugReportURL(version string) string {
	q := url.Values{}
	q.Set("template", "bug_report.yml")
	if version != "" {
		q.Set("version", version)
	}
	return RepoURL + "/issues/new?" + q.Encode()
}

func FeatureRequestURL() string {
	return RepoURL + "/issues/new?template=feature_request.yml"
}

func SupportRequestURL() string {
	body := "**What are you trying to do**\n\n" +
		"I want agent-manager to support another coding CLI.\n\n" +
		"**What you have in mind**\n\n" +
		"CLI name:\nHow to launch it:\nResume / session flags (if any):\n\n" +
		"**Area**\nConfig and tool support\n"
	return RepoURL + "/issues/new?body=" + url.QueryEscape(body)
}
