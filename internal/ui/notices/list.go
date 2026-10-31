package notices

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Update is what the root knows of releases: this build, a newer release,
// the ranges either side of it, and the refresh and self-update in flight.
type Update struct {
	Version    string
	Latest     string
	URL        string
	Available  ReleaseRange
	Installed  ReleaseRange
	Checked    bool
	Refreshing bool
	Applying   bool
}

// Sources are the root facts the notice list is built from, copied so the
// panel never reaches into the store, the network or the key tables.
type Sources struct {
	Store  bool
	Update Update
	Feed   []Message
	// WhatsNewVersion and WhatsNewFrom mirror the persisted what's-new
	// settings; ConfigImportError is why an old config.toml was refused.
	WhatsNewVersion   string
	WhatsNewFrom      string
	ConfigImportError string
}

func newestHeadline(releases []Release) string {
	if len(releases) == 0 {
		return ""
	}
	return releases[len(releases)-1].Headline
}

func releaseCountLabel(releaseRange ReleaseRange) string {
	count := fmt.Sprint(len(releaseRange.Releases))
	if !releaseRange.Complete {
		count += "+"
	}
	return count
}

// Active is every notice the user has not dismissed, in display order.
func (p Panel) Active(src Sources) []Notice {
	if !src.Store {
		return nil
	}
	var notices []Notice
	if src.Update.Latest != "" {
		releaseRange := src.Update.Available
		title := src.Update.Latest + " available"
		if len(releaseRange.Releases) > 1 {
			title = releaseCountLabel(releaseRange) + " releases available · " + src.Update.Latest
		}
		var body []string
		if len(releaseRange.Releases) == 0 {
			body = []string{"The generated change summary is not available yet; r refreshes it now."}
		}
		notices = append(notices, Notice{
			ID:            "update-" + src.Update.Latest,
			Kind:          KindRelease,
			Subtitle:      newestHeadline(releaseRange.Releases),
			Title:         title,
			Headline:      newestHeadline(releaseRange.Releases),
			ReleaseNotes:  true,
			Body:          body,
			Releases:      releaseRange.Releases,
			RangeComplete: releaseRange.Complete,
			Version:       src.Update.Latest,
			From:          src.Update.Version,
			URL:           src.Update.URL,
		})
	}
	if src.WhatsNewVersion == src.Update.Version {
		releaseRange := src.Update.Installed
		title := "Updated to " + src.Update.Version
		if len(releaseRange.Releases) > 1 {
			title = "Updated across " + releaseCountLabel(releaseRange) + " releases · " + src.Update.Version
		}
		var body []string
		if len(releaseRange.Releases) == 0 && !src.Update.Checked {
			body = []string{"Loading the change summary from GitHub…"}
		} else if len(releaseRange.Releases) == 0 {
			body = []string{"No generated change summary was found; r refreshes GitHub now."}
		}
		notices = append(notices, Notice{
			ID:            "whatsnew-" + src.Update.Version,
			Kind:          KindRelease,
			Subtitle:      newestHeadline(releaseRange.Releases),
			Title:         title,
			Headline:      newestHeadline(releaseRange.Releases),
			ReleaseNotes:  true,
			Body:          body,
			Releases:      releaseRange.Releases,
			RangeComplete: releaseRange.Complete,
			Version:       src.Update.Version,
			From:          src.WhatsNewFrom,
			URL:           RepoURL + "/releases/tag/v" + strings.TrimPrefix(src.Update.Version, "v"),
		})
	}
	for _, msg := range src.Feed {
		notices = append(notices, Notice{
			ID:       msg.ID,
			Kind:     msg.Kind,
			Subtitle: msg.Banner,
			Title:    msg.Title,
			Accent:   msg.Accent,
			Body:     msg.Body,
			URL:      msg.URL,
			Action:   msg.Action,
			Expires:  msg.Expires,
		})
	}
	if src.ConfigImportError != "" {
		notices = append(notices, Notice{
			ID:       ConfigNotImported,
			Kind:     KindHeadsUp,
			Subtitle: "Settings could not import it",
			Title:    "Your config.toml was not carried into Settings",
			Body: []string{
				"The keys and the editor live in Settings now. A config.toml that",
				"names them is read once to carry them over, and yours was refused:",
				"",
				src.ConfigImportError,
				"",
				"The keys are the defaults and the editor is picked for you until",
				"you set them in Settings. The file is not read again, and is yours",
				"to delete.",
			},
			URL:    RepoURL + "/blob/main/docs/configuration.md",
			Action: "Read what Settings holds",
		})
	}
	kept := notices[:0]
	for _, n := range notices {
		if !p.dismissed[n.ID] {
			n.Unread = !p.seen[n.ID]
			kept = append(kept, n)
		}
	}
	slices.SortStableFunc(kept, func(a, b Notice) int { return cmp.Compare(a.Kind, b.Kind) })
	return kept
}
