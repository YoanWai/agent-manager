//go:build windows

package mcpreg

import "testing"

func TestPythonFromVersionReadsWindowsInstallDirectory(t *testing.T) {
	version := "Hermes Agent v0.20.0 (2026.8.3)\r\n" +
		"Install directory: C:\\Users\\me\\hermes\\venv\\lib\\site-packages\r\n" +
		"Python: 3.14.7\r\n"
	want := `C:\Users\me\hermes\venv\Scripts\python.exe`
	if got := pythonFromVersion(version); got != want {
		t.Fatalf("pythonFromVersion = %q, want %q", got, want)
	}
	elsewhere := "Install directory: C:\\hermes\\src\\site-packages\r\n"
	if got := pythonFromVersion(elsewhere); got != "" {
		t.Fatalf("site-packages outside Lib = %q, want no interpreter", got)
	}
}
