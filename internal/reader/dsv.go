package reader

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/LinPr/sqltui/internal/data"
)

// dsvReader parses delimiter-separated text: csv (comma), tsv (tab) and
// dsv (custom separator via Options.Separator).
type dsvReader struct{}

func init() {
	r := dsvReader{}
	Register(FormatCSV, r)
	Register(FormatTSV, r)
	Register(FormatDSV, r)
}

func (dsvReader) Read(src *Source, opt Options) ([]NamedFrame, error) {
	cfg, err := dsvSetup(opt)
	if err != nil {
		return nil, err
	}
	f, err := src.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// encoding/csv hardcodes '"' as the quote character. To support a custom
	// quote we swap the two bytes in the input stream (bijective, safe for
	// ASCII quote chars since UTF-8 continuation bytes are >= 0x80), then swap
	// them back inside each parsed field.
	var rd io.Reader = stripBOM(f)
	if cfg.swapQuote {
		rd = &byteSwapReader{r: rd, a: cfg.quoteByte, b: '"'}
	}
	cr := newDSVCsvReader(rd, cfg, opt)

	var (
		header []string
		rows   [][]any
		width  int
		first  = true
	)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if opt.IgnoreErrors {
				continue
			}
			return nil, err
		}
		if cfg.swapQuote {
			for i := range rec {
				rec[i] = swapBytes(rec[i], cfg.quoteByte, '"')
			}
		}
		if first {
			first = false
			width = len(rec)
			if opt.NoHeader {
				header = synthesizeHeader(width)
				// fall through: the record is data
			} else {
				header = append([]string(nil), rec...)
				continue
			}
		}
		row, skip, err := normalizeRow(rec, width, opt)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		rows = append(rows, row)
	}

	frame := buildFrame(header, rows)
	frame = applyInfer(frame, opt)
	return []NamedFrame{{Name: src.Name(), Frame: frame}}, nil
}

// dsvConfig is the fully-resolved dsv parse setup for one Options value,
// shared by the full reader and the paged scanner.
type dsvConfig struct {
	sep       rune
	swapQuote bool
	quoteByte byte
}

// dsvSetup resolves separator and quote handling from options.
func dsvSetup(opt Options) (dsvConfig, error) {
	cfg := dsvConfig{sep: ','}
	switch opt.Format {
	case FormatTSV:
		cfg.sep = '\t'
	case FormatDSV:
		if opt.Separator != 0 {
			cfg.sep = opt.Separator
		}
	}
	if opt.Quote != 0 && opt.Quote != '"' {
		if opt.Quote >= utf8.RuneSelf {
			return cfg, fmt.Errorf("quote character %q must be ASCII", opt.Quote)
		}
		cfg.swapQuote = true
		cfg.quoteByte = byte(opt.Quote)
	}
	return cfg, nil
}

// newDSVCsvReader builds the csv.Reader for a prepared input stream.
func newDSVCsvReader(rd io.Reader, cfg dsvConfig, opt Options) *csv.Reader {
	cr := csv.NewReader(rd)
	cr.Comma = cfg.sep
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = opt.IgnoreErrors
	cr.ReuseRecord = false
	return cr
}

// OpenPaged implements Paged for csv/tsv/dsv.
func (r dsvReader) OpenPaged(src *Source, opt Options) (*Pager, error) {
	cfg, err := dsvSetup(opt)
	if err != nil {
		return nil, err
	}
	dp := &dsvPager{src: src, opt: opt, cfg: cfg}
	header, err := dp.readHeader()
	if err != nil {
		return nil, err
	}
	return &Pager{name: src.Name(), header: header, opt: opt, impl: dp, rows: -1}, nil
}

// dsvPager is the pagerImpl for delimiter-separated text.
type dsvPager struct {
	src *Source
	opt Options
	cfg dsvConfig
}

func (d *dsvPager) readHeader() ([]string, error) {
	return d.walk(0, 0, nil)
}

