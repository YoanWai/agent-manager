//go:build linux

package clipboard

import (
	designclip "golang.design/x/clipboard"
)

func init() {
	// In-process X11/Wayland pasteboard when the runtime can open a display.
	// Falls through to wl-paste/xclip/WSL when Init fails or no image is set.
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
