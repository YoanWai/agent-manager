package notices

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
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
	GlyphMode presentation.GlyphMode
	Store     bool
	Update    Update
	Feed      []Message
	// WhatsNewVersion and WhatsNewFrom mirror the persisted what's-new
	// settings; ConfigImportError is why an old config.toml was refused.
	WhatsNewVersion   string
	WhatsNewFrom      string
	ConfigImportError string
	SessionKeys       keybind.Table
	ListKeys          keybind.Table
}

// newestHeadline is the headline of the last release in an oldest-first range.
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
	g := presentation.GlyphsFor(src.GlyphMode)
	var notices []Notice
	if src.Update.Latest != "" {
		releaseRange := src.Update.Available
		title := src.Update.Latest + " available"
		if len(releaseRange.Releases) > 1 {
			title = releaseCountLabel(releaseRange) + " releases available · " + src.Update.Latest
		}
		body := []string{"You are on " + src.Update.Version + ". Here is everything released since then:"}
		if len(releaseRange.Releases) == 0 {
			body = append(body, "The generated change summary is not available yet; r refreshes it now.")
		}
		notices = append(notices, Notice{
			ID:            "update-" + src.Update.Latest,
			Glyph:         g.UpdateGlyph,
			Tint:          TintAccent,
			Title:         title,
			Headline:      newestHeadline(releaseRange.Releases),
			ReleaseNotes:  true,
			Body:          body,
			Releases:      releaseRange.Releases,
			RangeComplete: releaseRange.Complete,
			After: []string{
				"u updates once to " + src.Update.Latest + " and restarts; every release above is included.",
				"Enter opens the full release notes.",
			},
			URL: src.Update.URL,
		})
	}
	if src.WhatsNewVersion == src.Update.Version {
		releaseRange := src.Update.Installed
		title := "Updated to " + src.Update.Version
		if len(releaseRange.Releases) > 1 {
			title = "Updated across " + releaseCountLabel(releaseRange) + " releases · " + src.Update.Version
		}
		body := []string{"Updated from " + src.WhatsNewFrom + " to " + src.Update.Version + "."}
		if len(releaseRange.Releases) == 0 && !src.Update.Checked {
			body = append(body, "Loading the change summary from GitHub…")
		} else if len(releaseRange.Releases) == 0 {
			body = append(body, "No generated change summary was found; r refreshes GitHub now.")
		}
		notices = append(notices, Notice{
			ID:           "whatsnew-" + src.Update.Version,
			Glyph:        g.NewGlyph,
			Tint:         TintAccent2,
			Title:        title,
			Headline:     newestHeadline(releaseRange.Releases),
			ReleaseNotes: true,
			Body:         body,
			Releases:     releaseRange.Releases,
			After: []string{
				"Enter opens the full release notes.",
			},
			RangeComplete: releaseRange.Complete,
			URL:           RepoURL + "/releases/tag/v" + strings.TrimPrefix(src.Update.Version, "v"),
		})
	}
	for _, msg := range src.Feed {
		notices = append(notices, Notice{
			ID:       msg.ID,
			Glyph:    g.FeedGlyph,
			Tint:     TintAccent2,
			Title:    msg.Title,
			Headline: msg.Headline,
			Accent:   msg.Accent,
			Body:     msg.Body,
			URL:      msg.URL,
		})
	}
	if src.ConfigImportError != "" {
		notices = append(notices, Notice{
			ID:    ConfigNotImported,
			Glyph: g.SettingsGlyph,
			Tint:  TintWarm,
			Title: "Your config.toml was not carried into Settings",
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
			After: []string{
				"Enter opens the page on what Settings holds.",
			},
			URL: RepoURL + "/blob/main/docs/configuration.md",
		})
	}
	notices = append(notices,
		Notice{
			ID:    Welcome,
			Glyph: g.WelcomeGlyph,
			Tint:  TintAccent2,
			Title: "Welcome to agent-manager",
			Body:  src.welcomeBody(),
			URL:   RepoURL + "#readme",
		},
		Notice{
			ID:    ArrowStep,
			Glyph: g.BetaGlyph,
			Tint:  TintWarm,
			Title: "New in beta: step in and out with ← →",
			Body: []string{
				"→ on a list row steps in: it focuses the session, or opens the group.",
				"← steps out: it closes the group, and inside a focused session it",
				"returns here - only while the caret sits at the start of the agent's",
				"prompt, where ← would do nothing. Anywhere else in the prompt it",
				"moves the caret as always.",
				"",
				"We are testing this. If a ← ever lands somewhere you did not expect,",
				"Enter here opens a prefilled report, and Settings (s) has a",
				"\"←→ step in/out\" row that turns the pair off.",
			},
			URL: arrowStepFeedbackURL(src.Update.Version),
		},
	)

	kept := notices[:0]
	for _, n := range notices {
		if !p.dismissed[n.ID] {
			kept = append(kept, n)
		}
	}
	return kept
}

