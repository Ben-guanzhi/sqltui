package writer_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/reader"
	"github.com/LinPr/sqltui/internal/writer"
)

// excelFrame is a small frame without the all-nil row sampleFrame carries: an
// xlsx row of empty cells is an empty row, and the reader (like Excel itself)
// does not distinguish it from a missing row — the roundtrip of a null row
// through xlsx is inherently lossy, so the test exercises what xlsx can hold.
func excelFrame() *data.Frame {
	return &data.Frame{Columns: []data.Column{
		data.NewColumn("s", data.TypeString, []any{"hello", `pipe|and,comma "q"`}),
		data.NewColumn("i", data.TypeInt, []any{int64(1), int64(-42)}),
		data.NewColumn("f", data.TypeFloat, []any{float64(1.5), float64(-0.25)}),
		data.NewColumn("b", data.TypeBool, []any{true, false}),
	}}
}

// The xlsx writer must produce a workbook the reader parses back: the export
// is only useful if it roundtrips. Numbers stay numbers (native cells), not
// text — a text "30" in Excel would break downstream formulas.
func TestExcelRoundtrip(t *testing.T) {
	w, err := writer.For(writer.FormatExcel)
	if err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	if err := w.Write(buf, excelFrame(), writer.DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "out.xlsx")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := reader.FromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := reader.For(reader.FormatExcel)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := r.Read(src, reader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}
	got := frames[0].Frame
	if got.NumCols() != 4 || got.NumRows() != 2 {
		t.Fatalf("shape = %dx%d, want 4x2", got.NumCols(), got.NumRows())
	}
	if got.Columns[0].Name != "s" || got.Columns[1].Name != "i" {
		t.Errorf("header = %q, %q; want s, i", got.Columns[0].Name, got.Columns[1].Name)
	}
	// The int column reads back as a number, the string column keeps its
	// separator characters.
	if got.CellString(0, 1) != "1" {
		t.Errorf("int cell = %q, want 1", got.CellString(0, 1))
	}
	if got.CellString(0, 0) != "hello" {
		t.Errorf("string cell = %q, want hello", got.CellString(0, 0))
	}
}

// Without a header the workbook starts directly with the data rows; the
// reader still treats the first row as the header (xlsx has no header flag),
// so what comes back is one header row plus the remaining data rows.
func TestExcelNoHeader(t *testing.T) {
	w, err := writer.For(writer.FormatExcel)
	if err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	opt := writer.DefaultOptions()
	opt.Header = false
	if err := w.Write(buf, excelFrame(), opt); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nohdr.xlsx")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := reader.FromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := reader.For(reader.FormatExcel)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := r.Read(src, reader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := frames[0].Frame
	if got.NumRows() != 1 {
		t.Fatalf("rows = %d, want 1 (the first of two data rows is consumed as the header)", got.NumRows())
	}
	if got.Columns[0].Name != "hello" {
		t.Errorf("first row = %q, want hello read back as the header", got.Columns[0].Name)
	}
}
