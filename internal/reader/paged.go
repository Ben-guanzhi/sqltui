package reader

import (
	"errors"
	"math"

	"github.com/LinPr/sqltui/internal/data"
)

// Paged is an optional capability a Reader implements for formats whose rows
// can be produced incrementally (csv/tsv/dsv today). Callers that would
// otherwise materialize a source too large for memory open a Pager and load
// bounded row windows instead; the UI keeps working on the materialized
// window frame.
//
// The contract is intentionally minimal so a future lazy-loading UI can grow
// on top of it without changing callers: a Pager serves row windows on
// demand; how it scans internally is its own business.
type Paged interface {
	// OpenPaged parses the source header and returns a Pager over its data
	// rows. The caller owns the pager and must Close it. For stdin/URL
	// sources the spool file must outlive the pager (Cleanup after Close).
	OpenPaged(src *Source, opt Options) (*Pager, error)
}

// Pager is an open, lazily-scanned single table. Data-row indices match a
// full Read of the same source with the same options (IgnoreErrors and
// TruncateRagged row handling included). It is not safe for concurrent use.
//
// Scanning cost: every Window/NumRows call re-opens the source and reads
// sequentially, Θ(lo+hi) records per window (NumRows scans once, cached).
// That is the right shape for bounded previews and infrequent window loads,
// not for per-keystroke paging of huge files; a sequential-cursor
// optimization can later land inside the Pager without changing this
// contract.
type Pager struct {
	name   string
	header []string
	opt    Options
	impl   pagerImpl
	rows   int // total data rows; -1 until NumRows counts them
	closed bool
}

// pagerImpl is the per-format scan engine behind a Pager.
type pagerImpl interface {
	// scan returns the header and the kept data rows in [lo, hi).
	scan(lo, hi int) ([]string, [][]any, error)
	// count returns the total number of kept data rows without
	// materializing them.
	count() (int, error)
}

// ErrPagerClosed is returned by Pager methods after Close.
var ErrPagerClosed = errors.New("reader: pager is closed")

// Name returns the display name for tab titles (the source's name).
func (p *Pager) Name() string { return p.name }

// Columns returns a copy of the resolved header names.
func (p *Pager) Columns() []string {
	return append([]string(nil), p.header...)
}

// NumRows returns the total data-row count. The first call is a full
// sequential scan (no row materialization); the result is cached.
func (p *Pager) NumRows() (int, error) {
	if p.closed {
		return 0, ErrPagerClosed
	}
	if p.rows >= 0 {
		return p.rows, nil
	}
	n, err := p.impl.count()
	if err != nil {
		return 0, err
	}
	p.rows = n
	return n, nil
}

// Window materializes data rows [lo, hi) as a frame. hi is clamped to the
// rows present; lo at or past the end yields an empty frame with the
// header. Schema inference runs over this window's own cells, so types
// reflect the previewed rows (documented consequence of partial loading).
func (p *Pager) Window(lo, hi int) (*data.Frame, error) {
	if p.closed {
		return nil, ErrPagerClosed
	}
	if lo < 0 {
		lo = 0
	}
	if hi < lo {
		hi = lo
	}
	header, rows, err := p.impl.scan(lo, hi)
	if err != nil {
		return nil, err
	}
	frame := buildFrame(header, rows)
	return applyInfer(frame, p.opt), nil
}

// Close releases the pager; subsequent calls return ErrPagerClosed. The
// underlying file is opened per operation and closed before it returns, so
// no handle outlives a method call — Close only seals the pager.
func (p *Pager) Close() error {
	p.closed = true
	return nil
}

// maxWindowRows bounds a single scan; callers pass smaller hi values.
const maxWindowRows = math.MaxInt
