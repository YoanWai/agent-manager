package pwsh

import (
	"strings"
	"testing"
)

// PowerShell closes a verbatim string on any of the typographic single
// quotes too, so each must be doubled to stay literal.
func TestQuoteDoublesEverySingleQuoteForm(t *testing.T) {
	got := Quote("it's ‘x’")
	if want := "'it''s ‘‘x’’'"; got != want {
		t.Fatalf("Quote = %q, want %q", got, want)
	}
}

// PowerShell reads -EncodedCommand as base64 over UTF-16LE.
func TestEncodedCommandIsUTF16LEBase64(t *testing.T) {
	got := EncodedCommand("Ab€")
	if got != "QQBiAKwg" {
		t.Fatalf("EncodedCommand = %q, want QQBiAKwg", got)
	}
}

// Only a PowerShell from 7.3 on passes a quoted argument to a native
// program whole, so everything before it is refused, and the refusal names
// the version found without the formatting PowerShell prints around it.
func TestCheckVersionStartsAt73(t *testing.T) {
	for _, tc := range []struct {
		version string
		refused bool
	}{
		{"5.1.26100.2161", true},
		{"7.2.24", true},
		{"7.3.0", false},
		{"7.5.0-preview.3", false},
		{"8.0.0", false},
		{"", true},
		{"Major  Minor", true},
	} {
		err := checkVersion(tc.version)
		if (err != nil) != tc.refused {
			t.Errorf("checkVersion(%q) = %v, want refused %v", tc.version, err, tc.refused)
		}
	}
	if err := checkVersion("7.2.24"); err == nil || !strings.HasPrefix(err.Error(), "PowerShell 7.2 is too old") {
		t.Errorf("checkVersion(7.2) = %v, want it to name 7.2", err)
	}
}
