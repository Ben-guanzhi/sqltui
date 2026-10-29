// Package core adapts dbcore kernel drivers to the sqltui db.Backend contract.
//
// Every SQL engine wrapper (mysqlbe, postgresbe, sqlitebe, oraclebe) dials
// through dbcore.Open and hands the kernel driver to this adapter, so catalog
// SQL, statement routing, identifier quoting, and row browsing live in the
// kernel exactly once. The wrapper keeps only what the kernel must not know:
// the UI engine word, the connection title, and the FROM-clause spelling.
package core

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/sqltext"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// QueryTimeout bounds every statement. Unbounded statements would hang the UI
// forever on a wedged server or network (same value every sqltui backend used
// before the kernel migration).
const QueryTimeout = 30 * time.Second

// Options carries the per-engine presentation semantics injected by the thin
// engine wrappers.
type Options struct {
	// Kind is the sqltui engine word ("mysql", "postgres", "sqlite",
	// "oracle"); it feeds db.Backend.Kind and the sqltext dialect lookup.
	Kind string
	// Title is the connection label for the UI header.
	Title string
	// Target spells namespace.table as a FROM-clause target using the
	// engine's own quoting, exactly the way FetchTable names it.
	Target func(namespace, table string) string
	// CurrentNS reports the namespace new objects resolve to on this
	// connection; nil means the backend does not implement db.Namespaced.
	CurrentNS func(ctx context.Context) string
	// HasLastInsertID reports whether ExecResult.LastInsertID is meaningful
	// (mysql, sqlite); false keeps the field hidden (postgres, oracle).
	HasLastInsertID bool
}

// Backend adapts one dbcore.SQLDriver to the sqltui db.Backend contract.
type Backend struct {
	drv    dbcore.SQLDriver
	dbType string // sqltext spelling of Options.Kind
	opt    Options
}

var (
	_ db.Backend       = (*Backend)(nil)
	_ db.BoundRunner   = (*Backend)(nil)
	_ db.TableTargeter = (*Backend)(nil)
	_ db.Quoter        = (*Backend)(nil)
	_ db.Escaper       = (*Backend)(nil)
)

// New wraps a kernel SQL driver. The driver is owned by the Backend: Close
// closes it.
func New(drv dbcore.SQLDriver, opt Options) *Backend {
	return &Backend{drv: drv, dbType: sqltext.Normalize(opt.Kind), opt: opt}
}

// Driver exposes the kernel driver for engine wrappers that need a capability
// beyond the shared surface (e.g. sqlserverbe's cross-database catalog).
func (b *Backend) Driver() dbcore.SQLDriver { return b.drv }

func (b *Backend) Kind() string  { return b.opt.Kind }
func (b *Backend) Title() string { return b.opt.Title }

// Run executes a statement through the kernel, which routes read statements
// (SELECT/WITH/PRAGMA/SHOW/EXPLAIN/VALUES/TABLE/DESC and mutations carrying a
// RETURNING clause) to the query path and everything else to exec. Query
// results come back as a native-typed frame; NULL stays distinct from the
// empty string.
func (b *Backend) Run(stmt string) (db.Result, error) {
	if strings.TrimSpace(stmt) == "" {
		return db.Result{}, fmt.Errorf("empty query")
	}
	return b.RunBound(stmt, nil)
}

// adHocRowLimit caps one ad-hoc query's materialized rows. A bare
// `SELECT *` used to append every row of the table into memory
// (FrameFromResult); table fetches already cap at TableFetchLimit, the query
// path had no ceiling at all. A visible truncation marker in the TUI is UI work (the frame shape
// carries none); the cap itself is the same convention TableFetchLimit uses.
const adHocRowLimit = 10000

