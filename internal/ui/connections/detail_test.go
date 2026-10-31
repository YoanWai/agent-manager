package connections

import (
	"strings"
	"testing"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	"github.com/charmbracelet/x/ansi"
)

func TestConnectionDetailPadsItsFactLabels(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{{Name: "box", Destination: "me@box", Binary: "/opt/am"}})
	lines, _ := c.Content(Row{Kind: ConnectionRow, Host: "box"}, 60, 4, Look{})
	var text []string
	for _, line := range lines {
		text = append(text, ansi.Strip(line.Text))
	}
	joined := strings.Join(text, "\n")
	for _, want := range []string{"ssh to me@box", "binary /opt/am"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("detail missing %q:\n%s", want, joined)
		}
	}
}
