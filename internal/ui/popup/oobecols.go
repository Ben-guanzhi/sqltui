package popup

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/config"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	ui.Factories["oobecols"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		cols := ctx.ColumnNames()
		if len(cols) == 0 {
			return nil, fmt.Errorf("no columns available")
		}
		selected := make(map[string]bool)
		saved := config.ReadOobeSummaryColumns()
		if len(saved) > 0 {
			for _, c := range saved {
				selected[c] = true
			}
		} else {
			// default: timestamp and body
			selected["timestamp"] = true
			selected["body"] = true
		}
		return &oobeColPicker{cols: cols, selected: selected, cursor: 0}, nil
	}
}

type oobeColPicker struct {
	cols     []string
	selected map[string]bool
	cursor   int
	offset   int
}

func (o *oobeColPicker) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "esc", "q":
		return o, ui.CloseOverlay
	case "up", "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "down", "j":
		if o.cursor < len(o.cols)-1 {
			o.cursor++
		}
	case "space", " ":
		o.selected[o.cols[o.cursor]] = !o.selected[o.cols[o.cursor]]
	case "a":
		// toggle select all
		allSelected := true
		for _, c := range o.cols {
			if !o.selected[c] {
				allSelected = false
				break
			}
		}
		for _, c := range o.cols {
			o.selected[c] = !allSelected
		}
	case "enter":
		var chosen []string
		// preserve original column order
		for _, c := range o.cols {
			if o.selected[c] {
				chosen = append(chosen, c)
			}
		}
		if len(chosen) == 0 {
			return o, nil
		}
		if err := config.WriteOobeSummaryColumns(chosen); err != nil {
			e := err
			return o, func() tea.Msg { return ui.ErrorMsg{Err: e} }
		}
		msg := chosen
		return o, tea.Sequence(
			ui.CloseOverlay,
			func() tea.Msg { return ui.OobeColsSelectedMsg{Columns: msg} },
		)
	}
	return o, nil
}

func (o *oobeColPicker) View(w, h int, th *theme.Theme) string {
	const bw = 54
	inner := bw - 2
	var lines []string
	maxRows := h - 6
	if maxRows < 1 {
		maxRows = 1
	}
	// Edge-follow: keep cursor visible within the window.
	if o.cursor < o.offset {
		o.offset = o.cursor
	}
	if o.cursor >= o.offset+maxRows {
		o.offset = o.cursor - maxRows + 1
	}
	if o.offset < 0 {
		o.offset = 0
	}
	start := o.offset
	end := start + maxRows
	if end > len(o.cols) {
		end = len(o.cols)
	}
	for i := start; i < end; i++ {
		c := o.cols[i]
		check := "  "
		if o.selected[o.cols[i]] {
			check = "✓ "
		}
		label := oobePad(fmt.Sprintf(" %s %s", check, c), inner)
		if i == o.cursor {
			lines = append(lines, th.ListSelected.Render(label))
		} else {
			lines = append(lines, th.ListItem.Render(label))
		}
	}
	selectedCount := 0
	for _, v := range o.selected {
		if v {
			selectedCount++
		}
	}
	hint := th.Subtle.Render(oobePad(fmt.Sprintf(" space toggle  a all  enter save (%d selected)", selectedCount), inner))
	lines = append(lines, hint)
	body := strings.Join(lines, "\n")
	return ui.Box("select columns", body, bw, th)
}
