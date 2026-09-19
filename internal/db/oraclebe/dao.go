package oraclebe

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/sijms/go-ora/v2"

	dbapi "github.com/LinPr/sqltui/internal/db"
)

const (
	SELECT   = "select"
	WITH     = "with"
	SHOW     = "show"
	TABLE    = "table"
	VALUES   = "values"

	// QueryTimeout is the default timeout for queries
	QueryTimeout = 30 * time.Second

	// ConnectTimeout is the timeout for establishing a connection
	ConnectTimeout = 10 * time.Second

	// MaxRetries is the maximum number of connection retry attempts
	MaxRetries = 3

	// RetryDelay is the delay between retry attempts
	RetryDelay = 1 * time.Second
)

var (
	DbClient *DB
	clientMu sync.RWMutex
)

type DB struct {
	*sql.DB
	dsn    string
	dbName string
}

// NewDB opens an Oracle connection with retry logic and connection pool settings.
func NewDB(dsn string, dbName string) (*DB, error) {
	var dbc *sql.DB
	var err error

	// Retry connection attempts
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(RetryDelay * time.Duration(attempt))
		}

		dbc, err = sql.Open("oracle", dsn)
		if err != nil {
			continue
		}

		// Configure connection pool
		dbc.SetMaxOpenConns(25)
		dbc.SetMaxIdleConns(5)
		dbc.SetConnMaxIdleTime(5 * time.Minute)
		dbc.SetConnMaxLifetime(30 * time.Minute)

		ctx, cancel := context.WithTimeout(context.Background(), ConnectTimeout)
		err = dbc.PingContext(ctx)
		cancel()

		if err == nil {
			break
		}
		dbc.Close()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect after %d attempts: %w", MaxRetries+1, err)
	}

	clientMu.Lock()
	defer clientMu.Unlock()
	if DbClient != nil {
		DbClient.DB.Close()
	}

	DbClient = &DB{
		DB:     dbc,
		dsn:    dsn,
		dbName: dbName,
	}
	return DbClient, nil
}

// GetDB returns the global database client.
func GetDB() *DB {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return DbClient
}

type RawCommandResult struct {
	Fields  []string
	Records [][]string
	Result  sql.Result
	IsDQL   bool
}

func (db *DB) RawSqlCommand(query string) (rawCmdResult RawCommandResult, err error) {
	cmd := strings.Fields(strings.TrimSpace(query))
	if len(cmd) == 0 {
		return rawCmdResult, fmt.Errorf("empty query")
	}

	first := strings.ToLower(cmd[0])
	switch first {
	case SELECT, WITH, SHOW, TABLE, VALUES:
		rawCmdResult.IsDQL = true
	case "desc", "describe":
		rawCmdResult.IsDQL = true
	default:
		switch {
		case strings.HasPrefix(first, "("):
			rawCmdResult.IsDQL = true
		default:
			rawCmdResult.IsDQL = hasReturningClause(cmd)
		}
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
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	fields, err = rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	records, err = readRecords(rows)
	if err != nil {
		return nil, nil, err
	}

	return fields, records, nil
}

func (db *DB) RawExec(query string) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	res, err := db.ExecContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (db *DB) ShowSchemas() ([]string, error) {
	query := "SELECT username FROM all_users ORDER BY username"
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
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

func (db *DB) ShowSchemaTables(schema string) ([]string, error) {
	query := "SELECT table_name FROM all_tables WHERE owner = UPPER(:1) ORDER BY table_name"
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, schema)
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

func (db *DB) FetchTableFields(table string) ([]string, error) {
	query := "SELECT column_name FROM all_tab_columns WHERE table_name = UPPER(:1) ORDER BY column_id"
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records, err := readRecords(rows)
	if err != nil {
		return nil, err
	}

	fields := []string{}
	for _, record := range records {
		fields = append(fields, record[0])
	}
	return fields, nil
}

func (db *DB) FetchTableRecords(table string) ([][]string, error) {
	query := fmt.Sprintf("SELECT * FROM %s", quoteIdent(table))
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records, err := readRecords(rows)
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}

func (db *DB) PrimaryKeys(schema, table string) ([]string, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("oracle connection is not open")
	}
	query := `SELECT cols.column_name
		FROM all_constraints cons
		JOIN all_cons_columns cols ON cons.constraint_name = cols.constraint_name
		WHERE cons.constraint_type = 'P'
		AND cols.table_name = UPPER(:1)
		ORDER BY cols.position`

	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, table)
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

func (db *DB) ColumnsMeta(schema, table string) ([]dbapi.ColumnMeta, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("oracle connection is not open")
	}
	query := `SELECT column_name, data_type, nullable, data_default
		FROM all_tab_columns
		WHERE table_name = UPPER(:1)
		ORDER BY column_id`

	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []dbapi.ColumnMeta
	for rows.Next() {
		var c dbapi.ColumnMeta
		var defaultVal sql.NullString
		if err := rows.Scan(&c.Name, &c.DataType, &c.IsNullable, &defaultVal); err != nil {
			return nil, err
		}
		if defaultVal.Valid {
			c.Default = defaultVal.String
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (db *DB) ColumnIndexTypes(schema, table string) (map[string]string, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("oracle connection is not open")
	}
	query := `SELECT
		i.index_name,
		c.column_name,
		CASE WHEN i.uniqueness = 'UNIQUE' THEN 'UNIQUE' ELSE 'INDEX' END AS index_type
		FROM all_indexes i
		JOIN all_ind_columns c ON i.index_name = c.index_name
		WHERE i.table_name = UPPER(:1)
		ORDER BY CASE WHEN i.uniqueness = 'UNIQUE' THEN 0 ELSE 1 END`

	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var indexName, colName, indexType string
		if err := rows.Scan(&indexName, &colName, &indexType); err != nil {
			return nil, err
		}
		if existing, ok := result[colName]; !ok || indexPriority(existing) < indexPriority(indexType) {
			result[colName] = indexType
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

func quoteIdent(name string) string {
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}

func readRecords(rows *sql.Rows) ([][]string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var records [][]string
	for rows.Next() {
		record := make([]any, len(columns))
		for i := range columns {
			record[i] = new(any)
		}

		if err := rows.Scan(record...); err != nil {
			return nil, err
		}
		var currentRow []string
		for _, rawValue := range record {
			currentRow = append(currentRow, formatValue(*(rawValue.(*any))))
		}
		records = append(records, currentRow)
	}
	return records, rows.Err()
}

func formatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(v)
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}
