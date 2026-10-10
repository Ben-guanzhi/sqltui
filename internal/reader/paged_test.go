package reader

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
)

// writeTempSource writes content to a temp file and returns a Source.
func writeTempSource(t *testing.T, name, content string) *Source {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// gridCSV builds a csv with header and n data rows: id,v with v = "row<i>".
func gridCSV(n int) string {
	var b strings.Builder
	b.WriteString("id,v\n")
	for i := 0; i < n; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteString(",row")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\n")
	}
	return b.String()
}

// framesEqual compares two frames cell by cell.
func framesEqual(t *testing.T, what string, got, want *data.Frame) {
	t.Helper()
	if got.NumRows() != want.NumRows() || got.NumCols() != want.NumCols() {
		t.Fatalf("%s: shape %dx%d, want %dx%d", what,
			got.NumRows(), got.NumCols(), want.NumRows(), want.NumCols())
	}
	for c := 0; c < got.NumCols(); c++ {
		if got.Columns[c].Name != want.Columns[c].Name || got.Columns[c].Type != want.Columns[c].Type {
			t.Fatalf("%s: col %d = %q/%v, want %q/%v", what, c,
				got.Columns[c].Name, got.Columns[c].Type,
				want.Columns[c].Name, want.Columns[c].Type)
		}
	}
	for r := 0; r < got.NumRows(); r++ {
		for c := 0; c < got.NumCols(); c++ {
			if !reflect.DeepEqual(got.Cell(r, c), want.Cell(r, c)) {
				t.Fatalf("%s: cell (%d,%d) = %#v, want %#v", what, r, c,
					got.Cell(r, c), want.Cell(r, c))
			}
		}
	}
}

