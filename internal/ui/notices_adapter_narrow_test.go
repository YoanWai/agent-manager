package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestClickOnStackedNoticeKeys(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{10, 13, 20, 35} {
			for _, action := range []string{"esc", "x", "r", "enter"} {
				t.Run(fmt.Sprintf("%s/%d/%s", mode, width, action), func(t *testing.T) {
					m := noticeModel(noticeStore(t), "v1.0.0")
					m.layout.width, m.layout.height = width, 40
					m.prefs.glyphMode = mode
					m.openNotices(noticeTip)
					frame := strings.Split(ansi.Strip(preparedView(m)), "\n")
					var hit keyHit
					found := false
					for _, candidate := range m.layout.keyHits {
						if candidate.Key.String() == action {
							hit = candidate
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("no painted target for %s", action)
					}
					if text := ansi.Cut(frame[hit.Y], hit.X0, hit.X1); strings.TrimSpace(text) == "" {
						t.Fatal("target covers only padding")
					}
					if action == "r" {
						for _, x := range []int{hit.X0 - 1, hit.X1} {
							_, cmd := leftClickCmd(m, x, hit.Y)
							if cmd != nil || m.release.Refreshing || !m.noticesOpen() {
								t.Fatal("padding triggered an action")
							}
						}
					}
					var cmd tea.Cmd
					m, cmd = leftClickCmd(m, hit.X0, hit.Y)
					switch action {
					case "esc":
						if m.noticesOpen() {
							t.Fatal("close key did not close messages")
						}
					case "x":
						m.drainEffects(t)
						if !m.notices.Dismissed(noticeTip) {
							t.Fatal("dismiss key did not dismiss")
						}
					case "r":
						if cmd == nil || !m.release.Refreshing {
							t.Fatal("refresh key did not start refresh")
						}
					case "enter":
						if cmd == nil {
							t.Fatal("open key returned no link command")
						}
					}
				})
			}
		}
	}
}

func TestClippedNoticeFooterTakesNoClick(t *testing.T) {
	for _, width := range []int{10, 13, 20} {
		for height := 4; height <= 14; height++ {
			m := noticeModel(noticeStore(t), "v1.0.0")
			m.layout.width, m.layout.height = width, height
			m.openNotices(noticeTip)
			preparedView(m)
			for _, hit := range m.layout.keyHits {
				if hit.Y >= height || hit.Y < 0 {
					t.Fatalf("%dx%d: invisible target %+v", width, height, hit)
				}
			}
		}
	}
}
