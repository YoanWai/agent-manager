//go:build darwin

package clipboard

import (
	designclip "golang.design/x/clipboard"
)

func init() {
	// In-process NSPasteboard via purego (no cgo, no osascript). Reading a
	// ~1MB screenshot is typically under a millisecond after the first call.
	readNativeImage = func() ([]byte, error) {
		if err := designclip.Init(); err != nil {
			return nil, err
		}
		data := designclip.Read(designclip.FmtImage)
		if len(data) == 0 {
			return nil, ErrNoImage
		}
		return data, nil
	}
}
