package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/theme"
)

// SearchBar implements live search over the current frame. While active it
// keeps a preview frame (the filtered rows) that the app displays instead of
// the pane's top frame; the stack is only touched on commit.
type SearchBar struct {
	active bool
	fuzzy  bool
	query  string

	base       *data.Frame // frame being searched (top of stack at activation)
	preview    *data.Frame // live filtered result
	searchCols []int       // when non-nil, search only these column indices
}

func (s *SearchBar) Active() bool  { return s.active }
func (s *SearchBar) Fuzzy() bool   { return s.fuzzy }
func (s *SearchBar) Query() string { return s.query }

// Start activates the bar over frame f.
func (s *SearchBar) Start(f *data.Frame, fuzzy bool) {
	s.StartCols(f, fuzzy, nil)
}

// StartCols activates the bar over frame f, restricting the search to the
// given column indices (nil = search all columns).
func (s *SearchBar) StartCols(f *data.Frame, fuzzy bool, cols []int) {
	s.active = true
	s.fuzzy = fuzzy
	s.query = ""
	s.base = f
	s.preview = f
	s.searchCols = cols
}

// Type appends printable input and recomputes the preview.
func (s *SearchBar) Type(text string) {
	if !s.active || text == "" {
		return
	}
	s.query += text
	s.recompute()
}

// Paste appends pasted text as a single edit (bracketed paste delivers the
// whole chunk in one message, not per-rune key presses), flattened to one
// line first.
func (s *SearchBar) Paste(text string) {
	s.Type(flattenPaste(text))
}

// flattenPaste makes pasted text safe for the single-line search input:
// newlines and tabs become single spaces, carriage returns and other control
// characters are dropped.
func flattenPaste(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(' ')
		case r < ' ' || r == 0x7f:
			// drop \r and other control characters
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Backspace removes the last rune and recomputes the preview.
func (s *SearchBar) Backspace() {
	if !s.active || s.query == "" {
		return
	}
	r := []rune(s.query)
	s.query = string(r[:len(r)-1])
	s.recompute()
}

func (s *SearchBar) recompute() {
	if s.base == nil {
		return
	}
	if s.query == "" {
		s.preview = s.base
		return
	}
	var rows []int
	if len(s.searchCols) > 0 {
		rows = data.SearchExactCols(s.base, s.searchCols, s.query)
	} else if s.fuzzy {
		rows = data.SearchFuzzy(s.base, s.query)
	} else {
		rows = data.SearchExact(s.base, s.query)
	}
	s.preview = s.base.Select(rows)
}

// Preview returns the frame to display while the bar is active (never nil
// while active with a base frame).
func (s *SearchBar) Preview() *data.Frame {
	if s.preview != nil {
		return s.preview
	}
	return s.base
}

// Commit deactivates the bar and returns the filtered frame plus a crumb
// label. ok is false when there is nothing to commit (empty query).
func (s *SearchBar) Commit() (f *data.Frame, crumb string, ok bool) {
	f, q, fuzzy := s.preview, s.query, s.fuzzy
	s.Cancel()
	if q == "" || f == nil {
		return nil, "", false
	}
	if fuzzy {
		return f, "fuzzy", true
	}
	return f, "search", true
}

// Cancel deactivates the bar and drops the preview.
func (s *SearchBar) Cancel() {
	s.active = false
	s.query = ""
	s.base = nil
	s.preview = nil
}

// View renders the input line shown at the bottom of the screen.
func (s *SearchBar) View(width int, th *theme.Theme) string {
	rowInfo := ""
	if s.base != nil {
		n := 0
		if s.preview != nil {
			n = s.preview.NumRows()
		}
		rowInfo = th.Subtle.Render(strconv.Itoa(n) + " rows ")
	}
	filterPart := FilterLine(s.query, s.active, width-ansi.StringWidth(rowInfo), "type to search", th)
	// Overlay the row count on the right edge of the filter line.
	if rowInfo != "" && ansi.StringWidth(filterPart)+ansi.StringWidth(rowInfo) <= width {
		// Trim trailing spaces from filterPart, then append rowInfo right-aligned.
		trimmed := strings.TrimRight(filterPart, " ")
		pad := width - ansi.StringWidth(trimmed) - ansi.StringWidth(rowInfo)
		if pad < 1 {
			pad = 1
		}
		filterPart = trimmed + strings.Repeat(" ", pad) + rowInfo
	}
	return filterPart
}
