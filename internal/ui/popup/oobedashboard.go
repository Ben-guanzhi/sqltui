package popup

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/db/oobe"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// async message

type panelDataMsg struct {
	owner    *panelViewerOverlay
	panelIdx int
	data     []map[string]any
	err      error
}

// panelViewerOverlay shows one dashboard's panels full-screen, navigable with left/right.
type panelViewerOverlay struct {
	ctx       ui.AppContext
	backend   *oobe.Backend
	dashboard oobe.DashboardInfo

	panels []oobe.Panel // pre-populated from DashboardInfo.Panels

	panelIdx         int
	tableOffset      int // horizontal/row scroll offset for chart panels
	sidebarOff       int // scroll offset for sidebar panel list
	timeRangeMinutes int // current time range shown in header
	data             []map[string]any
	dataErr          string
	dataLoading      bool
}

func newPanelViewerOverlay(ctx ui.AppContext, be *oobe.Backend, dash oobe.DashboardInfo) *panelViewerOverlay {
	return &panelViewerOverlay{
		ctx:              ctx,
		backend:          be,
		dashboard:        dash,
		panels:           dash.Panels,
		timeRangeMinutes: be.GetTimeRange(),
	}
}

func (v *panelViewerOverlay) Fullscreen() bool { return true }

// Init loads data for the first panel immediately.
func (v *panelViewerOverlay) Init() tea.Cmd {
	if len(v.dashboard.Panels) > 0 {
		v.panels = v.dashboard.Panels
		if len(v.panels) > 0 {
			return v.loadPanelData(0)
		}
		return nil
	}
	return nil
}

func (v *panelViewerOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	switch m := msg.(type) {
	case panelDataMsg:
		if m.owner != v {
			return v, nil
		}
		v.dataLoading = false
		if m.panelIdx != v.panelIdx {
			return v, nil // stale result
		}
		if m.err != nil {
			v.dataErr = m.err.Error()
		} else {
			v.data = m.data
			v.dataErr = ""
		}
		return v, nil

	case tea.KeyPressMsg:
		return v.handleKey(m)
	}
	return v, nil
}

func (v *panelViewerOverlay) loadPanelData(idx int) tea.Cmd {
	if idx < 0 || idx >= len(v.panels) {
		return nil
	}
	panel := v.panels[idx]
	if len(panel.Queries) == 0 || panel.Queries[0].SQL == "" {
		return nil
	}
	v.dataLoading = true
	v.data = nil
	v.dataErr = ""
	be := v.backend
	owner := v
	dash := v.dashboard
	q := panel.Queries[0]
	fallback := q.XAlias
	if fallback == "" {
		fallback = "_timestamp"
	}
	queryCtx := oobe.PanelQueryContext{
		DashboardID:   dash.DashboardID,
		DashboardName: dash.Title,
		FolderID:      dash.FolderID,
		FolderName:    dash.FolderName,
		PanelID:       panel.ID,
		PanelName:     panel.Title,
		FallbackCol:   fallback,
		SQL:           q.SQL,
	}
	return func() tea.Msg {
		data, err := be.QueryPanel(queryCtx)
		return panelDataMsg{owner: owner, panelIdx: idx, data: data, err: err}
	}
}

func (v *panelViewerOverlay) handleKey(msg tea.KeyPressMsg) (ui.Overlay, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		return v, ui.CloseOverlay
	case "up", "k":
		if v.panelIdx > 0 {
			v.panelIdx--
			v.tableOffset = 0
			// edge-follow: scroll sidebar up if cursor moves above visible window
			if v.panelIdx < v.sidebarOff {
				v.sidebarOff = v.panelIdx
			}
			return v, v.loadPanelData(v.panelIdx)
		}
	case "down", "j":
		if v.panelIdx < len(v.panels)-1 {
			v.panelIdx++
			v.tableOffset = 0
			// edge-follow: scroll sidebar down handled in View where listH is known
			return v, v.loadPanelData(v.panelIdx)
		}
	case "left", "h":
		if v.tableOffset > 0 {
			v.tableOffset--
		}
	case "right", "l":
		if v.tableOffset < len(v.data)-1 {
			v.tableOffset++
		}
	case "r":
		v.tableOffset = 0
		return v, v.loadPanelData(v.panelIdx)
	case "p":
		sel := newTimeOnlySelector(v.timeRangeMinutes, func(min int) tea.Cmd {
			v.backend.SetTimeRange(min)
			v.backend.ResetTimeWindow()
			v.timeRangeMinutes = min
			v.tableOffset = 0
			return v.loadPanelData(v.panelIdx)
		})
		return v, func() tea.Msg { return ui.PushOverlayMsg{Overlay: sel} }
	}
	return v, nil
}

