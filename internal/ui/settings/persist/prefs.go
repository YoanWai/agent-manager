package persist

import uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"

// StoredTerminalBackground reads the background row. A store error yields
// the painted default.
func StoredTerminalBackground(reader Reader) (bool, error) {
	chosen, err := reader.Setting(uisettings.BackgroundSetting)
	return chosen == "terminal", err
}

// StoredSplitLayout reads the review layout row; anything but "unified"
// keeps the split.
func StoredSplitLayout(reader Reader) (bool, error) {
	chosen, err := reader.Setting(uisettings.DiffLayoutSetting)
	return chosen != "unified", err
}
