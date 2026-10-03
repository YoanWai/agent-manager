//go:build windows

package agentsession

import "strings"

// fileURISpelling is the path part agy writes in a file URI: the directory
// with forward separators behind the same three slashes a POSIX absolute
// path puts there, as file:///C:/Users/....
func fileURISpelling(dir string) string {
	return "/" + strings.ReplaceAll(dir, `\`, "/")
}
