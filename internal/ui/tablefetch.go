// TableLoader is the single transient overlay that fetches a live table's
// rows off the update loop. Two modes share the fetch, keys and view:
//
//   - open mode (Refresh=false, "opentable"): on success the frame is applied
//     as a new tab;
//   - refresh mode (Refresh=true, "refreshtable"): on success the active
//     pane's base frame is replaced in place (no new tab) and a toast shows.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/config"
	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/theme"
)

// TableFetchLimit caps how many rows a table fetch loads, matching the legacy
// tree-view behavior.
const TableFetchLimit = 200

// TableLoadedMsg carries the fetched rows (or the fetch error) back to the
// loading overlay.
type TableLoadedMsg struct {
	Table string
	Frame *data.Frame
	Err   error
}

// TableLoader is a transient overlay: it kicks off the fetch on Init and
// closes itself, applying the result (new tab or in-place refresh) or
// surfacing the error when it lands.
type TableLoader struct {
	Ctx     AppContext
	Be      db.Backend
	NS      string
	Table   string
	Refresh bool
}

// NewTableLoader builds a table-fetch overlay. refresh selects between
// replacing the active pane's base frame (true) and opening a new tab.
func NewTableLoader(ctx AppContext, be db.Backend, ns, table string, refresh bool) *TableLoader {
	return &TableLoader{Ctx: ctx, Be: be, NS: ns, Table: table, Refresh: refresh}
}

func (o *TableLoader) Init() tea.Cmd {
	be, ns, table := o.Be, o.NS, o.Table
	return func() tea.Msg {
		f, err := be.FetchTable(ns, table, TableFetchLimit)
		return TableLoadedMsg{Table: table, Frame: f, Err: err}
	}
}

func (o *TableLoader) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch m := msg.(type) {
	case TableLoadedMsg:
		return o, tea.Sequence(o.ResultCmds(m)...)
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc", "q", "ctrl+c":
			// Abandon the view; a late result is dropped with the overlay.
			return o, CloseOverlay
		}
	}
	return o, nil
}

// ResultCmds is the ordered message sequence for a fetch outcome: close the
// loading overlay first, then open the tab / replace the base frame / report
// the error.
func (o *TableLoader) ResultCmds(m TableLoadedMsg) []tea.Cmd {
	if m.Err != nil {
		err := m.Err
		return []tea.Cmd{CloseOverlay, func() tea.Msg { return ErrorMsg{Err: err} }}
	}
	if o.Refresh {
		paneID := o.Ctx.ActivePaneID()
		frame := m.Frame
		return []tea.Cmd{
			CloseOverlay,
			func() tea.Msg { return ReplaceBaseMsg{Frame: frame, PaneID: paneID} },
			func() tea.Msg { return ToastMsg{Text: "refreshed"} },
		}
	}
	frame, title := m.Frame, m.Table
	msg := ApplyFrameMsg{Frame: frame, NewTab: true, TabTitle: title, Crumb: "table"}
	// For OpenObserve streams, the frame has all columns (for row-detail) but
	// the table summary view should only show the summary columns.
	if o.Be.Kind() == "openobserve" {
		msg.TableCols = openobserveSummaryCols(frame)
	}
	return []tea.Cmd{CloseOverlay, func() tea.Msg { return msg }}
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

func (o *TableLoader) View(width, height int, th *theme.Theme) string {
	verb, boxTitle := " loading ", "table"
	if o.Refresh {
		verb, boxTitle = " refreshing ", "refresh"
	}
	w := len(o.Table) + 24
	if w > width-4 {
		w = width - 4
	}
	if w < 24 {
		w = 24
	}
	body := th.Text.Render(verb + o.Table + " ...")
	return Box(boxTitle, body, w, th)
}

// compile-time interface checks
var (
	_ Overlay       = (*TableLoader)(nil)
	_ OverlayIniter = (*TableLoader)(nil)
)
