package popup

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	ui.Factories["register"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		if ctx.Engine() == nil {
			return nil, fmt.Errorf("register: no embedded SQL engine (only available in file mode)")
		}
		if ctx.CurrentFrame() == nil {
			return nil, fmt.Errorf("register: no table open")
		}
		return &registerPrompt{input: ui.NewLineInput(strings.TrimSpace(arg))}, nil
	}
}

// registerPrompt asks for a table name to register the current frame under
// in the embedded SQL engine.
type registerPrompt struct {
	input  ui.LineInput
	errMsg string
}

// registerValidate checks a candidate table name; empty result means valid.
func registerValidate(name string) string {
	switch {
	case name == "":
		return "name must not be empty"
	case name == "_":
		return `"_" is reserved for the current frame`
	default:
		return ""
	}
}

// registerKey applies one key press. It returns a command to emit alongside
// closing, and whether to close.
func (p *registerPrompt) registerKey(key, text string) (cmd tea.Cmd, close bool) {
	switch key {
	case "esc", "ctrl+c":
		return nil, true
	case "enter":
		name := strings.TrimSpace(p.input.String())
		if msg := registerValidate(name); msg != "" {
			p.errMsg = msg
			return nil, false
		}
		return func() tea.Msg { return ui.RegisterTableMsg{Name: name} }, true
	case "backspace":
		p.input.Backspace()
	case "delete":
		p.input.Delete()
	case "left":
		p.input.Left()
	case "right":
		p.input.Right()
	case "home", "ctrl+a":
		p.input.Home()
	case "end", "ctrl+e":
		p.input.End()
	case "ctrl+u":
		p.input.CtrlU()
	default:
		if text != "" && !strings.ContainsAny(text, "\n\r") {
			p.input.InsertKey(text)
			p.errMsg = ""
		}
	}
	return nil, false
}

func (p *registerPrompt) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	if pm, ok := msg.(tea.PasteMsg); ok {
		// Insert the paste at the cursor as one edit, flattened to one line.
		if p.input.Paste(pm.Content) {
			p.errMsg = ""
		}
		return p, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	cmd, close := p.registerKey(key.String(), key.Text)
	if close {
		if cmd != nil {
			name := strings.TrimSpace(p.input.String())
			toast := func() tea.Msg {
				return ui.ToastMsg{Text: fmt.Sprintf("registered table %q", name)}
			}
			return p, tea.Batch(cmd, toast, ui.CloseOverlay)
		}
		return p, ui.CloseOverlay
	}
	return p, cmd
}

func (p *registerPrompt) View(width, height int, th *theme.Theme) string {
	boxW := 44
	if boxW > width-4 && width-4 >= 20 {
		boxW = width - 4
	}
	inner := boxW - 2

	line := " " + th.Subtle.Render("table name:") + " " + p.input.Render(th)

	body := line
	if p.errMsg != "" {
		body += "\n" + th.Error.Render(ansi.Truncate(" "+p.errMsg, inner, "…"))
	} else {
		body += "\n" + th.Subtle.Render(" enter register  •  esc cancel")
	}
	return ui.Box("register", body, boxW, th)
}
