package tmux

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// ClipboardWrite identifies the pane requesting a terminal clipboard write.
type ClipboardWrite struct {
	PaneID string
	Text   string
}

const clipboardIntroducer = "\x1b]52;"

// Bound unfinished control sequences from pane output.
const clipboardSequenceLimit = 1 << 20

// Each pane keeps partial sequences across control-mode output notifications.
type clipboardScanner struct {
	pending map[string]string
}

func (s *clipboardScanner) feed(pane, output string) []string {
	if s.pending == nil {
		s.pending = map[string]string{}
	}
	data := s.pending[pane] + output
	delete(s.pending, pane)
	var copies []string
	for {
		start := strings.Index(data, clipboardIntroducer)
		if start < 0 {
			if tail := introducerPrefix(data); tail != "" {
				s.pending[pane] = tail
			}
			return copies
		}
		data = data[start:]
		body := data[len(clipboardIntroducer):]
		end := strings.IndexAny(body, "\a\x1b")
		if end < 0 || (body[end] == '\x1b' && end+1 == len(body)) {
			if len(data) <= clipboardSequenceLimit {
				s.pending[pane] = data
			}
			return copies
		}
		terminated := body[end] == '\a' || body[end+1] == '\\'
		if terminated && len(clipboardIntroducer)+end <= clipboardSequenceLimit {
			if text, ok := clipboardText(body[:end]); ok {
				copies = append(copies, text)
			}
		}
		data = body[end:]
	}
}

func introducerPrefix(data string) string {
	for size := len(clipboardIntroducer) - 1; size > 0; size-- {
		if strings.HasSuffix(data, clipboardIntroducer[:size]) {
			return data[len(data)-size:]
		}
	}
	return ""
}

func clipboardText(body string) (string, bool) {
	_, payload, ok := strings.Cut(body, ";")
	if !ok || payload == "" || payload == "?" {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

// Control mode octal-escapes control bytes and backslashes.
func unescapeOutput(escaped string) string {
	if !strings.Contains(escaped, `\`) {
		return escaped
	}
	var out strings.Builder
	out.Grow(len(escaped))
	for index := 0; index < len(escaped); index++ {
		if escaped[index] == '\\' && index+3 < len(escaped) {
			if value, err := strconv.ParseUint(escaped[index+1:index+4], 8, 8); err == nil {
				out.WriteByte(byte(value))
				index += 3
				continue
			}
		}
		out.WriteByte(escaped[index])
	}
	return out.String()
}
