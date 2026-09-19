package oraclebe

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// Config holds the connection parameters for an Oracle database.
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	DbName   string
}

// Validate checks that all required fields are present and returns an error
// describing the first missing field, or nil if the config is valid.
func (c Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port == "" {
		return fmt.Errorf("port is required")
	}
	if c.UserName == "" {
		return fmt.Errorf("username is required")
	}
	if c.DbName == "" {
		return fmt.Errorf("database name is required")
	}
	return nil
}

// Dsn renders the config as a driver connection string with URL-encoded
// credentials so that special characters in passwords are handled safely.
func (c Config) Dsn() string {
	return fmt.Sprintf("oracle://%s:%s@%s:%s/%s",
		url.QueryEscape(c.UserName), url.QueryEscape(c.Password),
		c.Host, c.Port, url.QueryEscape(c.DbName))
}

// Backend adapts the Oracle data-access layer to the db.Backend contract.
type Backend struct {
	db   *DB
	host string
}

var _ db.Backend = (*Backend)(nil)

// Connect opens an Oracle connection, validates the config, pings the
// server, and returns a ready-to-use Backend.
func Connect(cfg Config) (*Backend, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("oracle connect: invalid config: %w", err)
	}
	conn, err := NewDB(cfg.Dsn(), cfg.DbName)
	if err != nil {
		return nil, fmt.Errorf("oracle connect: %w", err)
	}
	return &Backend{db: conn, host: cfg.Host}, nil
}

// Kind returns the engine identifier used for keyword autocompletion.
func (b *Backend) Kind() string { return "oracle" }

// Title returns a human-readable connection title for the UI header.
func (b *Backend) Title() string {
	if b.db == nil {
		return "oracle://(disconnected)"
	}
	return fmt.Sprintf("oracle://%s/%s", b.host, b.db.dbName)
}

// Run executes a statement, routing queries vs. mutations. Query results
// come back as a typed data.Frame; mutations return an ExecResult with
// RowsAffected.
func (b *Backend) Run(stmt string) (db.Result, error) {
	words := strings.Fields(strings.TrimSpace(stmt))
	if len(words) == 0 {
		return db.Result{}, fmt.Errorf("empty query")
	}

	isDQL := false
	switch strings.ToLower(words[0]) {
	case SELECT, WITH, SHOW, TABLE, VALUES:
		isDQL = true
	case "desc", "describe":
		isDQL = true
	default:
		switch {
		case strings.HasPrefix(words[0], "("):
			isDQL = true
		default:
			isDQL = hasReturningClause(words)
		}
	}

	if isDQL {
		frame, err := b.queryFrame(stmt)
		if err != nil {
			return db.Result{}, fmt.Errorf("oracle query: %w", err)
		}
		return db.Result{Frame: frame}, nil
	}

	res, err := b.db.RawExec(stmt)
	if err != nil {
		return db.Result{}, fmt.Errorf("oracle exec: %w", err)
	}
	exec := &db.ExecResult{HasLastInsert: false}
	if res != nil {
		if n, err := res.RowsAffected(); err == nil {
			exec.RowsAffected = n
		}
	}
	return db.Result{Exec: exec}, nil
}

// Namespaces lists the schemas on the server.
func (b *Backend) Namespaces() ([]string, error) {
	return b.db.ShowSchemas()
}

// CurrentNamespace reports the schema this connection was opened against.
func (b *Backend) CurrentNamespace() string {
	if b.db == nil {
		return ""
	}
	return b.db.dbName
}

// Tables lists the tables of one schema.
func (b *Backend) Tables(namespace string) ([]string, error) {
	return b.db.ShowSchemaTables(namespace)
}

// FetchTable loads up to limit rows of namespace.table, preserving native
// Go types for accurate display.
func (b *Backend) FetchTable(namespace, table string, limit int) (*data.Frame, error) {
	if limit <= 0 {
		limit = 1
	} else if limit > 10000 {
		limit = 10000
	}
	ident := quoteIdent(table)
	if namespace != "" {
		ident = quoteIdent(namespace) + "." + ident
	}
	query := fmt.Sprintf("SELECT * FROM %s WHERE ROWNUM <= %d", ident, limit)
	return b.queryFrame(query)
}

// PrimaryKeys lists the primary-key column names of namespace.table.
func (b *Backend) PrimaryKeys(namespace, table string) ([]string, error) {
	return b.db.PrimaryKeys(namespace, table)
}

// ColumnsMeta returns column metadata for namespace.table.
func (b *Backend) ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error) {
	return b.db.ColumnsMeta(namespace, table)
}

// ColumnIndexTypes returns index type labels for indexed columns.
func (b *Backend) ColumnIndexTypes(namespace, table string) (map[string]string, error) {
	return b.db.ColumnIndexTypes(namespace, table)
}

// Close releases the underlying database connection.
func (b *Backend) Close() error {
	return b.db.Close()
}

// queryFrame runs a query and scans the result set into a frame, preserving
// native Go types (int64, float64, bool, time.Time, string, nil) instead of
// converting everything to strings.
func (b *Backend) queryFrame(query string) (*data.Frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := b.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	frame := data.New(fields...)
	for rows.Next() {
		values := make([]any, len(fields))
		for i := range values {
			values[i] = new(any)
		}
		if err := rows.Scan(values...); err != nil {
			return nil, err
		}
		row := make([]any, len(fields))
		for i, value := range values {
			row[i] = cellValue(*(value.(*any)))
		}
		frame.AppendRow(row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return frame, nil
}

// cellValue converts a raw driver value into a display-friendly Go type.
func cellValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	case time.Time:
		return v
	case string, int64, float64, bool:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
