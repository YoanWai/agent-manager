package launch

import "strings"

func DeliveredPrompt(text string) string {
	if text == DeferredRenameDirective {
		return ""
	}
	for _, note := range []string{ProactiveCoordinationNote, OnRequestCoordinationNote} {
		if text == note {
			return ""
		}
		text = strings.TrimPrefix(text, note+"\n\n")
	}
	text = strings.TrimPrefix(text, RenameDirective+"\n\n")
	return strings.TrimPrefix(text, RenameAvailableNote+"\n\n")
}
