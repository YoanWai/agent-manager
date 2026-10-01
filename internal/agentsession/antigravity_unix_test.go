//go:build !windows

package agentsession

// fileURISpelling is the path part agy writes in a file URI: a POSIX
// directory's own absolute path.
func fileURISpelling(dir string) string {
	return dir
}
