// Package db defines the interface between the UI and live database
// connections (MySQL, PostgreSQL, SQLite files, Redis).
package db

import (
	"context"

	"github.com/LinPr/sqltui/internal/data"
)

// ExecResult reports the outcome of a non-query statement.
type ExecResult struct {
	RowsAffected  int64
	LastInsertID  int64
	HasLastInsert bool // false for engines without insert ids (e.g. postgres)
}

// Result is either a row set (Frame != nil) or an exec outcome.
type Result struct {
	Frame *data.Frame
	Exec  *ExecResult
}

// ColumnMeta describes one column of a live table, for the info overlay
// and the row sheet type display. DataType is the engine-native type name
// (e.g. "int", "varchar(255)", "text"); IsNullable is "YES"/"NO" or "" when
// unknown; Default is the column default literal or ""; Comment is the
// column comment or "".
type ColumnMeta struct {
	Name       string
	DataType   string
	IsNullable string
	Default    string
	Comment    string
}

// Backend is a live SQL database connection.
type Backend interface {
	// Kind identifies the engine: "mysql", "postgres" or "sqlite".
	Kind() string
	// Title is a short human label for the connection (e.g. "mysql://host/db").
	Title() string
	// Run executes a statement, routing queries vs. mutations automatically.
	Run(stmt string) (Result, error)
	// Namespaces lists databases (mysql) or schemas (postgres). Engines
	// without namespaces return a single empty string.
	Namespaces() ([]string, error)
	// Tables lists the tables in a namespace.
	Tables(namespace string) ([]string, error)
	// FetchTable loads up to limit rows of a table.
	FetchTable(namespace, table string, limit int) (*data.Frame, error)
	// PrimaryKeys lists the primary-key column names of namespace.table
	// (empty when the table has no primary key). Engines without namespaces
	// ignore namespace.
	PrimaryKeys(namespace, table string) ([]string, error)
	// ColumnsMeta returns column metadata for namespace.table, in ordinal
	// order. Engines without namespaces ignore namespace.
	ColumnsMeta(namespace, table string) ([]ColumnMeta, error)
	// ColumnIndexTypes returns a map of column name → index type label for each
	// indexed column in namespace.table. Labels are "PK", "UNIQUE", or "INDEX"
	// (highest priority wins when a column appears in multiple indexes).
	// Returns an empty map (not an error) when the table has no indexes.
	ColumnIndexTypes(namespace, table string) (map[string]string, error)
	Close() error
}

// KVBackend is a live key-value store connection (Redis).
type KVBackend interface {
	// Title is a short human label for the connection.
	Title() string
	// Do runs a raw command and returns a rendered result.
	Do(args []string) (string, error)
	// ScanKeys lists keys of one type, up to a server-side cap.
	ScanKeys(keyType string) ([]string, error)
	// Value fetches and renders the value stored at key.
	Value(key string) (string, error)
	Close() error
}

// Optional capabilities a backend may implement beyond Backend. The UI probes
// them with type assertions and degrades gracefully when a backend lacks one.

// Namespaced reports the namespace (database, schema, or org) the connection
// is currently operating against.
type Namespaced interface {
	CurrentNamespace() string
}

// TimeRanger reports and adjusts the time window of log queries.
type TimeRanger interface {
	GetTimeRange() int
	SetTimeRange(minutes int)
}

// Paged reports and navigates paginated results.
type Paged interface {
	GetPage() int
	SetPage(page int)
	GetPageSize() int
	SetPageSize(pageSize int)
}

// QueryParamsSetter bundles TimeRanger and Paged: the capabilities needed to
// drive the unified query-params popup.
type QueryParamsSetter interface {
	TimeRanger
	Paged
}

// TimeWindowResetter resets the sliding query time window, e.g. after the
// time range or page of a paginated log query changes.
type TimeWindowResetter interface {
	ResetTimeWindow()
}

// TableTargeter spells namespace.table as a FROM-clause target using the
// engine's own identifier quoting, exactly the way FetchTable names it.
// The inline-prompt SQL builders probe this so a pane's recorded source
// table becomes real SQL without duplicating per-engine quoting in the UI.
type TableTargeter interface {
	TableTarget(namespace, table string) string
}

// Quoter quotes a SQL identifier using the engine's own quoting style.
// MySQL uses backticks, others use double quotes.
type Quoter interface {
	QuoteIdent(name string) string
}

// Escaper wraps a string value as a SQL literal, applying engine-specific
// escaping (e.g. MySQL doubles backslashes).
type Escaper interface {
	EscapeLiteral(s string) string
}

// BoundRunner executes a statement with bound parameters: values ride as
// driver arguments (nil / string / int64 / float64 / bool / time.Time /
// []byte), never as concatenated text. Backends backed by the dbcore kernel
// implement this; the cell editor uses it for its UPDATE/DELETE statements
// and falls back to inline literals only for backends without it.
type BoundRunner interface {
	RunBound(stmt string, args []any) (Result, error)
}

// Canceler is the optional ability to cancel the statement currently running
// on this backend. Run/RunBound are fire-and-forget from tea.Cmd closures (no
// ctx travels with the call), so a backend registers each statement's cancel
// handle internally and this stops the most recent one. Returning false means
// nothing was in flight - the UI uses that to decide between "canceled" and
// quit.
type Canceler interface {
	CancelRunning() bool
}

// NamespaceWarmer is the optional ability to resolve the connected namespace
// ahead of time. The resolution is a single-row query (postgres's
// current_schema()); the app warms it the moment a connection lands, so the
// update loop's namespace lookups (delete confirmation, completion warm)
// answer from the memo instead of blocking on a cold 30s round trip.
type NamespaceWarmer interface {
	WarmNamespace()
}

// BrowseBackend is the optional ability to load a table's rows past the first
// window: BrowsePage continues a keyset walk (a single sort key, defaulting to
// the table's single primary key) or falls back to OFFSET paging when there
// is no usable sort key. Page 1 comes through the ordinary FetchTable; this
// is the continuation a "next page" key uses.
type BrowseBackend interface {
	BrowsePage(ctx context.Context, namespace, table string, req *BrowsePageRequest) (*BrowsePageResult, error)
}

// BrowsePageResult is one window of a table plus everything the caller needs
// to continue it: the total (when actually read), and the keyset anchor
// column ("" means OFFSET mode, so the next window uses Page instead of After).
type BrowsePageResult struct {
	Frame      *data.Frame
	Total      uint64
	TotalFound bool
	SortColumn string
}

// BrowsePageRequest selects one window of a table's rows.
type BrowsePageRequest struct {
	// Page is 1-based and used in OFFSET mode (no single sort key).
	Page int
	// After continues a keyset walk: the values of the sort column at the end
	// of the previous window. It wins over Page when a sort key exists.
	After []string
	// Limit caps the window.
	Limit int
}
