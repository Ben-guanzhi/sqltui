package oobe

import (
	"testing"

	"github.com/LinPr/sqltui/internal/data"
)

// Without a schema the columns come out of a map, and Go iterates maps in
// deliberately random order. Sorting them is what keeps the column order the
// same across two refreshes of the same data.
func TestBuildFrameWithoutSchemaSortsColumns(t *testing.T) {
	hits := []map[string]any{
		{"zebra": "z", "alpha": "a", "m": "m"},
		{"zebra": "Z", "alpha": "A", "m": "M"},
	}
	f := buildFrame(nil, hits)

	want := []string{"alpha", "m", "zebra"}
	if f.NumCols() != len(want) {
		t.Fatalf("want %d columns, got %d", len(want), f.NumCols())
	}
	for i, name := range want {
		if f.Columns[i].Name != name {
			t.Errorf("column %d: got %q, want %q", i, f.Columns[i].Name, name)
		}
	}
	// Values follow the sorted columns, not the map's order.
	if f.Cell(0, 0) != "a" || f.Cell(1, 2) != "Z" {
		t.Errorf("values out of column order: %+v", f.Columns)
	}
}

// A numeric column holding text that does not parse shows a missing value.
// The old unchecked Sscanf showed 0, and 0 is also a real number a row can
// legitimately hold - the two must not look the same.
func TestCellValueUnparseableNumberIsMissing(t *testing.T) {
	if got := cellValue("n/a", data.TypeInt); got != nil {
		t.Errorf("unparseable int: got %#v, want nil", got)
	}
	if got := cellValue("", data.TypeInt); got != nil {
		t.Errorf("empty int: got %#v, want nil", got)
	}
	if got := cellValue("1.2.3", data.TypeFloat); got != nil {
		t.Errorf("unparseable float: got %#v, want nil", got)
	}
	if got := cellValue("42", data.TypeInt); got != int64(42) {
		t.Errorf("parsable int: got %#v, want int64(42)", got)
	}
	if got := cellValue("0", data.TypeInt); got != int64(0) {
		t.Errorf("a real zero: got %#v, want int64(0)", got)
	}
	if got := cellValue("1.5", data.TypeFloat); got != 1.5 {
		t.Errorf("parsable float: got %#v, want 1.5", got)
	}
}

func TestTableTarget(t *testing.T) {
	b := &Backend{}
	if got := b.TableTarget("default/projects/demo", "streams"); got != `"streams"` {
		t.Errorf("target = %s, want \"streams\" (namespace lives in the URL, not the SQL)", got)
	}
}