// fullRead runs a full Read for the source/options and returns its frame.
func fullRead(t *testing.T, src *Source, opt Options) *data.Frame {
	t.Helper()
	r, err := For(opt.Format)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := r.Read(src, opt)
	if err != nil {
		t.Fatalf("full read: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("full read: %d frames, want 1", len(frames))
	}
	return frames[0].Frame
}

// openPager opens a pager for the source and registers cleanup.
func openPager(t *testing.T, src *Source, opt Options) *Pager {
	t.Helper()
	r, err := For(opt.Format)
	if err != nil {
		t.Fatal(err)
	}
	pg, ok := r.(Paged)
	if !ok {
		t.Fatalf("format %q does not implement Paged", opt.Format)
	}
	pager, err := pg.OpenPaged(src, opt)
	if err != nil {
		t.Fatalf("OpenPaged: %v", err)
	}
	t.Cleanup(func() { pager.Close() })
	return pager
}

func TestPagedWindowMatchesFullRead(t *testing.T) {
	src := writeTempSource(t, "grid.csv", gridCSV(300))
	opt := noInferOpts(FormatCSV)
	full := fullRead(t, src, opt)
	if full.NumRows() != 300 {
		t.Fatalf("full rows = %d, want 300", full.NumRows())
	}

	pager := openPager(t, src, opt)

	n, err := pager.NumRows()
	if err != nil || n != 300 {
		t.Fatalf("NumRows = %d, %v; want 300", n, err)
	}
	if n2, err := pager.NumRows(); err != nil || n2 != n {
		t.Fatalf("cached NumRows = %d, %v; want %d", n2, err, n)
	}

	// hi past the end is clamped: window == the full frame.
	f, err := pager.Window(0, 1000)
	if err != nil {
		t.Fatalf("Window(0,1000): %v", err)
	}
	framesEqual(t, "full window", f, full)

	// A middle window is the row slice of the full frame.
	f2, err := pager.Window(10, 25)
	if err != nil {
		t.Fatalf("Window(10,25): %v", err)
	}
	framesEqual(t, "middle window", f2, full.Select(rangeRows(10, 25)))

	// Tail window.
	f3, err := pager.Window(250, 10000)
	if err != nil {
		t.Fatalf("Window(250,10000): %v", err)
	}
	framesEqual(t, "tail window", f3, full.Select(rangeRows(250, 300)))

	// lo past the end: empty frame, header intact.
	f4, err := pager.Window(400, 500)
	if err != nil {
		t.Fatalf("Window(400,500): %v", err)
	}
	if f4.NumRows() != 0 || f4.NumCols() != 2 || f4.Columns[0].Name != "id" {
		t.Fatalf("past-end window = %d rows, cols %v", f4.NumRows(), f4.ColumnNames())
	}

	// hi < lo: empty.
	f5, err := pager.Window(20, 10)
	if err != nil {
		t.Fatalf("Window(20,10): %v", err)
	}
	if f5.NumRows() != 0 {
		t.Fatalf("inverted window has %d rows", f5.NumRows())
	}

	if got := pager.Columns(); !reflect.DeepEqual(got, []string{"id", "v"}) {
		t.Errorf("Columns = %v", got)
	}
	if pager.Name() != "grid" {
		t.Errorf("Name = %q, want grid", pager.Name())
	}
}

func TestPagedNoHeader(t *testing.T) {
	src := writeTempSource(t, "nh.csv", "1,alice\n2,bob\n3,cyd\n")
	opt := noInferOpts(FormatCSV)
	opt.NoHeader = true
	full := fullRead(t, src, opt)

	pager := openPager(t, src, opt)
	if n, err := pager.NumRows(); err != nil || n != 3 {
		t.Fatalf("NumRows = %d, %v; want 3", n, err)
	}
	f, err := pager.Window(1, 3)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	framesEqual(t, "no-header window", f, full.Select(rangeRows(1, 3)))
	if f.Columns[0].Name != "column_1" {
		t.Errorf("synthesized header = %q", f.Columns[0].Name)
	}
}

func TestPagedCustomSeparatorAndQuote(t *testing.T) {
	src := fixture(t, "custquote.dsv")
	opt := noInferOpts(FormatDSV)
	opt.Separator = ';'
	opt.Quote = '\''
	full := fullRead(t, src, opt)

	pager := openPager(t, src, opt)
	if n, err := pager.NumRows(); err != nil || n != full.NumRows() {
		t.Fatalf("NumRows = %d, %v; want %d", n, err, full.NumRows())
	}
	f, err := pager.Window(0, 100)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	framesEqual(t, "dsv window", f, full)
}

func TestPagedRowNumberingMatchesFullRead(t *testing.T) {
	// ragged.csv: row 2 has 4 fields against a 3-field header. The pager's
	// row indices must line up with whichever full-read policy is active.
	for _, tc := range []struct {
		name  string
		apply func(*Options)
	}{
		{"ignore-errors", func(o *Options) { o.IgnoreErrors = true }},
		{"truncate-ragged", func(o *Options) { o.TruncateRagged = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fixture(t, "ragged.csv")
			opt := noInferOpts(FormatCSV)
			tc.apply(&opt)
			full := fullRead(t, src, opt)
			if full.NumRows() == 0 {
				t.Fatal("policy dropped every row; fixture assumption broken")
			}
			pager := openPager(t, src, opt)
			if n, err := pager.NumRows(); err != nil || n != full.NumRows() {
				t.Fatalf("NumRows = %d, %v; want %d", n, err, full.NumRows())
			}
			f, err := pager.Window(0, 100)
			if err != nil {
				t.Fatalf("Window: %v", err)
			}
			framesEqual(t, "ragged policy window", f, full)
		})
	}
}

func TestPagedHeaderOnly(t *testing.T) {
	src := writeTempSource(t, "empty.csv", "id,v\n")
	opt := noInferOpts(FormatCSV)
	pager := openPager(t, src, opt)
	if n, err := pager.NumRows(); err != nil || n != 0 {
		t.Fatalf("NumRows = %d, %v; want 0", n, err)
	}
	f, err := pager.Window(0, 10)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if f.NumRows() != 0 || f.NumCols() != 2 {
		t.Fatalf("header-only window = %d rows × %d cols", f.NumRows(), f.NumCols())
	}
}

func TestPagedClosed(t *testing.T) {
	src := writeTempSource(t, "c.csv", gridCSV(5))
	opt := noInferOpts(FormatCSV)
	pager := openPager(t, src, opt)
	if err := pager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := pager.Window(0, 1); err != ErrPagerClosed {
		t.Errorf("Window after Close = %v, want ErrPagerClosed", err)
	}
	if _, err := pager.NumRows(); err != ErrPagerClosed {
		t.Errorf("NumRows after Close = %v, want ErrPagerClosed", err)
	}
}

func TestPagedTSV(t *testing.T) {
	src := writeTempSource(t, "g.tsv", "id\tv\n0\trow0\n1\trow1\n2\trow2\n")
	opt := noInferOpts(FormatTSV)
	full := fullRead(t, src, opt)
	pager := openPager(t, src, opt)
	f, err := pager.Window(1, 3)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	framesEqual(t, "tsv window", f, full.Select(rangeRows(1, 3)))
}

// rangeRows is [lo, hi).
func rangeRows(lo, hi int) []int {
	out := make([]int, 0, hi-lo)
	for i := lo; i < hi; i++ {
		out = append(out, i)
	}
	return out
}
