package postgresbe

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	dbapi "github.com/LinPr/sqltui/internal/db"
)

const (
	// DQL keywords
	SELECT  = "select"
	WITH    = "with"
	SHOW    = "show"
	EXPLAIN = "explain"
	VALUES  = "values"
	TABLE   = "table"

	// max rows fetched when browsing a table from the tree view
	FetchLimit = 200
)

var (
	DbClient *DB
	// clientMu serializes the check-close-assign swap of DbClient: dial
	// commands run in goroutines and two overlapping reconnects must not
	// race on the global client.
	clientMu sync.Mutex
)

type DB struct {
	conn   *pgx.Conn
	dsn    string
	dbName string
}

// BuildDsn builds a postgres connection string of the form
// postgres://user:pass@host:port/dbname?sslmode=<sslMode>
func BuildDsn(userName, password, host, port, dbName, sslMode string) string {
	if sslMode == "" {
		sslMode = "disable"
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(userName, password),
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + dbName,
		RawQuery: "sslmode=" + url.QueryEscape(sslMode),
	}
	return u.String()
}

func NewDB(dsn string, dbName string) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Close(ctx)
		return nil, fmt.Errorf("ping: %w", err)
	}

	clientMu.Lock()
	defer clientMu.Unlock()
	if DbClient != nil {
		DbClient.conn.Close(context.Background())
	}

	DbClient = &DB{
		conn:   conn,
		dsn:    dsn,
		dbName: dbName,
	}
	return DbClient, nil
}

func GetDB() *DB {
	return DbClient
}

func GetDbName() string {
	if DbClient == nil {
		return ""
	}
	return DbClient.dbName
}

type RawCommandResult struct {
	Fields  []string
	Records [][]string
	Result  pgconn.CommandTag
	IsDQL   bool
}

func (db *DB) RawSqlCommand(query string) (rawCmdResult RawCommandResult, err error) {
	words := strings.Fields(strings.TrimSpace(query))
	if len(words) == 0 {
		return rawCmdResult, fmt.Errorf("empty query")
	}

	switch strings.ToLower(words[0]) {
	case SELECT, WITH, SHOW, EXPLAIN, VALUES, TABLE:
		rawCmdResult.IsDQL = true
	default:
		rawCmdResult.IsDQL = hasReturningClause(words)
	}

	if rawCmdResult.IsDQL {
		rawCmdResult.Fields, rawCmdResult.Records, err = db.RawQuery(query)
		return rawCmdResult, err
	}
	rawCmdResult.Result, err = db.RawExec(query)
	return rawCmdResult, err
}

// hasReturningClause reports whether the statement contains a RETURNING
// clause, whose result set would be silently discarded by db.Exec.
func hasReturningClause(words []string) bool {
	for _, word := range words[1:] {
		if strings.EqualFold(word, "returning") {
			return true
		}
	}
	return false
}

func (db *DB) RawQuery(query string) (fields []string, records [][]string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := db.conn.Query(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	return readRecords(rows)
}

func (db *DB) RawExec(query string) (pgconn.CommandTag, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tag, err := db.conn.Exec(ctx, query)
	return tag, err
}

func (db *DB) ListSchemas() ([]string, error) {
	query := `SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schema_name`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return schemas, rows.Err()
}

func (db *DB) ListTables(schema string) ([]string, error) {
	query := `SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1
		ORDER BY table_name`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.conn.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func (db *DB) FetchTableRecords(schema, table string) (fields []string, records [][]string, err error) {
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d",
		quoteIdentifier(schema), quoteIdentifier(table), FetchLimit)
	return db.RawQuery(query)
}

// ListPrimaryKeys lists the primary-key column names of schema.table, in
// index-ordinal order. Empty when the table has no primary key.
func (db *DB) ListPrimaryKeys(schema, table string) ([]string, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("postgres connection is not open")
	}
	regclass := quoteIdentifier(schema) + "." + quoteIdentifier(table)
	query := `SELECT a.attname FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indisprimary AND i.indrelid = $1::regclass
		ORDER BY array_position(i.indkey, a.attnum)`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.conn.Query(ctx, query, regclass)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pks []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		pks = append(pks, col)
	}
	return pks, rows.Err()
}

// ColumnsMeta returns column metadata for schema.table, in ordinal order.
// Column comments are resolved via col_description.
func (db *DB) ColumnsMeta(schema, table string) ([]dbapi.ColumnMeta, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("postgres connection is not open")
	}
	regclass := quoteIdentifier(schema) + "." + quoteIdentifier(table)
	query := `SELECT c.column_name, c.data_type, c.is_nullable, c.column_default,
		col_description($1::regclass, c.ordinal_position)
		FROM information_schema.columns c
		WHERE c.table_schema = $2 AND c.table_name = $3
		ORDER BY c.ordinal_position`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.conn.Query(ctx, query, regclass, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []dbapi.ColumnMeta
	for rows.Next() {
		var c dbapi.ColumnMeta
		var defaultVal *string
		var comment *string
		if err := rows.Scan(&c.Name, &c.DataType, &c.IsNullable, &defaultVal, &comment); err != nil {
			return nil, err
		}
		if defaultVal != nil {
			c.Default = *defaultVal
		}
		if comment != nil {
			c.Comment = *comment
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// ColumnIndexTypes returns a column→index-type map for schema.table.
// Priority: PK > UNIQUE > INDEX.
func (db *DB) ColumnIndexTypes(schema, table string) (map[string]string, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("postgres connection is not open")
	}
	query := `SELECT a.attname,
		CASE WHEN ix.indisprimary THEN 'PK'
		     WHEN ix.indisunique THEN 'UNIQUE'
		     ELSE 'INDEX' END AS index_type
	FROM pg_class t
	JOIN pg_index ix ON t.oid = ix.indrelid
	JOIN pg_class i ON i.oid = ix.indexrelid
	JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
	WHERE t.relname = $1
	  AND t.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $2)
	ORDER BY ix.indisprimary DESC, ix.indisunique DESC`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.conn.Query(ctx, query, table, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var colName, label string
		if err := rows.Scan(&colName, &label); err != nil {
			return nil, err
		}
		if existing, ok := result[colName]; !ok || indexPriority(existing) < indexPriority(label) {
			result[colName] = label
		}
	}
	return result, rows.Err()
}

func indexPriority(label string) int {
	switch label {
	case "PK":
		return 3
	case "UNIQUE":
		return 2
	case "INDEX":
		return 1
	}
	return 0
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func readRecords(rows pgx.Rows) (fields []string, records [][]string, err error) {
	fds := rows.FieldDescriptions()
	fields = make([]string, len(fds))
	for i, f := range fds {
		fields[i] = f.Name
	}

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, nil, err
		}

		currentRow := make([]string, len(vals))
		for i, v := range vals {
			currentRow[i] = formatAny(v)
		}
		records = append(records, currentRow)
	}
	return fields, records, rows.Err()
}

func formatAny(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case time.Time:
		if val.Hour() == 0 && val.Minute() == 0 && val.Second() == 0 && val.Nanosecond() == 0 {
			return val.Format("2006-01-02")
		}
		return val.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", val)
	}
}

func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close(context.Background())
	}
	return nil
}
