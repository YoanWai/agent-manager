//go:build !windows

package agentsession

import "net/url"

// fileURIPath is the directory a file URI names. A POSIX file URI's path is
// the directory itself.
func fileURIPath(u *url.URL) string {
	return u.Path
}
