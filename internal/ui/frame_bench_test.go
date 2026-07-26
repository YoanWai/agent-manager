package ui

import "testing"

func BenchmarkListFrame(b *testing.B) {
	m := shotModel()
	m.width, m.height = 200, 50
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
