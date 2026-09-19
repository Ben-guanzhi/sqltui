package popup

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/theme"
)

// TestBarChartWarnVisible reproduces the bug where a very small value (WARN=6)
// rounds to heightCells=0 and produces an invisible bar.
func TestBarChartWarnVisible(t *testing.T) {
	data := []map[string]any{
		{"count": float64(3997), "severity": "DEBUG"},
		{"count": float64(181), "severity": "ERROR"},
		{"count": float64(184), "severity": "INFO"},
		{"count": float64(6), "severity": "WARN"},
	}
	th := theme.Default()
	out := renderBarChart(data, "severity", "count", 80, 20, th)
	t.Logf("rendered:\n%s", out)

	if !strings.Contains(ansi.Strip(out), "WARN") {
		t.Fatal("WARN label missing from bar chart output")
	}

	// Find the column offset of the WARN label in the x-axis row.
	lines := strings.Split(out, "\n")
	warnColStart := -1
	for _, line := range lines {
		plain := ansi.Strip(line)
		if idx := strings.Index(plain, "WARN"); idx >= 0 {
			warnColStart = idx
			break
		}
	}
	if warnColStart < 0 {
		t.Fatal("WARN label not found in any output line")
	}

	// Check that at least one bar row has a block char in the WARN column.
	blockFound := false
	for _, line := range lines {
		plain := ansi.Strip(line)
		runes := []rune(plain)
		if warnColStart >= len(runes) {
			continue
		}
		seg := runes[warnColStart:]
		if len(seg) > 5 {
			seg = seg[:5]
		}
		if strings.ContainsRune(string(seg), '█') {
			blockFound = true
			break
		}
	}
	if !blockFound {
		t.Errorf("WARN bar has zero height — no block chars found in WARN column (value=6, maxVal=3997, heightCells rounds to 0)")
	}
}
