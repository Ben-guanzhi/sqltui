package popup

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/theme"
)

// renderChartForPanel renders data for a given panel type.
// offset is used for table scrolling.
func renderChartForPanel(data []map[string]any, panelType, xAlias, yAlias string, w, h, offset int, th *theme.Theme) string {
	if len(data) == 0 {
		return centerText("(no data)", w, h, th)
	}
	switch panelType {
	case "table":
		return renderTableChart(data, offset, w, h, th)
	case "pie":
		xCol, yCol := xAlias, yAlias
		if xCol == "" || yCol == "" {
			xCol, yCol = detectXY(data)
		}
		return renderPieChart(data, xCol, yCol, w, h, th)
	case "scatter", "line", "bar", "area":
		result := renderBarChart(data, xAlias, yAlias, w, h, th)
		return result
	default:
		return renderTableChart(data, offset, w, h, th)
	}
}

// formatCellValue formats a cell value for display.
// Special-cases _timestamp columns (microsecond epoch) to human-readable time.
func formatCellValue(col string, v any) string {
	if col == "_timestamp" {
		var us int64
		switch val := v.(type) {
		case float64:
			us = int64(val)
		case int64:
			us = val
		}
		if us > 0 {
			t := time.UnixMicro(us).UTC()
			return t.Format("01-02 15:04:05")
		}
	}
	return fmt.Sprint(v)
}

