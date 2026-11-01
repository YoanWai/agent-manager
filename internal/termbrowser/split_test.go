package termbrowser

import (
	"slices"
	"testing"
)

func TestSplitCommandKeepsQuotedSpaces(t *testing.T) {
	got, err := SplitCommand(`fill @e3 "hello world" it\'s\ fine 'a "b"'`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fill", "@e3", "hello world", "it's fine", `a "b"`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSplitCommandOpensQuotesMidWord(t *testing.T) {
	got, err := SplitCommand(`--value='a b'`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--value=a b"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSplitCommandRejectsUnterminatedQuote(t *testing.T) {
	if _, err := SplitCommand(`fill "oops`); err == nil {
		t.Fatal("want error")
	}
}
