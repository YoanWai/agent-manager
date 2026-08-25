//go:build linux

package clipboard

import (
	"context"
	"errors"

	designclip "golang.design/x/clipboard"
)

func init() {
	// In-process X11/Wayland pasteboard when the runtime can open a display.
	// Falls through to wl-paste/xclip/WSL when Init fails or no image is set.
	readNativeImage = func() ([]byte, error) {
		if err := designclip.Init(); err != nil {
			return nil, err
		}
		data, err := designclip.Read(context.Background(), designclip.FmtImage)
		if errors.Is(err, designclip.ErrNoData) {
			return nil, ErrNoImage
		}
		if err != nil {
			return nil, err
		}
		return data, nil
	}
}
