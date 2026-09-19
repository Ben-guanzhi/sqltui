package dbmode

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/config"
	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// fetchLimit caps how many rows a live table preview loads (matching the
// legacy tree-view behavior).
const fetchLimit = 200

// openTableFactory backs the "opentable" command dispatched by the schema
// browser (arg is "ns\ttable"). It shows a small loading overlay while the
// rows are fetched asynchronously.
func openTableFactory(ctx ui.AppContext, arg string) (ui.Overlay, error) {
	be := ctx.Backend()
	if be == nil {
		return nil, fmt.Errorf("opentable: no live database connection")
	}
	ns, table := splitTableArg(arg)
	if strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("opentable: no table name")
	}
	return &tableLoader{be: be, ns: ns, table: table}, nil
}

// tableLoadedMsg carries the fetched rows (or the fetch error) back to the
// loading overlay.
type tableLoadedMsg struct {
	table string
	frame *data.Frame
	err   error
}

// tableLoader is a transient overlay: it kicks off the fetch on Init and
// replaces itself with a new tab (or an error popup) when the result lands.
type tableLoader struct {
	be        db.Backend
	ns, table string
}

func (o *tableLoader) Init() tea.Cmd {
	be, ns, table := o.be, o.ns, o.table
	return func() tea.Msg {
		f, err := be.FetchTable(ns, table, fetchLimit)
		return tableLoadedMsg{table: table, frame: f, err: err}
	}
}

func (o *tableLoader) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	switch m := msg.(type) {
	case tableLoadedMsg:
		return o, tea.Sequence(o.resultCmds(m)...)
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc", "q", "ctrl+c":
			// Abandon the view; a late result is dropped with the overlay.
			return o, ui.CloseOverlay
		}
	}
	return o, nil
}

// resultCmds is the ordered message sequence for a fetch outcome: close the
// loading overlay first, then open the tab or report the error.
func (o *tableLoader) resultCmds(m tableLoadedMsg) []tea.Cmd {
	if m.err != nil {
		err := m.err
		return []tea.Cmd{ui.CloseOverlay, func() tea.Msg { return ui.ErrorMsg{Err: err} }}
	}
	frame, title := m.frame, m.table
	msg := ui.ApplyFrameMsg{Frame: frame, NewTab: true, TabTitle: title, Crumb: "table"}
	// For OpenObserve streams, the frame has all columns (for row-detail) but
	// the table summary view should only show _time and body.
	if o.be.Kind() == "openobserve" {
		msg.TableCols = openobserveSummaryCols(frame)
	}
	return []tea.Cmd{ui.CloseOverlay, func() tea.Msg { return msg }}
}

// openobserveSummaryCols returns the column indices to display in the summary
// table view. It reads user-configured columns from config first, falling back
// to timestamp + body.
func openobserveSummaryCols(f *data.Frame) []int {
	// Try user-configured columns from config file first.
	if saved := config.ReadOobeSummaryColumns(); len(saved) > 0 {
		var idxs []int
		for _, name := range saved {
			for i, c := range f.Columns {
				if c.Name == name {
					idxs = append(idxs, i)
					break
				}
			}
		}
		if len(idxs) > 0 {
			return idxs
		}
	}
	// Default: timestamp + body.
	timeIdx, bodyIdx := -1, -1
	for i, c := range f.Columns {
		switch c.Name {
		case "timestamp":
			timeIdx = i
		case "body":
			bodyIdx = i
		}
	}
	if timeIdx < 0 || bodyIdx < 0 {
		return nil
	}
	return []int{timeIdx, bodyIdx}
}

func (o *tableLoader) View(width, height int, th *theme.Theme) string {
	w := len(o.table) + 24
	if w > width-4 {
		w = width - 4
	}
	if w < 24 {
		w = 24
	}
	body := th.Text.Render(" loading " + o.table + " ...")
	return ui.Box("table", body, w, th)
}
