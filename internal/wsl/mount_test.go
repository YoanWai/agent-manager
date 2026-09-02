package wsl

import (
	"os"
	"path/filepath"
	"testing"
)

// mountInfo writes a mountinfo fixture and points the reader at it. Each
// line is the real format: the mount point is the fifth field, and the
// filesystem type is the first field after the lone dash.
func mountInfo(t *testing.T, lines ...string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mountinfo")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	original := mountInfoFile
	mountInfoFile = path
	t.Cleanup(func() { mountInfoFile = original })
}

const rootMount = "23 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sdc rw,discard"

func TestOnWindowsMount(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		path  string
		want  bool
	}{
		{
			name:  "wsl2 plan9 drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p C:\ rw,dirsync,aname=drvfs;path=C:\;uid=1000`},
			path:  "/mnt/c/npm-global/claude",
			want:  true,
		},
		{
			name:  "wsl2 virtiofs drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - virtiofs drvfs rw`},
			path:  "/mnt/c/npm-global/claude",
			want:  true,
		},
		{
			name:  "wsl1 drvfs drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - drvfs C: rw,case=off`},
			path:  "/mnt/c/npm-global/claude",
			want:  true,
		},
		{
			// The automount root is configurable, so the mount point says
			// nothing on its own; the filesystem type is what decides.
			name:  "drive mounted outside /mnt",
			lines: []string{rootMount, `56 24 0:52 / /c rw,noatime - 9p C:\ rw`},
			path:  "/c/npm-global/claude",
			want:  true,
		},
		{
			name:  "mount point with an escaped space",
			lines: []string{rootMount, `56 24 0:52 / /mnt/my\040drive rw,noatime - 9p D:\ rw`},
			path:  "/mnt/my drive/claude",
			want:  true,
		},
		{
			name:  "linux filesystem mounted under a drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p C:\ rw`, `57 56 0:53 / /mnt/c/tmp rw - tmpfs tmpfs rw`},
			path:  "/mnt/c/tmp/claude",
			want:  false,
		},
		{
			// A drive whose name is a prefix of another path's must not
			// claim it.
			name:  "sibling of a drive mount",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p C:\ rw`},
			path:  "/mnt/cache/claude",
			want:  false,
		},
		{
			name:  "distro path",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p C:\ rw`},
			path:  "/home/dev/.local/bin/claude",
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mountInfo(t, tt.lines...)
			got, err := OnWindowsMount(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("OnWindowsMount(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestOnWindowsMountUnreadableFile(t *testing.T) {
	original := mountInfoFile
	mountInfoFile = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() { mountInfoFile = original })

	if _, err := OnWindowsMount("/mnt/c/claude"); err == nil {
		t.Fatal("an unreadable mount table must not pass for a distro path")
	}
}
