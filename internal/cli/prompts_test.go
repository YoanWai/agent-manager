package cli

import (
	"bytes"
	"testing"
)

func TestPromptsAddReplacesANameGivenWithSurroundingSpaces(t *testing.T) {
	configDir := t.TempDir()
	var out bytes.Buffer
	if err := runPromptsAdd(&out, []string{"Role", "one"}, configDir); err != nil {
		t.Fatal(err)
	}
	if err := runPromptsAdd(&out, []string{" Role ", "two"}, configDir); err != nil {
		t.Fatalf("re-adding with spaces: %v", err)
	}
	preset, found, err := settingsStore(t, configDir).SessionPreset("Role")
	if err != nil || !found || preset.Instructions != "two" {
		t.Fatalf("Role = %+v, %t, %v; want its text replaced", preset, found, err)
	}
}
