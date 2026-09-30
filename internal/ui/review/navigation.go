package review

import (
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/diff"
)

var nonCodeExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".svg": true, ".woff": true, ".woff2": true, ".ttf": true,
	".otf": true, ".eot": true, ".wasm": true, ".bin": true, ".exe": true,
	".dll": true, ".so": true, ".dylib": true, ".o": true, ".a": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".mp3": true, ".mp4": true, ".wav": true, ".ogg": true,
	".avi": true, ".mov": true, ".pdf": true, ".lock": true,
	".class": true, ".pyc": true,
}

var nonCodeNames = map[string]bool{
	"package-lock.json": true, "pnpm-lock.yaml": true, "go.sum": true,
}

func isNonCode(fd *diff.FileDiff) bool {
	name := filepath.Base(fd.File.Path)
	return fd.Binary || fd.Stat.Binary || nonCodeNames[name] || nonCodeExts[strings.ToLower(filepath.Ext(name))]
}
