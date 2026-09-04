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
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p C:\134 rw,dirsync,aname=drvfs;path=C:\;uid=1000;gid=1000;symlinkroot=/mnt/,mmap,access=client,msize=65536,trans=fd,rfd=5,wfd=5`},
			path:  "/mnt/c/npm-global/claude",
			want:  true,
		},
		{
			// A drive shared over the virtio transport rather than a file
			// descriptor, and bound outside the automount root, as WSL does
			// when one distro shares another's mount.
			name:  "plan9 drive over virtio",
			lines: []string{rootMount, `194 193 0:46 / /mnt/wsl/c rw,noatime shared:2 - 9p drvfsa rw,dirsync,aname=drvfs;path=C:\;uid=1000;gid=1000;metadata;symlinkroot=/mnt/,mmap,access=client,msize=262144,trans=virtio`},
			path:  "/mnt/wsl/c/npm-global/claude",
			want:  true,
		},
		{
			// A Windows path with a space leaves a raw space in the super
			// options, so the line runs to more fields than the format's
			// usual count.
			name:  "drive whose windows path holds a space",
			lines: []string{rootMount, `2619 80 0:302 / /Docker/host rw,noatime - 9p drvfs rw,dirsync,aname=drvfs;path=C:\Program Files\Docker\Docker\resources;symlinkroot=/mnt/,mmap,access=client,msize=262144,trans=virtio`},
			path:  "/Docker/host/cli/docker",
			want:  true,
		},
		{
			// virtiofs carries no attach name at all, only the mount's own
			// options, so the filesystem type is all there is to go on.
			name:  "wsl2 virtiofs drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - virtiofs drvfsC0 rw,noatime`},
			path:  "/mnt/c/npm-global/claude",
			want:  true,
		},
		{
			name:  "wsl1 drvfs drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - drvfs C:\134 rw,noatime,uid=1000,gid=1000,case=off`},
			path:  "/mnt/c/npm-global/claude",
			want:  true,
		},
		{
			// The automount root is configurable, so the mount point says
			// nothing on its own.
			name:  "drive mounted outside /mnt",
			lines: []string{rootMount, `56 24 0:52 / /c rw,noatime - 9p drvfs rw,aname=drvfs;path=C:\`},
			path:  "/c/npm-global/claude",
			want:  true,
		},
		{
			name:  "mount point with an escaped space",
			lines: []string{rootMount, `56 24 0:52 / /mnt/my\040drive rw,noatime - 9p drvfs rw,aname=drvfs;path=D:\`},
			path:  "/mnt/my drive/claude",
			want:  true,
		},
		{
			// WSL serves its own init binary, libraries and GPU drivers
			// over the same filesystem type it shares drives over, and
			// those hold Linux files. One of them is on PATH.
			name: "wsl's own plan9 mounts",
			lines: []string{
				rootMount,
				`166 165 0:20 /init /init ro,relatime - 9p tools ro,dirsync,aname=tools;fmask=022,loose,access=client,msize=65536,trans=fd,rfd=6,wfd=6`,
				`191 165 0:55 / /usr/lib/wsl/drivers ro,nosuid,nodev,noatime - 9p drivers ro,dirsync,aname=drivers;fmask=333;dmask=222,mmap,access=client,msize=65536,trans=fd,rfd=4,wfd=4`,
				`192 165 0:56 / /usr/lib/wsl/lib ro,nosuid,nodev,noatime - 9p lib ro,dirsync,aname=lib;fmask=333;dmask=222,mmap,access=client,msize=65536,trans=fd,rfd=4,wfd=4`,
			},
			path: "/usr/lib/wsl/lib/libcuda.so",
			want: false,
		},
		{
			name:  "linux filesystem mounted under a drive",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p drvfs rw,aname=drvfs;path=C:\`, `57 56 0:53 / /mnt/c/tmp rw - tmpfs tmpfs rw`},
			path:  "/mnt/c/tmp/claude",
			want:  false,
		},
		{
			// Docker Desktop puts its Linux CLI on a loop device under the
			// cross-distro mount point, not on a drive share.
			name:  "docker desktop cli tools",
			lines: []string{rootMount, `100 24 7:0 / /mnt/wsl/docker-desktop/cli-tools ro,relatime - iso9660 /dev/loop0 ro,nojoliet,check=s,map=n,blocksize=2048`},
			path:  "/mnt/wsl/docker-desktop/cli-tools/usr/bin/docker",
			want:  false,
		},
		{
			// A drive whose name is a prefix of another path's must not
			// claim it.
			name:  "sibling of a drive mount",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p drvfs rw,aname=drvfs;path=C:\`},
			path:  "/mnt/cache/claude",
			want:  false,
		},
		{
			name:  "distro path",
			lines: []string{rootMount, `56 24 0:52 / /mnt/c rw,noatime - 9p drvfs rw,aname=drvfs;path=C:\`},
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

// The mount table is written by the kernel, but a parser that panics on
// an odd line would take the whole manager down; every line has to come
// back as a decision or be skipped.
func FuzzMountEntry(f *testing.F) {

	f.Add(rootMount)
	f.Add(`56 24 0:52 / /mnt/my\040drive rw shared:1 master:2 - virtiofs drvfs rw`)
	f.Add(" - ")
	f.Add("")
	f.Fuzz(func(t *testing.T, line string) {
		parsed, ok := mountEntry(line)
		if !ok {
			return
		}
		if parsed.fstype == "" {
			t.Fatalf("line %q parsed with no filesystem type", line)
		}
		parsed.isWindowsDrive()
		underMount(parsed.point, parsed.point)
	})
}
