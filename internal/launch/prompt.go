package launch

import "strings"

func DeliveredPrompt(text string) string {
	if text == DeferredRenameDirective || text == CoordinationNote {
		return ""
	}
	text = strings.TrimPrefix(text, CoordinationNote+"\n\n")
	text = strings.TrimPrefix(text, RenameDirective+"\n\n")
	return strings.TrimPrefix(text, RenameAvailableNote+"\n\n")
}