func (d *dsvPager) scan(lo, hi int) ([]string, [][]any, error) {
	var rows [][]any
	header, err := d.walk(lo, hi, func(_ int, row []any) {
		rows = append(rows, row)
	})
	return header, rows, err
}

func (d *dsvPager) count() (int, error) {
	n := 0
	_, err := d.walk(0, maxWindowRows, func(int, []any) { n++ })
	return n, err
}

// walk streams kept data rows, invoking emit with each row's 0-based index
// (matching a full Read) for lo <= i < hi. emit==nil counts without keeping
// rows. The source is opened and closed within the call.
func (d *dsvPager) walk(lo, hi int, emit func(i int, row []any)) (header []string, err error) {
	f, err := d.src.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rd io.Reader = stripBOM(f)
	if d.cfg.swapQuote {
		rd = &byteSwapReader{r: rd, a: d.cfg.quoteByte, b: '"'}
	}
	cr := newDSVCsvReader(rd, d.cfg, d.opt)

	var (
		width int
		first = true
		i     int
	)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if d.opt.IgnoreErrors {
				continue
			}
			return nil, err
		}
		if d.cfg.swapQuote {
			for j := range rec {
				rec[j] = swapBytes(rec[j], d.cfg.quoteByte, '"')
			}
		}
		if first {
			first = false
			width = len(rec)
			if d.opt.NoHeader {
				header = synthesizeHeader(width)
				// fall through: the record is data
			} else {
				header = append([]string(nil), rec...)
				continue
			}
		}
		if i >= hi {
			break
		}
		row, skip, err := normalizeRow(rec, width, d.opt)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		if i >= lo && emit != nil {
			emit(i, row)
		}
		i++
	}
	return header, nil
}

// byteSwapReader swaps two bytes in the stream it wraps.
type byteSwapReader struct {
	r    io.Reader
	a, b byte
}

func (s *byteSwapReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	for i := 0; i < n; i++ {
		switch p[i] {
		case s.a:
			p[i] = s.b
		case s.b:
			p[i] = s.a
		}
	}
	return n, err
}

// swapBytes swaps every occurrence of a and b in s.
func swapBytes(s string, a, b byte) string {
	if !strings.ContainsAny(s, string([]byte{a, b})) {
		return s
	}
	bs := []byte(s)
	for i, c := range bs {
		switch c {
		case a:
			bs[i] = b
		case b:
			bs[i] = a
		}
	}
	return string(bs)
}

// --- helpers shared by the text-format readers in this package ---

// synthesizeHeader produces column_1..column_n names.
func synthesizeHeader(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("column_%d", i+1)
	}
	return names
}

// normalizeRow fits a raw record to the header width: short rows are padded
// with nulls; long rows error unless opt.TruncateRagged (truncate) or
// opt.IgnoreErrors (drop the row).
func normalizeRow(rec []string, width int, opt Options) (row []any, skip bool, err error) {
	if len(rec) > width {
		switch {
		case opt.TruncateRagged:
			rec = rec[:width]
		case opt.IgnoreErrors:
			return nil, true, nil
		default:
			return nil, false, fmt.Errorf("row has %d fields, expected %d (use --truncate-ragged-lines or --ignore-errors)", len(rec), width)
		}
	}
	row = make([]any, width)
	for i := 0; i < width; i++ {
		if i < len(rec) {
			row[i] = rec[i]
		} else {
			row[i] = nil
		}
	}
	return row, false, nil
}

// buildFrame assembles a string-typed frame from a header and rows whose
// cells are string or nil.
func buildFrame(header []string, rows [][]any) *data.Frame {
	f := data.New(header...)
	for _, r := range rows {
		f.AppendRow(r)
	}
	return f
}

// applyInfer runs schema/type inference over a freshly parsed string frame.
func applyInfer(f *data.Frame, opt Options) *data.Frame {
	return data.InferFrame(f, string(opt.InferSchema), opt.InferTypes)
}
