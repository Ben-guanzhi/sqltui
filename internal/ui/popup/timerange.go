// Package popup - unified OpenObserve query-params selector.
package popup

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	f := func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		p := newQueryParams()
		type oobeGetter interface {
			GetTimeRange() int
			GetPage() int
			GetPageSize() int
		}
		if b := ctx.Backend(); b != nil {
			if oo, ok := b.(oobeGetter); ok {
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
	pageInput     []rune
	pageInputCur  int
	pageSizeInput []rune
	pageSizeCur   int
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
		pageInput:     []rune("1"),
		pageInputCur:  1,
		pageSizeInput: []rune("200"),
		pageSizeCur:   3,
	}
}

func (q *queryParams) initFromBackend(minutes, page, pageSize int) {
	for i, o := range trOptions {
		if o.minutes == minutes {
			q.trSel = i
			break
		}
	}
	ps := strconv.Itoa(page + 1)
	q.pageInput = []rune(ps)
	q.pageInputCur = len(q.pageInput)
	pss := strconv.Itoa(pageSize)
	q.pageSizeInput = []rune(pss)
	q.pageSizeCur = len(q.pageSizeInput)
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
				q.pageInput = []rune("1")
				q.pageInputCur = 1
			}
		case "down", "j":
			if q.trSel < len(trOptions)-1 {
				q.trSel++
				q.pageInput = []rune("1")
				q.pageInputCur = 1
			}
		}
	case secPage:
		if !q.timeOnly {
			qEditRune(key, &q.pageInput, &q.pageInputCur)
		}
	case secPageSize:
		if !q.timeOnly {
			qEditRune(key, &q.pageSizeInput, &q.pageSizeCur)
		}
	}
	return q, nil
}

func qEditRune(key tea.KeyPressMsg, buf *[]rune, cur *int) {
	k := key.String()
	switch k {
	case "backspace", "ctrl+h":
		if *cur > 0 {
			*buf = append((*buf)[:*cur-1], (*buf)[*cur:]...)
			*cur--
		}
	case "delete":
		if *cur < len(*buf) {
			*buf = append((*buf)[:*cur], (*buf)[*cur+1:]...)
		}
	case "left":
		if *cur > 0 {
			*cur--
		}
	case "right":
		if *cur < len(*buf) {
			*cur++
		}
	case "home", "ctrl+a":
		*cur = 0
	case "end", "ctrl+e":
		*cur = len(*buf)
	case "ctrl+u":
		*buf = nil
		*cur = 0
	default:
		if t := key.Key().Text; t != "" && key.Mod == 0 {
			for _, ch := range []rune(t) {
				if ch >= '0' && ch <= '9' {
					*buf = append((*buf)[:*cur], append([]rune{ch}, (*buf)[*cur:]...)...)
					*cur++
				}
			}
		}
	}
}

func (q *queryParams) submit() tea.Cmd {
	minutes := trOptions[q.trSel].minutes
	if q.timeOnly && q.onConfirm != nil {
		return tea.Sequence(ui.CloseOverlay, q.onConfirm(minutes))
	}
	page := 0
	if v, err := strconv.Atoi(string(q.pageInput)); err == nil && v >= 1 {
		page = v - 1
	}
	pageSize := 200
	if v, err := strconv.Atoi(string(q.pageSizeInput)); err == nil && v >= 1 {
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
		lines = append(lines, th.ListSelected.Render(qpPad("▸ Time Range", inner)))
	} else {
		lines = append(lines, th.Subtle.Render(qpPad("  Time Range", inner)))
	}
	for i, o := range trOptions {
		label := qpPad("   "+o.label, inner)
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
			lines = append(lines, th.ListSelected.Render(qpPad("▸ Page", inner)))
		} else {
			lines = append(lines, th.Subtle.Render(qpPad("  Page", inner)))
		}
		lines = append(lines, "  "+qpRenderInput(q.pageInput, q.pageInputCur, q.focus == secPage, inner-2, th))

		lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))

		// page size section
		if q.focus == secPageSize {
			lines = append(lines, th.ListSelected.Render(qpPad("▸ Page Size", inner)))
		} else {
			lines = append(lines, th.Subtle.Render(qpPad("  Page Size", inner)))
		}
		lines = append(lines, "  "+qpRenderInput(q.pageSizeInput, q.pageSizeCur, q.focus == secPageSize, inner-2, th))
	}

	lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))
	lines = append(lines, th.Subtle.Render(qpPad(" ↑/↓ select  enter ok  esc cancel", inner)))

	body := strings.Join(lines, "\n")
	title := "query params"
	if q.timeOnly {
		title = "time range"
	}
	return ui.Box(title, body, qpWidth, th)
}

func qpRenderInput(buf []rune, cur int, active bool, w int, th *theme.Theme) string {
	s := string(buf)
	if active {
		if cur <= len(buf) {
			s = string(buf[:cur]) + "█" + string(buf[cur:])
		}
		return th.Subtle.Render(qpPad(s, w+1))
	}
	return th.Subtle.Render(qpPad("["+s+"]", w))
}

func qpPad(s string, w int) string {
	for len(s) < w {
		s += " "
	}
	if len(s) > w {
		s = s[:w]
	}
	return s
}
