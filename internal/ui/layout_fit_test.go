package ui

// The frame fits the terminal, at every size.
//
// A terminal wraps a line that is one column too wide, and the extra line
// pushes everything below it down: the table's last row falls off, or the
// status bar ends up on a second screen. A TUI has no scrollbar to catch it,
// so the only way to know is to render at a size and measure.
//
// The golden tests pin one size (80x24) byte for byte. This sweeps many, and
// each render is measured with the same width rule the terminal uses —
// ansi.StringWidth, which counts a CJK glyph as two columns.
//
// 波 101 续三十九.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/data"
)

// fitFrame is a frame whose cells are wide enough to make the layout work:
// a CJK name (two columns), a long value and a number.
func fitFrame() *data.Frame {
	return &data.Frame{Columns: []data.Column{
		data.NewColumn("id", data.TypeInt, []any{int64(101), int64(102), nil}),
		data.NewColumn("name", data.TypeString, []any{"alice", "客户", "a-very-long-value-here"}),
		data.NewColumn("客户备注", data.TypeString, []any{"created", "updated", "archived"}),
		data.NewColumn("score", data.TypeFloat, []any{3.14, 0.0, 123456.789}),
		data.NewColumn("ok", data.TypeBool, []any{true, false, nil}),
	}}
}

// TestFrameFitsTerminalWidth renders at a range of terminal sizes and checks
// that no line is wider than the terminal — the wrap that would push the
// bottom of the screen off.
func TestFrameFitsTerminalWidth(t *testing.T) {
	sizes := []struct{ w, h int }{
		{200, 40}, {120, 30}, {100, 30}, {80, 24}, {72, 24}, {64, 20}, {52, 16}, {40, 12},
	}
	for _, s := range sizes {
		a := goldenApp(t, fitFrame(), s.w, s.h)
		got := a.View().Content
		lines := strings.Split(got, "\n")
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > s.w {
				t.Errorf("%dx%d: line %d is %d columns wide, wider than the terminal (%q)",
					s.w, s.h, i, w, ansi.Strip(l))
				break
			}
		}
	}
}

// TestFrameHasNoEmptyRows checks the other half of the fit: the frame is
// exactly the terminal's height, so the status bar at the bottom is on the
// bottom row rather than a screen further down.
func TestFrameHasNoEmptyRows(t *testing.T) {
	for _, s := range []struct{ w, h int }{{120, 30}, {80, 24}, {64, 20}} {
		a := goldenApp(t, fitFrame(), s.w, s.h)
		got := a.View().Content
		lines := strings.Split(got, "\n")
		if len(lines) != s.h {
			t.Errorf("%dx%d: the frame is %d lines, want %d", s.w, s.h, len(lines), s.h)
			continue
		}
		// The last line carries the status bar; it must not be empty.
		if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
			t.Errorf("%dx%d: the last line is empty, so the status bar is not at the bottom", s.w, s.h)
		}
	}
}

// TestStatusBarFitsWidth checks the status bar on its own: it is the line
// with the most in it (connection, rows, columns, breadcrumb), so it is where
// an overflow shows first. The status bar drops its least important segments
// when the terminal is narrow, and what is left must still fit.
func TestStatusBarFitsWidth(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 64, 52, 40, 30, 20} {
		a := goldenApp(t, fitFrame(), w, 24)
		bar := a.renderBottom(w)
		for i, l := range strings.Split(bar, "\n") {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("status bar at width %d: line %d is %d columns wide (%q)", w, i, got, ansi.Strip(l))
				break
			}
		}
	}
}
