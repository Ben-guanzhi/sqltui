// Package popup - unified OpenObserve query-params selector.
package popup

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	f := func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		p := newQueryParams()
		if b := ctx.Backend(); b != nil {
			if oo, ok := b.(db.QueryParamsSetter); ok {
				p.initFromBackend(oo.GetTimeRange(), oo.GetPage(), oo.GetPageSize())
			}
		}
		return p, nil
	}
	ui.Factories["queryparams"] = f
	ui.Factories["timerange"] = f
}

type trOption struct {
	label   string
	minutes int
}

var trOptions = []trOption{
	{label: "Last  1 min", minutes: 1},
	{label: "Last  5 min", minutes: 5},
	{label: "Last 15 min", minutes: 15},
	{label: "Last 30 min", minutes: 30},
	{label: "Last  1 h  ", minutes: 60},
	{label: "Last  6 h  ", minutes: 360},
	{label: "Last 12 h  ", minutes: 720},
	{label: "Last 24 h  ", minutes: 1440},
}

const (
	secTimeRange = 0
	secPage      = 1
	secPageSize  = 2
)

type queryParams struct {
	focus         int
	trSel         int
	pageInput     ui.LineInput
	pageSizeInput ui.LineInput
	timeOnly      bool
	onConfirm     func(minutes int) tea.Cmd
}

// newTimeOnlySelector returns a queryParams configured as a time-range-only picker.
// onConfirm is called with the selected minutes when the user presses enter.
func newTimeOnlySelector(currentMinutes int, onConfirm func(int) tea.Cmd) *queryParams {
	p := newQueryParams()
	p.timeOnly = true
	p.onConfirm = onConfirm
	for i, o := range trOptions {
		if o.minutes == currentMinutes {
			p.trSel = i
			break
		}
	}
	return p
}

func newQueryParams() *queryParams {
	return &queryParams{
		trSel:         2,
		pageInput:     ui.NewLineInput("1"),
		pageSizeInput: ui.NewLineInput("200"),
	}
}

func (q *queryParams) initFromBackend(minutes, page, pageSize int) {
	for i, o := range trOptions {
		if o.minutes == minutes {
			q.trSel = i
			break
		}
	}
	q.pageInput.Set(strconv.Itoa(page + 1))
	q.pageSizeInput.Set(strconv.Itoa(pageSize))
}

func (q *queryParams) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return q, nil
	}
	k := key.String()
	switch k {
	case "esc":
		return q, ui.CloseOverlay
	case "enter":
		return q, q.submit()
	case "tab":
		if !q.timeOnly {
			q.focus = (q.focus + 1) % 3
		}
		return q, nil
	}
	switch q.focus {
	case secTimeRange:
		switch k {
		case "up", "k":
			if q.trSel > 0 {
				q.trSel--
				q.pageInput.Set("1")
			}
		case "down", "j":
			if q.trSel < len(trOptions)-1 {
				q.trSel++
				q.pageInput.Set("1")
			}
		}
	case secPage:
		if !q.timeOnly {
			qEditRune(key, &q.pageInput)
		}
	case secPageSize:
		if !q.timeOnly {
			qEditRune(key, &q.pageSizeInput)
		}
	}
	return q, nil
}

// qEditRune routes one key press into a numeric line input: the shared
// editing machine plus a digits-only insert policy.
func qEditRune(key tea.KeyPressMsg, in *ui.LineInput) {
	switch key.String() {
	case "backspace", "ctrl+h":
		in.Backspace()
	case "delete":
		in.Delete()
	case "left":
		in.Left()
	case "right":
		in.Right()
	case "home", "ctrl+a":
		in.Home()
	case "end", "ctrl+e":
		in.End()
	case "ctrl+u":
		in.Clear()
	default:
		if t := key.Key().Text; t != "" && key.Mod == 0 {
			var digits []rune
			for _, ch := range t {
				if ch >= '0' && ch <= '9' {
					digits = append(digits, ch)
				}
			}
			in.Insert(string(digits))
		}
	}
}

func (q *queryParams) submit() tea.Cmd {
	minutes := trOptions[q.trSel].minutes
	if q.timeOnly && q.onConfirm != nil {
		return tea.Sequence(ui.CloseOverlay, q.onConfirm(minutes))
	}
	page := 0
	if v, err := strconv.Atoi(q.pageInput.String()); err == nil && v >= 1 {
		page = v - 1
	}
	pageSize := 200
	if v, err := strconv.Atoi(q.pageSizeInput.String()); err == nil && v >= 1 {
		pageSize = v
	}
	return tea.Sequence(
		ui.CloseOverlay,
		func() tea.Msg {
			return ui.SetQueryParamsMsg{Minutes: minutes, Page: page, PageSize: pageSize}
		},
	)
}

const qpWidth = 30

func (q *queryParams) View(width, height int, th *theme.Theme) string {
	inner := qpWidth - 2
	var lines []string

	// time range section
	if q.focus == secTimeRange {
		lines = append(lines, th.ListSelected.Render(ui.Pad("▸ Time Range", inner)))
	} else {
		lines = append(lines, th.Subtle.Render(ui.Pad("  Time Range", inner)))
	}
	for i, o := range trOptions {
		label := ui.Pad("   "+o.label, inner)
		if i == q.trSel {
			lines = append(lines, th.Subtle.Render("  ●"+label[3:]))
		} else {
			lines = append(lines, th.Subtle.Render("   "+label[3:]))
		}
	}

	if !q.timeOnly {
		lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))

		// page section
		if q.focus == secPage {
			lines = append(lines, th.ListSelected.Render(ui.Pad("▸ Page", inner)))
		} else {
			lines = append(lines, th.Subtle.Render(ui.Pad("  Page", inner)))
		}
		lines = append(lines, "  "+qpRenderInput(&q.pageInput, q.focus == secPage, inner-2, th))

		lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))

		// page size section
		if q.focus == secPageSize {
			lines = append(lines, th.ListSelected.Render(ui.Pad("▸ Page Size", inner)))
		} else {
			lines = append(lines, th.Subtle.Render(ui.Pad("  Page Size", inner)))
		}
		lines = append(lines, "  "+qpRenderInput(&q.pageSizeInput, q.focus == secPageSize, inner-2, th))
	}

	lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))
	lines = append(lines, th.Subtle.Render(ui.Pad(" ↑/↓ select  enter ok  esc cancel", inner)))

	body := strings.Join(lines, "\n")
	title := "query params"
	if q.timeOnly {
		title = "time range"
	}
	return ui.Box(title, body, qpWidth, th)
}

func qpRenderInput(in *ui.LineInput, active bool, w int, th *theme.Theme) string {
	if active {
		r := []rune(in.String())
		cur := in.Cursor()
		if cur > len(r) {
			cur = len(r)
		}
		s := string(r[:cur]) + "█" + string(r[cur:])
		return th.Subtle.Render(ui.Pad(s, w+1))
	}
	return th.Subtle.Render(ui.Pad("["+in.String()+"]", w))
}
