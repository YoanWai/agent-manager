package settings

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// Exit is what a key asks of the root beyond the dialog's own state.
type Exit uint8

const (
	Stay Exit = iota
	Save
	Update
	ReportBug
	OpenDocs
	OpenFeatureRequest
	RequestCLISupport
)

// Notice is what a key leaves on the status bar: nothing, a cleared bar,
// or an error.
type Notice struct {
	Changed bool
	Text    string
}

func clearNotice() Notice            { return Notice{Changed: true} }
func errorNotice(text string) Notice { return Notice{Changed: true, Text: text} }

// Context is what a key reads from the root: the tools, the cached
// hidden CLIs and default tool, and the live key tables.
type Context struct {
	Tools       Tools
	Hidden      map[string]bool
	DefaultTool string
	Session     keybind.Table
	List        keybind.Table
	// UpdateReady says the version row offers a release to update to.
	UpdateReady bool
}

// Result is a key's outcome for the root to run.
type Result struct {
	Exit   Exit
	Notice Notice
	// Palette asks the root to repaint with ThemeIndex. DetectTheme asks it
	// to resolve the auto-detected theme first and show it with ShowTheme.
	Palette     bool
	DetectTheme bool
	// Background asks the root to preview TerminalBackground.
	Background bool
	Glyphs     bool
	// SaveHidden is the CLI picker closing; SaveKeys the key picker.
	SaveHidden bool
	SaveKeys   bool
	// LoadPrompts asks for the saved prompts catalog; SavePrompt and
	// DeletePrompt write it. Each carries the panel's Gen.
	LoadPrompts  bool
	SavePrompt   *PromptSave
	DeletePrompt string
	// Cmd is the typed editor line's own command.
	Cmd tea.Cmd
}

// Key runs one key on the open dialog.
func (s *State) Key(msg tea.KeyMsg, ctx Context) Result {
	if s.Prompts.open {
		return s.promptsKey(msg)
	}
	if s.KeyPicker && !s.CLIPicker {
		return s.keyPickerKey(msg)
	}
	if s.CLIPicker {
		return s.cliPickerKey(msg, ctx)
	}
	if s.Editor.typing {
		return Result{Cmd: s.editorTypingKey(msg)}
	}
	switch msg.String() {
	case "up", "k":
		s.Field = (s.Field + FieldCount - 1) % FieldCount
	case "down", "j":
		s.Field = (s.Field + 1) % FieldCount
	case "left", "h":
		return s.Cycle(-1)
	case "right", "l":
		return s.Cycle(1)
	case "enter":
		switch s.Field {
		case FieldDocs:
			return Result{Exit: OpenDocs}
		case FieldBugReport:
			return Result{Exit: ReportBug}
		case FieldFeatureRequest:
			return Result{Exit: OpenFeatureRequest}
		case FieldCLIs:
			s.openCLIPicker(ctx)
			return Result{}
		case FieldPrompts:
			return s.openPrompts()
		case FieldKeybindings:
			s.openKeyPicker(ctx)
			return Result{Notice: clearNotice()}
		case FieldEditor:
			if s.Editor.custom {
				s.openEditorTyping()
				return Result{}
			}
		case FieldUpdate:
			return Result{Exit: Update}
		}
		return Result{Exit: Save}
	case "esc":
		return Result{Exit: Save}
	}
	return Result{}
}

// Cycle steps the focused setting by one. The theme applies as it is
// stepped so the picker doubles as a live preview of the palette.
func (s *State) Cycle(step int) Result {
	switch s.Field {
	case FieldTool:
		count := len(s.ToolNames)
		if count == 0 {
			return Result{}
		}
		s.ToolIndex = (s.ToolIndex + step + count) % count
	case FieldTheme:
		// Stepping the theme is a manual choice; it wins over auto-detect
		// rather than being silently overridden on the next start.
		s.ThemeAuto = false
		s.ThemeIndex = (s.ThemeIndex + step + len(uitheme.Palettes)) % len(uitheme.Palettes)
		s.ManualTheme = uitheme.Palettes[s.ThemeIndex].Name
		s.Dirty = true
		return Result{Palette: true}
	case FieldThemeAuto:
		s.ThemeAuto = !s.ThemeAuto
		s.ThemeIndex = uitheme.Index(s.ManualTheme)
		s.Dirty = true
		return Result{Palette: true, DetectTheme: s.ThemeAuto}
	case FieldBackground:
		s.TerminalBackground = !s.TerminalBackground
		s.Dirty = true
		return Result{Background: true}
	case FieldGlyphs:
		delete(s.Baseline, GlyphModeSetting)
		if s.GlyphMode.Effective() == presentation.GlyphModeAdvanced {
			s.GlyphMode = presentation.GlyphModeBasic
		} else {
			s.GlyphMode = presentation.GlyphModeAdvanced
		}
		s.Dirty = true
		return Result{Glyphs: true}
	case FieldEditor:
		s.Editor.cycle(step)
	default:
		setting := byField[s.Field]
		if setting == nil || setting.Flag == nil {
			return Result{}
		}
		flag := setting.Flag(s)
		*flag = !*flag
	}
	s.Dirty = true
	return Result{}
}

// OpenPrompts opens the saved prompts panel from its row.
func (s *State) OpenPrompts() Result {
	s.Field = FieldPrompts
	return s.openPrompts()
}

// ShowTheme points the theme row at the palette auto-detect resolved.
func (s *State) ShowTheme(name string) {
	s.ThemeIndex = uitheme.Index(name)
}