func (v *panelViewerOverlay) View(w, h int, th *theme.Theme) string {
	dashTitle := v.dashboard.Title
	if dashTitle == "" {
		dashTitle = v.dashboard.DashboardID
	}

	// Header line
	var headerText string
	if len(v.panels) == 0 {
		headerText = fmt.Sprintf(" Dashboard: %s  |  no panels", dashTitle)
	} else {
		panel := v.panels[v.panelIdx]
		mainPart := fmt.Sprintf(" Dashboard: %s  |  Panel %d/%d: %s [%s]",
			dashTitle, v.panelIdx+1, len(v.panels), panel.Title, panel.Type)
		timeLabel := " last " + formatTimeRange(v.timeRangeMinutes) + " "
		padW := w - ansi.StringWidth(mainPart) - ansi.StringWidth(timeLabel)
		if padW < 0 {
			padW = 0
		}
		headerText = mainPart + strings.Repeat(" ", padW) + timeLabel
	}
	hint := " ↑/↓ select panel  h/l scroll  r refresh  p time  esc back"

	bodyH := h - 2
	if bodyH < 1 {
		bodyH = 1
	}

	// Layout widths
	sidebarW := w / 4
	if sidebarW < 6 {
		sidebarW = 6
	}
	chartW := w - sidebarW - 1 // 1 char for "|" separator
	if chartW < 1 {
		chartW = 1
	}
	chartH := bodyH

	// Edge-follow: ensure panelIdx is visible in sidebar list.
	// The sidebar has 1 title row + (bodyH-1) list rows.
	listH := bodyH - 1
	if listH < 1 {
		listH = 1
	}
	if v.panelIdx < v.sidebarOff {
		v.sidebarOff = v.panelIdx
	}
	if v.panelIdx >= v.sidebarOff+listH {
		v.sidebarOff = v.panelIdx - listH + 1
	}
	if v.sidebarOff < 0 {
		v.sidebarOff = 0
	}

	// Build sidebar lines (bodyH total)
	sidebarLines := make([]string, 0, bodyH)
	// Row 0: "Panels" title
	sidebarLines = append(sidebarLines, th.Header.Render(sidebarPad("Panels", sidebarW)))
	// Rows 1..bodyH-1: panel list items
	for row := 0; row < listH; row++ {
		idx := v.sidebarOff + row
		if idx >= len(v.panels) {
			sidebarLines = append(sidebarLines, sidebarPad("", sidebarW))
			continue
		}
		label := sidebarPad(v.panels[idx].Title, sidebarW)
		if idx == v.panelIdx {
			sidebarLines = append(sidebarLines, th.ListSelected.Render(label))
		} else {
			sidebarLines = append(sidebarLines, th.ListItem.Render(label))
		}
	}

	// Build chart lines (bodyH total)
	var chartArea string
	if len(v.panels) == 0 {
		chartArea = centerText("no panels in this dashboard", chartW, chartH, th)
	} else if v.dataLoading {
		chartArea = centerText("loading data...", chartW, chartH, th)
	} else if v.dataErr != "" {
		chartArea = centerText("error: "+v.dataErr, chartW, chartH, th)
	} else {
		panel := v.panels[v.panelIdx]
		xAlias, yAlias := "", ""
		if len(panel.Queries) > 0 {
			xAlias = panel.Queries[0].XAlias
			yAlias = panel.Queries[0].YAlias
		}
		chartArea = renderChartForPanel(v.data, panel.Type, xAlias, yAlias, chartW, chartH, v.tableOffset, th)
	}
	chartLines := strings.Split(centerBlock(chartArea, chartW), "\n")
	for len(chartLines) < chartH {
		chartLines = append(chartLines, "")
	}
	if len(chartLines) > chartH {
		chartLines = chartLines[:chartH]
	}

	// Join sidebar and chart line-by-line, pad every row to exactly w display columns
	// so FillPage/Composite sees bw=w and does not add centering offset.
	var body strings.Builder
	for i := 0; i < bodyH; i++ {
		var sl, cl string
		if i < len(sidebarLines) {
			sl = sidebarLines[i]
		} else {
			sl = sidebarPad("", sidebarW)
		}
		if i < len(chartLines) {
			cl = chartLines[i]
		}
		line := sl + "│" + cl
		// Pad to exactly w display columns so Composite doesn't center the box.
		if pad := w - ansi.StringWidth(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		body.WriteString(line)
		if i < bodyH-1 {
			body.WriteString("\n")
		}
	}

	var sb strings.Builder
	sb.WriteString(th.Header.Render(padLineToWidth(headerText, w)))
	sb.WriteString("\n")
	sb.WriteString(body.String())
	sb.WriteString("\n")
	sb.WriteString(th.Subtle.Render(padLineToWidth(hint, w)))
	return sb.String()
}

func padLineToWidth(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw < w {
		s += strings.Repeat(" ", w-sw)
	} else if sw > w {
		s = ansi.Truncate(s, w, "")
	}
	return s
}

// sidebarPad pads or truncates s to exactly w bytes (ASCII-safe for panel titles).
func sidebarPad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		r = r[:w]
	}
	result := string(r)
	for len([]rune(result)) < w {
		result += " "
	}
	return result
}

func formatTimeRange(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dmin", minutes)
	}
	return fmt.Sprintf("%dh", minutes/60)
}
