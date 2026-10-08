package ui

import (
	"strings"
	"testing"
)

func TestSenderNameIsTheShortHostnameCutToFit(t *testing.T) {
	for host, want := range map[string]string{
		"laptop.local":          "laptop: you",
		"":                      "you",
		"bad\x07host.lan":       "badhost: you",
		strings.Repeat("é", 40): strings.Repeat("é", 29) + ": you",
		strings.Repeat("a", 80): strings.Repeat("a", 59) + ": you",
	} {
		got := senderName(host)
		if got != want || len(got) > maxSenderBytes {
			t.Fatalf("senderName(%q) = %q, want %q", host, got, want)
		}
	}
}
