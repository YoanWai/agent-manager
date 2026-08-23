package wsl

import (
	"os"
	"path/filepath"
	"testing"
)

const origReleaseFile = "/proc/sys/kernel/osrelease"

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		release string
		want    bool
	}{
		{name: "distro name env", env: map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, want: true},
		{name: "interop env", env: map[string]string{"WSL_INTEROP": "/run/WSL/1_interop"}, want: true},
		{name: "microsoft kernel", release: "6.6.87-microsoft-standard-WSL2", want: true},
		{name: "wsl kernel", release: "5.15.90.1-wsl", want: true},
		{name: "plain linux kernel", release: "6.8.0-generic", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() { osReleaseFile = origReleaseFile }()
			t.Setenv("WSL_DISTRO_NAME", "")
			t.Setenv("WSL_INTEROP", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.release != "" {
				osReleaseFile = filepath.Join(t.TempDir(), "osrelease")
				if err := os.WriteFile(osReleaseFile, []byte(tt.release), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := Detect(); got != tt.want {
				t.Fatalf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectUnreadableReleaseFile(t *testing.T) {
	defer func() { osReleaseFile = origReleaseFile }()
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	osReleaseFile = filepath.Join(t.TempDir(), "missing")
	if Detect() {
		t.Fatal("unreadable release file should not mark WSL")
	}
}
