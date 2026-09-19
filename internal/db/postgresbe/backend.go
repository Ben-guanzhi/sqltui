package postgresbe

import (
	"context"
	"fmt"
	"strings"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// Config holds the connection parameters for a PostgreSQL server.
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	DbName   string
	SslMode  string
}

// Dsn renders the config as a driver connection string:
// postgres://user:pass@host:port/dbname?sslmode=<sslmode>
func (c Config) Dsn() string {
	return BuildDsn(c.UserName, c.Password, c.Host, c.Port, c.DbName, c.SslMode)
}

// Backend adapts the postgres data-access layer to the db.Backend contract.
type Backend struct {
	db   *DB
	host string
}

var _ db.Backend = (*Backend)(nil)

// Connect opens a postgres connection and pings it before returning.
func Connect(cfg Config) (*Backend, error) {
	conn, err := NewDB(cfg.Dsn(), cfg.DbName)
	if err != nil {
		return nil, err
	}
	return &Backend{db: conn, host: cfg.Host}, nil
}

func (b *Backend) Kind() string { return "postgres" }

func (b *Backend) Title() string {
	if b.db == nil || b.db.dbName == "" {
		return "postgres"
	}
	return fmt.Sprintf("postgres://%s/%s", b.host, b.db.dbName)
}

// Run executes a statement with the same DQL / RETURNING routing as the
// underlying data-access layer. Query results come back as an all-string
// frame via the StringFrame path.
func (b *Backend) Run(stmt string) (db.Result, error) {
	words := strings.Fields(strings.TrimSpace(stmt))
	if len(words) == 0 {
		return db.Result{}, fmt.Errorf("empty query")
	}

	isDQL := false
	switch strings.ToLower(words[0]) {
	case SELECT, WITH, SHOW, EXPLAIN, VALUES, TABLE:
		isDQL = true
	default:
		isDQL = hasReturningClause(words)
	}

	if isDQL {
		fields, records, err := b.db.RawQuery(stmt)
		if err != nil {
			return db.Result{}, err
		}
		return db.Result{Frame: db.StringFrame(fields, records)}, nil
	}

	tag, err := b.db.RawExec(stmt)
	if err != nil {
		return db.Result{}, err
	}
	exec := &db.ExecResult{HasLastInsert: false}
	exec.RowsAffected = int64(tag.RowsAffected())
	return db.Result{Exec: exec}, nil
}

// Namespaces lists the user schemas of the database.
func (b *Backend) Namespaces() ([]string, error) {
	return b.db.ListSchemas()
}

// CurrentNamespace reports the schema new objects resolve to on this
// connection (usually "public"), so browsers can scope their listing to it.
func (b *Backend) CurrentNamespace() string {
	if b.db != nil && b.db.conn != nil {
		var schema string
		err := b.db.conn.QueryRow(context.Background(), "SELECT current_schema()").Scan(&schema)
		if err == nil && schema != "" {
			return schema
		}
	}
	return "public"
}

// Tables lists the tables of one schema.
func (b *Backend) Tables(namespace string) ([]string, error) {
	return b.db.ListTables(namespace)
}

// FetchTable loads up to limit rows of namespace.table.
func (b *Backend) FetchTable(namespace, table string, limit int) (*data.Frame, error) {
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d",
		quoteIdentifier(namespace), quoteIdentifier(table), limit)
	fields, records, err := b.db.RawQuery(query)
	if err != nil {
		return nil, err
	}
	return db.StringFrame(fields, records), nil
}

// PrimaryKeys lists the primary-key column names of namespace.table.
func (b *Backend) PrimaryKeys(namespace, table string) ([]string, error) {
	return b.db.ListPrimaryKeys(namespace, table)
}

// ColumnsMeta returns column metadata for namespace.table.
func (b *Backend) ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error) {
	return b.db.ColumnsMeta(namespace, table)
}

// ColumnIndexTypes returns index type labels for indexed columns of namespace.table.
func (b *Backend) ColumnIndexTypes(namespace, table string) (map[string]string, error) {
	return b.db.ColumnIndexTypes(namespace, table)
}

func (b *Backend) Close() error {
	return b.db.Close()
}