// RunBound is Run with bound parameters: values ride as driver arguments
// (dbcore.ValueOf), never as concatenated text — the P0-4 rule.
func (b *Backend) RunBound(stmt string, args []any) (db.Result, error) {
	if strings.TrimSpace(stmt) == "" {
		return db.Result{}, fmt.Errorf("empty query")
	}
	vals := make([]dbcore.Value, len(args))
	for i, a := range args {
		vals[i] = dbcore.ValueOf(a)
	}
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	res, err := b.drv.Execute(ctx, stmt, vals...)
	if err != nil {
		return db.Result{}, err
	}
	if len(res.Rows) > adHocRowLimit {
		res.Rows = res.Rows[:adHocRowLimit]
	}
	return b.result(res), nil
}

// result maps a kernel QueryResult onto the sqltui Result shape: a column
// list means rows came back, anything else is an exec outcome.
func (b *Backend) result(res *dbcore.QueryResult) db.Result {
	if len(res.Columns) > 0 {
		return db.Result{Frame: FrameFromResult(res)}
	}
	exec := &db.ExecResult{RowsAffected: res.AffectedRows, HasLastInsert: b.opt.HasLastInsertID}
	if b.opt.HasLastInsertID {
		exec.LastInsertID = res.LastInsertID
	}
	return db.Result{Exec: exec}
}

// Namespaces lists the server's databases (mysql) or schemas (postgres,
// oracle); engines without namespaces see a single empty one.
func (b *Backend) Namespaces() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	schemas, err := b.drv.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out, nil
}

// CurrentNamespace reports the namespace new objects resolve to.
func (b *Backend) CurrentNamespace() string {
	if b.opt.CurrentNS == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	return b.opt.CurrentNS(ctx)
}

// Tables lists the base tables of one namespace (views stay out of the tree,
// matching the engine catalogs sqltui listed before the kernel migration).
func (b *Backend) Tables(namespace string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	tables, err := b.drv.ListTables(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		if t.Type != "table" {
			continue
		}
		out = append(out, t.Name)
	}
	return out, nil
}

// TableTarget spells namespace.table the way FetchTable does.
func (b *Backend) TableTarget(namespace, table string) string {
	return b.opt.Target(namespace, table)
}

// FetchTable loads up to limit rows of namespace.table. The limit spelling
// (LIMIT / TOP / ROWNUM) comes from the kernel dialect; identifiers are
// quoted, never concatenated raw.
func (b *Backend) FetchTable(namespace, table string, limit int) (*data.Frame, error) {
	if limit <= 0 {
		limit = 1
	} else if limit > 10000 {
		limit = 10000
	}
	query := sqltext.RowLimit(b.dbType, "SELECT * FROM "+b.opt.Target(namespace, table), limit)
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	res, err := b.drv.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	return FrameFromResult(res), nil
}

// PrimaryKeys lists the primary-key column names of namespace.table in
// key-ordinal order; empty when the table has no primary key.
func (b *Backend) PrimaryKeys(namespace, table string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	cols, err := b.drv.ListColumns(ctx, namespace, table)
	if err != nil {
		return nil, err
	}
	var pk []dbcore.ColumnInfo
	for _, c := range cols {
		if c.IsPrimaryKey {
			pk = append(pk, c)
		}
	}
	sort.Slice(pk, func(i, j int) bool { return pk[i].OrdinalPosition < pk[j].OrdinalPosition })
	out := make([]string, 0, len(pk))
	for _, c := range pk {
		out = append(out, c.Name)
	}
	return out, nil
}

// ColumnsMeta returns column metadata for namespace.table in ordinal order.
func (b *Backend) ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	cols, err := b.drv.ListColumns(ctx, namespace, table)
	if err != nil {
		return nil, err
	}
	out := make([]db.ColumnMeta, 0, len(cols))
	for _, c := range cols {
		m := db.ColumnMeta{
			Name:       c.Name,
			DataType:   c.Type,
			IsNullable: "NO",
			Comment:    c.Comment,
		}
		if c.Nullable {
			m.IsNullable = "YES"
		}
		if c.Default != nil {
			m.Default = *c.Default
		}
		out = append(out, m)
	}
	return out, nil
}

