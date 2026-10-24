package ui

import uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"

// reviewRoot is the root as the review picker reads it: the card geometry the
// stack's host does not carry.
type reviewRoot struct{ *Model }

func (r reviewRoot) CardWidth(cols int) int { return uiframe.CardWidthFor(cols) }

func (r reviewRoot) InnerWidth(width int) int { return uiframe.CardInnerWidth(width) }
