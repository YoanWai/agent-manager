//go:build windows

package agentsession

import (
	"net/url"
	"path/filepath"
	"strings"
)

// fileURIPath is the directory a file URI names. agy spells a Windows
// directory file:///C:/Users/..., whose parsed path carries the leading
// slash and forward separators.
func fileURIPath(u *url.URL) string {
	return filepath.FromSlash(strings.TrimPrefix(u.Path, "/"))
}