// ColumnIndexTypes maps each indexed column to its highest-priority label
// ("PK" > "UNIQUE" > "INDEX"). PK membership comes from the column catalog,
// not the index list: a SQLite INTEGER PRIMARY KEY (rowid alias) is in no
// index at all, and the column metadata is authoritative for every engine.
func (b *Backend) ColumnIndexTypes(namespace, table string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	result := make(map[string]string)
	set := func(col, label string) {
		if existing, ok := result[col]; !ok || db.IndexPriority(existing) < db.IndexPriority(label) {
			result[col] = label
		}
	}
	if cols, err := b.drv.ListColumns(ctx, namespace, table); err == nil {
		for _, c := range cols {
			if c.IsPrimaryKey {
				set(c.Name, "PK")
			}
		}
	}
	indexes, err := b.drv.ListIndexes(ctx, namespace, table)
	if err != nil {
		if len(result) > 0 {
			// The column catalog already answered the PK part; degrade to it.
			return result, nil
		}
		return nil, err
	}
	for _, idx := range indexes {
		label := "INDEX"
		switch {
		case idx.Primary || idx.Kind == dbcore.IndexKindPrimary:
			continue // covered by the column catalog above
		case idx.Unique || idx.Kind == dbcore.IndexKindUnique:
			label = "UNIQUE"
		}
		for _, col := range idx.Columns {
			set(col, label)
		}
	}
	return result, nil
}

// QuoteIdent quotes an identifier with the engine's own quoting style.
func (b *Backend) QuoteIdent(name string) string {
	return QuoteIdent(b.dbType, name)
}

// EscapeLiteral wraps a string value as a SQL literal, applying the engine's
// escaping rules (e.g. MySQL doubles backslashes).
func (b *Backend) EscapeLiteral(s string) string {
	return sqltext.QuotedLiteral(b.dbType, s)
}

func (b *Backend) Close() error { return b.drv.Close() }

// QuoteIdent quotes name for dbType through the kernel dialect; a name the
// dialect rejects (empty, control characters) is returned verbatim — such a
// name cannot have come from an engine catalog, and the callers only render
// catalog-sourced identifiers.
func QuoteIdent(dbType, name string) string {
	q, err := sqltext.QuoteIdent(dbType, name)
	if err != nil {
		return name
	}
	return q
}

// ParsePort parses a connection-form port field; 0 lets ConnSpec.Addr apply
// the engine default. Blank and malformed values both fall back.
func ParsePort(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 65535 {
		return 0
	}
	return n
}

// CurrentNSQuery builds a CurrentNS option that resolves the namespace with a
// single-row single-column query (e.g. "SELECT current_schema()"); an error
// or an empty answer yields "".
func CurrentNSQuery(drv dbcore.SQLDriver, query string) func(context.Context) string {
	return func(ctx context.Context) string {
		res, err := drv.Execute(ctx, query)
		if err != nil || len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
			return ""
		}
		s, _ := dbcore.TryGetString(res.Rows[0][0])
		return s
	}
}

// FrameFromResult converts a kernel query result into a native-typed frame
// (nil / string / int64 / float64 / bool / time.Time / []byte), the shape the
// oracle and sqlserver backends always produced; data.Frame infers column
// types from it.
func FrameFromResult(res *dbcore.QueryResult) *data.Frame {
	names := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		names[i] = c.Name
	}
	f := data.New(names...)
	for _, row := range res.Rows {
		cells := make([]any, len(row))
		for i, v := range row {
			cells[i] = NativeValue(v)
		}
		f.AppendRow(cells)
	}
	return f
}

// NativeValue converts a kernel Value into its native Go scalar.
func NativeValue(v dbcore.Value) any {
	switch v.Kind {
	case dbcore.ValNull:
		return nil
	case dbcore.ValInt:
		return v.I
	case dbcore.ValFloat:
		return v.F
	case dbcore.ValText:
		return v.S
	case dbcore.ValBlob:
		return v.B
	case dbcore.ValBool:
		return v.Bool
	case dbcore.ValTime:
		return v.T
	default:
		return v.String()
	}
}