// renderTableChart renders data as a scrollable ASCII table.
func renderTableChart(data []map[string]any, offset, w, h int, th *theme.Theme) string {
	if len(data) == 0 {
		return centerText("(no data)", w, h, th)
	}

	// Build column list from ALL rows (union of keys).
	seen := map[string]bool{}
	var cols []string
	for _, row := range data {
		for k := range row {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	sort.Strings(cols)

	// Compute column widths: max of header width and formatted value widths, capped.
	const maxColW = 28
	const minColW = 4
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = ansi.StringWidth(c)
	}
	for _, row := range data {
		for i, c := range cols {
			v := formatCellValue(c, row[c])
			if vw := ansi.StringWidth(v); vw > widths[i] {
				widths[i] = vw
			}
		}
	}
	for i := range widths {
		if widths[i] > maxColW {
			widths[i] = maxColW
		}
		if widths[i] < minColW {
			widths[i] = minColW
		}
	}

	// Truncate columns to fit terminal width.
	totalW := 1
	for _, cw := range widths {
		totalW += cw + 3
	}
	for totalW > w && len(widths) > 1 {
		widths = widths[:len(widths)-1]
		cols = cols[:len(cols)-1]
		totalW = 1
		for _, cw := range widths {
			totalW += cw + 3
		}
	}

	cellStr := func(s string, cw int) string {
		if ansi.StringWidth(s) > cw {
			s = ansi.Truncate(s, cw-1, "…")
		}
		pad := cw - ansi.StringWidth(s)
		if pad < 0 {
			pad = 0
		}
		return s + strings.Repeat(" ", pad)
	}

	sep := func(left, mid, right, fill string) string {
		var b strings.Builder
		b.WriteString(left)
		for i, cw := range widths {
			b.WriteString(strings.Repeat(fill, cw+2))
			if i < len(widths)-1 {
				b.WriteString(mid)
			}
		}
		b.WriteString(right)
		return b.String()
	}

	// 3 lines for borders (top, header, sep), 1 for bottom, 1 for scroll hint
	maxRows := h - 5
	if maxRows < 1 {
		maxRows = 1
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(data) {
		offset = len(data) - 1
	}

	var lines []string
	lines = append(lines, sep("┌", "┬", "┐", "─"))

	// Header.
	var hb strings.Builder
	hb.WriteString("│")
	for i, c := range cols {
		hb.WriteString(" ")
		hb.WriteString(th.Header.Render(cellStr(c, widths[i])))
		hb.WriteString(" │")
	}
	lines = append(lines, hb.String())
	lines = append(lines, sep("├", "┼", "┤", "─"))

	// Data rows with offset.
	end := offset + maxRows
	if end > len(data) {
		end = len(data)
	}
	for ri := offset; ri < end; ri++ {
		row := data[ri]
		style := th.RowEven
		if ri%2 != 0 {
			style = th.RowOdd
		}
		var rb strings.Builder
		rb.WriteString("│")
		for i, c := range cols {
			rb.WriteString(" ")
			rb.WriteString(style.Render(cellStr(formatCellValue(c, row[c]), widths[i])))
			rb.WriteString(" │")
		}
		lines = append(lines, rb.String())
	}
	lines = append(lines, sep("└", "┴", "┘", "─"))

	// Scroll hint.
	hint := fmt.Sprintf(" row %d-%d / %d  (↑/↓ scroll)", offset+1, end, len(data))
	lines = append(lines, th.Subtle.Render(hint))

	return strings.Join(lines, "\n")
}

// renderBarChart renders data as a vertical bar chart.
func renderBarChart(data []map[string]any, xCol, yCol string, w, h int, th *theme.Theme) string {
	if len(data) == 0 {
		return centerText("(no data)", w, h, th)
	}

	if xCol == "" || yCol == "" {
		xCol, yCol = detectXY(data)
	}
	if yCol == "" {
		return renderTableChart(data, 0, w, h, th)
	}

	type bar struct {
		label string
		value float64
	}

	bars := make([]bar, 0, len(data))
	maxVal := 0.0
	numericCount := 0
	for _, row := range data {
		var v float64
		switch val := row[yCol].(type) {
		case float64:
			v = val
			numericCount++
		case int64:
			v = float64(val)
			numericCount++
		case string:
			if parsed, err := strconv.ParseFloat(val, 64); err == nil {
				v = parsed
				numericCount++
			}
		}
		// Format x label: if it looks like a timestamp string (contains "T"), take HH:MM
		rawLabel := fmt.Sprint(row[xCol])
		label := rawLabel
		if len(label) > 5 && strings.Contains(label, "T") {
			// ISO time like "2026-07-22T04:50:00" → "04:50"
			parts := strings.Split(label, "T")
			if len(parts) == 2 && len(parts[1]) >= 5 {
				label = parts[1][:5]
			}
		} else if len(label) > 5 {
			label = label[len(label)-5:]
		}
		bars = append(bars, bar{label: label, value: v})
		if v > maxVal {
			maxVal = v
		}
	}

	// If no numeric values, fall back to table.
	if numericCount == 0 {
		return renderTableChart(data, 0, w, h, th)
	}
	if maxVal == 0 {
		maxVal = 1
	}

	const barW = 5
	const gap = 1
	chartH := h - 3
	if chartH < 2 {
		chartH = 2
	}
	maxBars := w / (barW + gap)
	if maxBars < 1 {
		maxBars = 1
	}
	totalBars := len(bars)
	if len(bars) > maxBars {
		bars = bars[len(bars)-maxBars:]
	}

	type cell struct {
		filled bool
		color  color.Color
	}
	grid := make([][]cell, chartH)
	for r := range grid {
		grid[r] = make([]cell, len(bars))
	}
	for bi, b := range bars {
		heightCells := int(math.Round(b.value / maxVal * float64(chartH)))
		if heightCells < 0 {
			heightCells = 0
		}
		// Non-zero values must produce at least 1 visible row.
		if b.value > 0 && heightCells < 1 {
			heightCells = 1
		}
		if heightCells > chartH {
			heightCells = chartH
		}
		col := th.SeriesColor(bi % 8)
		for r := chartH - heightCells; r < chartH; r++ {
			grid[r][bi] = cell{filled: true, color: col}
		}
	}

	var lines []string

	// Value labels.
	{
		var vb strings.Builder
		for bi, b := range bars {
			valStr := formatValue(b.value)
			for len(valStr) < barW {
				valStr = " " + valStr
			}
			if len(valStr) > barW {
				valStr = valStr[len(valStr)-barW:]
			}
			vb.WriteString(th.Subtle.Render(valStr))
			if bi < len(bars)-1 {
				vb.WriteString(strings.Repeat(" ", gap))
			}
		}
		lines = append(lines, vb.String())
	}

	// Bar rows.
	for r := 0; r < chartH; r++ {
		var lb strings.Builder
		for bi := range bars {
			c := grid[r][bi]
			seg := strings.Repeat("█", barW)
			if c.filled {
				lb.WriteString(lipgloss.NewStyle().Foreground(c.color).Render(seg))
			} else {
				lb.WriteString(th.Subtle.Render(strings.Repeat(" ", barW)))
			}
			if bi < len(bars)-1 {
				lb.WriteString(strings.Repeat(" ", gap))
			}
		}
		lines = append(lines, lb.String())
	}

	// X labels.
	{
		var xb strings.Builder
		for bi, b := range bars {
			label := b.label
			for len(label) < barW {
				label += " "
			}
			if len(label) > barW {
				label = label[:barW]
			}
			xb.WriteString(th.Subtle.Render(label))
			if bi < len(bars)-1 {
				xb.WriteString(strings.Repeat(" ", gap))
			}
		}
		lines = append(lines, xb.String())
	}

	if totalBars > maxBars {
		hint := fmt.Sprintf(" showing last %d of %d", maxBars, totalBars)
		lines = append(lines, th.Subtle.Render(hint))
	}

	return strings.Join(lines, "\n")
}

// renderPieChart renders data as a polar-coordinate pie chart.
// labelCol provides slice labels, valueCol provides numeric values.
func renderPieChart(data []map[string]any, labelCol, valueCol string, w, h int, th *theme.Theme) string {
	if len(data) == 0 {
		return centerText("(no data)", w, h, th)
	}

	type slice struct {
		label string
		value float64
	}
	slices := make([]slice, 0, len(data))
	total := 0.0
	for _, row := range data {
		var v float64
		switch val := row[valueCol].(type) {
		case float64:
			v = val
		case int64:
			v = float64(val)
		case string:
			v, _ = strconv.ParseFloat(val, 64)
		}
		label := fmt.Sprint(row[labelCol])
		if v > 0 {
			slices = append(slices, slice{label: label, value: v})
			total += v
		}
	}
	if total == 0 || len(slices) == 0 {
		return renderTableChart(data, 0, w, h, th)
	}

	// Reserve bottom lines for legend.
	legendLines := (len(slices) + 3) / 4 // 4 items per legend row
	if legendLines < 1 {
		legendLines = 1
	}
	chartH := h - legendLines - 1
	if chartH < 4 {
		chartH = 4
	}
	chartW := w

	// Compute cumulative angles [0, 2π).
	angles := make([]float64, len(slices)+1)
	angles[0] = 0
	for i, s := range slices {
		angles[i+1] = angles[i] + (s.value/total)*2*math.Pi
	}

	cx := float64(chartW) / 2
	cy := float64(chartH) / 2
	// radius: account for 2:1 terminal aspect ratio (chars are ~2x taller than wide)
	radius := math.Min(float64(chartW)/4, float64(chartH)/2) * 0.9

	// Build the chart grid.
	rows := make([]string, chartH)
	for row := 0; row < chartH; row++ {
		var sb strings.Builder
		for col := 0; col < chartW; col++ {
			// Adjust x for aspect ratio (terminal cells ~2x taller than wide).
			dx := (float64(col) - cx) * 0.5
			dy := float64(row) - cy
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > radius {
				sb.WriteString(" ")
				continue
			}
			// Determine angle [0, 2π).
			angle := math.Atan2(dy, dx) + math.Pi // shift to [0, 2π)
			// Find which slice this angle belongs to.
			sliceIdx := len(slices) - 1
			for i := 0; i < len(slices); i++ {
				if angle < angles[i+1] {
					sliceIdx = i
					break
				}
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(th.SeriesColor(sliceIdx % 8)).Render("█"))
		}
		rows[row] = sb.String()
	}

	// Build legend.
	var legendParts []string
	for i, s := range slices {
		pct := s.value / total * 100
		lbl := s.label
		if len(lbl) > 12 {
			lbl = lbl[:12]
		}
		dot := lipgloss.NewStyle().Foreground(th.SeriesColor(i % 8)).Render("●")
		legendParts = append(legendParts, fmt.Sprintf("%s %s %.0f%%", dot, lbl, pct))
	}

	var result strings.Builder
	for _, row := range rows {
		result.WriteString(row)
		result.WriteString("\n")
	}
	// Write legend rows (4 items per row).
	for i := 0; i < len(legendParts); i += 4 {
		end := i + 4
		if end > len(legendParts) {
			end = len(legendParts)
		}
		result.WriteString(th.Subtle.Render(strings.Join(legendParts[i:end], "  ")))
		result.WriteString("\n")
	}

	return strings.TrimRight(result.String(), "\n")
}

// detectXY auto-detects x (label) and y (numeric) columns.
func detectXY(data []map[string]any) (xCol, yCol string) {
	if len(data) == 0 {
		return "", ""
	}
	// Check all rows to find a column with numeric values.
	colNumeric := map[string]int{}
	colTotal := map[string]int{}
	for _, row := range data {
		for k, v := range row {
			colTotal[k]++
			switch v.(type) {
			case float64, int64:
				colNumeric[k]++
			case string:
				if s, ok := v.(string); ok {
					if _, err := strconv.ParseFloat(s, 64); err == nil {
						colNumeric[k]++
					}
				}
			}
		}
	}
	// Pick yCol as the column with most numeric values (excluding _timestamp).
	bestNumericScore := 0
	for k, n := range colNumeric {
		if k == "_timestamp" {
			continue
		}
		if n > bestNumericScore {
			bestNumericScore = n
			yCol = k
		}
	}
	// Pick xCol as the non-numeric, non-timestamp column.
	for k := range colTotal {
		if k != yCol && k != "_timestamp" {
			xCol = k
			break
		}
	}
	if xCol == "" && yCol != "" {
		// Use _timestamp or first col as x.
		for k := range colTotal {
			if k != yCol {
				xCol = k
				break
			}
		}
	}
	return xCol, yCol
}

// formatValue formats a float64 value concisely.
func formatValue(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.1fK", v/1_000)
	}
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// centerText centers a message in the available area.
func centerText(msg string, w, h int, th *theme.Theme) string {
	lines := make([]string, h)
	mid := h / 2
	pad := (w - ansi.StringWidth(msg)) / 2
	if pad < 0 {
		pad = 0
	}
	for i := range lines {
		if i == mid {
			lines[i] = th.Subtle.Render(strings.Repeat(" ", pad) + msg)
		} else {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// centerBlock centers a multi-line block horizontally within width w.
// Each line is padded on the left so the widest line is centered.
func centerBlock(content string, w int) string {
	lines := strings.Split(content, "\n")
	maxW := 0
	for _, l := range lines {
		if lw := ansi.StringWidth(l); lw > maxW {
			maxW = lw
		}
	}
	pad := (w - maxW) / 2
	if pad <= 0 {
		return content
	}
	prefix := strings.Repeat(" ", pad)
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