// arrowStepFeedbackURL prefills a report scoped to the beta ←→ pair, so
// feedback on it arrives labeled without the reporter typing the context.
func arrowStepFeedbackURL(version string) string {
	body := fmt.Sprintf("**Version:** %s\n**OS:** %s/%s\n\n**Area**\n←→ step in/out (beta)\n\n**What happened:**\n", version, runtime.GOOS, runtime.GOARCH)
	return RepoURL + "/issues/new?body=" + url.QueryEscape(body)
}

// The welcome names the keys as the tables bind them on this run: the first
// key of each action, and nothing for an action turned off.
func (src Sources) welcomeBody() []string {
	prompt, help, search, settings := src.firstListKey(keybind.Prompt), src.firstListKey(keybind.Help), src.firstListKey(keybind.Search), src.firstListKey(keybind.Settings)
	body := []string{
		"Every row on the left is a live agent session.",
		"",
		welcomeRow(src.firstListKey(keybind.NewSession), "new session", prompt, "quick prompt mode"),
		welcomeRow(src.firstListKey(keybind.Open), "focus it", src.firstListKey(keybind.Attach), "attach it full screen"),
		src.welcomeSessionKeysLine(),
		welcomeRow(src.firstListKeys(keybind.Kill, keybind.Revive), "kill / revive", settings, "settings"),
		"",
	}
	if prompt != "" {
		body = append(body, prompt+" on a group row opens quick prompt mode to start a new agent there.")
	}
	g := presentation.GlyphsFor(src.GlyphMode)
	body = append(body, "Each row's mark is its state: "+g.WorkingGlyph+" working, "+g.WaitingGlyph+" waiting, "+g.FinishedGlyph+" finished, "+g.IdleGlyph+" idle.", "Marks missing? Settings ("+settings+"), symbols, Compatible.", "")
	if help != "" {
		keyMap := "Press " + help + " for every key: the map scrolls"
		if search != "" {
			keyMap += ", and " + search + " searches it"
		}
		body = append(body, keyMap+".")
	}
	return append(body,
		"A bug or an idea? Settings ("+settings+") has a row for each.",
		"Messages like this one live here; x dismisses one for good.",
	)
}

func (src Sources) firstListKey(action string) string {
	keys := src.ListKeys.Binding(action).Keys()
	if len(keys) == 0 {
		return ""
	}
	return keys[0].Glyph()
}

func (src Sources) firstListKeys(actions ...string) string {
	var glyphs []string
	for _, action := range actions {
		if glyph := src.firstListKey(action); glyph != "" {
			glyphs = append(glyphs, glyph)
		}
	}
	return strings.Join(glyphs, " / ")
}

// welcomeRow is two key columns; a half with no key left is blank.
func welcomeRow(leftKey, leftDoes, rightKey, rightDoes string) string {
	if leftKey == "" {
		leftDoes = ""
	}
	if rightKey == "" {
		rightDoes = ""
	}
	return strings.TrimRight(fmt.Sprintf("%-6s %-22s%-6s %s", leftKey, leftDoes, rightKey, rightDoes), " ")
}

// The widths are the columns of the welcome rows around this line.
func (src Sources) welcomeSessionKeysLine() string {
	first := func(action string) string {
		keys := src.SessionKeys.Binding(action).Keys()
		if len(keys) == 0 {
			return ""
		}
		return keys[0].Tea()
	}
	return welcomeRow(first(keybind.Detach), "back to the manager", first(keybind.Review), "review its diff")
}
