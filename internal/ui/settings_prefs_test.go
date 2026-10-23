package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

func TestDefaultSplitLayout(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if !m.defaultSplitLayout() {
		t.Fatal("split should be the default layout")
	}
	if err := m.services.store.SetSetting(uisettings.DiffLayoutSetting, "unified"); err != nil {
		t.Fatal(err)
	}
	if m.defaultSplitLayout() {
		t.Fatal("stored unified choice should opt out of split")
	}
}
