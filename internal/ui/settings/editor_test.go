package settings

import "testing"

func TestEditorRowStepsThroughAutoFoundAndCustom(t *testing.T) {
	row := NewEditorRow("", nil)
	if got := row.Label(); got != "auto" {
		t.Fatalf("before the probe answers the row reads %q, want auto", got)
	}
	row.ApplyProbe(EditorsProbe{Found: []string{"code", "zed"}, Auto: "code"})
	for _, want := range []string{"auto · code", "code", "zed", "custom", "auto · code"} {
		if got := row.Label(); got != want {
			t.Fatalf("label = %q, want %q", got, want)
		}
		row.cycle(1)
	}
	row.cycle(-1)
	row.cycle(-1)
	if got := row.Label(); got != "custom" || row.Line() != "" {
		t.Fatalf("stepping back from auto should land on custom, got %q line %q", got, row.Line())
	}
}

func TestEditorRowPlacesTheStoredLine(t *testing.T) {
	probe := EditorsProbe{Found: []string{"code", "zed"}, Auto: "code"}
	for _, tc := range []struct{ stored, label string }{
		{"zed", "zed"},
		{"code -n", "custom · code -n"},
		{"subl", "custom · subl"},
	} {
		row := NewEditorRow(tc.stored, nil)
		if got := row.Line(); got != tc.stored {
			t.Fatalf("%s: line before the probe = %q", tc.stored, got)
		}
		row.ApplyProbe(probe)
		if got := row.Label(); got != tc.label {
			t.Errorf("%s: label = %q, want %q", tc.stored, got, tc.label)
		}
		if got := row.Line(); got != tc.stored {
			t.Errorf("%s: line = %q, the probe must not change the choice", tc.stored, got)
		}
	}
}

func TestEditorRowSaysWhenAutoFindsNothing(t *testing.T) {
	row := NewEditorRow("", nil)
	row.ApplyProbe(EditorsProbe{})
	if got := row.Label(); got != "auto · none found" {
		t.Fatalf("label = %q", got)
	}
}
