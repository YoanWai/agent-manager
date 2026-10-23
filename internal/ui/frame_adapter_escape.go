package ui

import "github.com/YoanWai/agent-manager/internal/ui/presentation"

func escapeControls(text string) string       { return presentation.EscapeControls(text) }
func escapeControlsInline(text string) string { return presentation.EscapeControlsInline(text) }
