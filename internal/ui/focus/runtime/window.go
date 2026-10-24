package runtime

import "strings"

// Window is the rows-high window offset rows up from the bottom of a history
// capture, in the capture's shape.
func Window(capture string, rows, offset int) string {
	lines := strings.Split(strings.TrimSuffix(capture, "\n"), "\n")
	end := max(len(lines)-offset, 0)
	start := max(end-rows, 0)
	return strings.Join(lines[start:end], "\n") + "\n"
}
