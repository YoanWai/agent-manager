package ui

import (
	"fmt"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
)

func BenchmarkListFrame(b *testing.B) {
	m := shotModel()
	m.width, m.height = 200, 50
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.viewFrame()
	}
}

func BenchmarkCloseLargeReview(b *testing.B) {
	m := shotModel()
	files := make([]diff.FileDiff, 10000)
	for i := range files {
		files[i].File = git.ChangedFile{Path: fmt.Sprintf("path/to/file-%05d.go", i)}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m.mode = modeDiff
		m.diff.active = true
		m.diff.sessID = "bench"
		m.diff.set = diff.Set{Files: files}
		b.StartTimer()
		m.closeDiff()
	}
}
