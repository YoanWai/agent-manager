package ui

import (
	"fmt"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

func BenchmarkListFrame(b *testing.B) {
	m := shotModel()
	m.layout.width, m.layout.height = 200, 50
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = preparedView(m)
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
		seedReviewForTest(m, uireview.Target{ID: "bench"}, git.ScopeUncommitted, "/repo", diff.Set{Files: files}, true)
		b.StartTimer()
		m.closeDiff()
	}
}
