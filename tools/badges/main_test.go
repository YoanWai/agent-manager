package main

import (
	"testing"
)

func TestCompact(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{
		{0, "0"},
		{176, "176"},
		{999, "999"},
		{1000, "1k"},
		{1918, "1.9k"},
		{12500, "12.5k"},
		{100000, "100k"},
	} {
		if got := compact(tc.in); got != tc.want {
			t.Errorf("compact(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
