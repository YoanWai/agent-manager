package notices

import (
	"slices"
	"testing"
	"time"
)

func TestActiveSortsKindsAndFillsSubtitles(t *testing.T) {
	expires := time.Date(2026, time.November, 20, 0, 0, 0, 0, time.UTC)
	src := Sources{
		Store: true,
		Update: Update{
			Version: "v0.45.0",
			Latest:  "v0.46.0",
			Available: ReleaseRange{Releases: []Release{
				{Version: "v0.45.1", Headline: "Older headline"},
				{Version: "v0.46.0", Headline: "A first-run setup"},
			}},
		},
		Feed: []Message{
			{ID: "feed-heads-up", Kind: KindHeadsUp, Title: "Heads up", Banner: "Config moves soon"},
			{ID: "feed-news", Kind: KindNews, Title: "Discord", Banner: "Agent Manager has a Discord now", Action: "Join on Discord", Expires: expires},
		},
		ConfigImportError: "bad key",
	}
	notices := NewPanel(map[string]bool{}, map[string]bool{}).Active(src)

	wantKinds := []Kind{KindRelease, KindNews, KindHeadsUp, KindHeadsUp}
	gotKinds := make([]Kind, len(notices))
	for i, notice := range notices {
		gotKinds[i] = notice.Kind
	}
	if !slices.Equal(gotKinds, wantKinds) {
		t.Fatalf("kinds = %v, want %v", gotKinds, wantKinds)
	}
	wantIDs := []string{"update-v0.46.0", "feed-news", "feed-heads-up", ConfigNotImported}
	if got := ids(notices); !slices.Equal(got, wantIDs) {
		t.Fatalf("ids = %v, want %v", got, wantIDs)
	}
	if notices[0].Subtitle != "A first-run setup" {
		t.Fatalf("release subtitle = %q", notices[0].Subtitle)
	}
	if notices[1].Subtitle != "Agent Manager has a Discord now" {
		t.Fatalf("news subtitle = %q", notices[1].Subtitle)
	}
	if notices[1].Action != "Join on Discord" || !notices[1].Expires.Equal(expires) {
		t.Fatalf("news action/expires = %q / %v", notices[1].Action, notices[1].Expires)
	}
	if notices[3].Subtitle != "Settings could not import it" {
		t.Fatalf("config warning subtitle = %q", notices[3].Subtitle)
	}
}

func TestActiveKeepsFeedOrderWithinAKind(t *testing.T) {
	src := Sources{
		Store:  true,
		Update: Update{Version: "v0.45.0"},
		Feed: []Message{
			{ID: "tip-b", Kind: KindTip},
			{ID: "news-b", Kind: KindNews},
			{ID: "tip-a", Kind: KindTip},
			{ID: "news-a", Kind: KindNews},
		},
	}
	got := ids(NewPanel(map[string]bool{}, map[string]bool{}).Active(src))
	want := []string{"news-b", "news-a", "tip-b", "tip-a"}
	if !slices.Equal(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestActiveWhatsNewSubtitleIsTheNewestHeadline(t *testing.T) {
	src := Sources{
		Store:           true,
		WhatsNewVersion: "v0.46.0",
		WhatsNewFrom:    "v0.45.0",
		Update: Update{
			Version: "v0.46.0",
			Installed: ReleaseRange{Releases: []Release{
				{Version: "v0.46.0", Headline: "A first-run setup"},
			}},
		},
	}
	notices := NewPanel(map[string]bool{}, map[string]bool{}).Active(src)
	if len(notices) != 1 || notices[0].Kind != KindRelease || notices[0].Subtitle != "A first-run setup" {
		t.Fatalf("notices = %+v", notices)
	}
}

func TestActiveReleaseWithoutHeadlineHasNoSubtitle(t *testing.T) {
	src := Sources{Store: true, Update: Update{Version: "v0.45.0", Latest: "v0.46.0"}}
	notices := NewPanel(map[string]bool{}, map[string]bool{}).Active(src)
	if len(notices) != 1 || notices[0].Subtitle != "" {
		t.Fatalf("notices = %+v", notices)
	}
}
