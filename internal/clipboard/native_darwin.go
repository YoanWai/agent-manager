//go:build darwin

package clipboard

import (
	"context"
	"errors"

	designclip "golang.design/x/clipboard"
)

func init() {
	// In-process NSPasteboard via purego (no cgo, no osascript). Reading a
	// ~1MB screenshot is typically under a millisecond after the first call.
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
