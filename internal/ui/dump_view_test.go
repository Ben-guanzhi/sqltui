package ui

// A one-off: print the app's TUI at a fixed size, so the terminal UI can be
// reviewed (and compared with the GUI clients) without a console session. Run:
//
//	go test ./internal/ui -run TestDumpViewForReview -v
//
// The output is the same string Bubble Tea would paint; the -v logging keeps it
// out of the pass/fail result.

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDumpViewForReview(t *testing.T) {
	a := newTestApp(t)
	// A fixed window so the dump is stable: 100x30 is a common terminal.
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	v := a.View()
	lines := strings.Split(v.Content, "\n")
	fmt.Printf("\n=== sqltui view 100x30 (%d lines) ===\n", len(lines))
	for i, l := range lines {
		fmt.Printf("%2d|%s|\n", i+1, l)
	}
	fmt.Printf("=== end ===\n")
}
