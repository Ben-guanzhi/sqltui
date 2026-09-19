package sqlserverbe

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/microsoft/go-mssqldb"

	dbapi "github.com/LinPr/sqltui/internal/db"
)

const (
	SELECT   = "select"
	WITH     = "with"
	EXPLAIN  = "explain"
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

// NewDB opens a SQL Server connection with retry logic and connection pool settings.
func NewDB(dsn string, dbName string) (*DB, error) {
	var dbc *sql.DB
	var err error

	// Retry connection attempts
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(RetryDelay * time.Duration(attempt))
		}

		dbc, err = sql.Open("sqlserver", dsn)
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
	case SELECT, WITH, EXPLAIN, SHOW, TABLE, VALUES:
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

func (db *DB) ShowDatabases() ([]string, error) {
	query := "SELECT name FROM sys.databases ORDER BY name"
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, err
		}
		databases = append(databases, database)
	}

	return databases, rows.Err()
}

func (db *DB) ShowDatabaseTables(database string) ([]string, error) {
	query := fmt.Sprintf("SELECT TABLE_NAME FROM %s.INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME", quoteIdent(database))
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
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
	query := "SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = @p1 ORDER BY ORDINAL_POSITION"
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
	query := fmt.Sprintf("select * from %s", quoteIdent(table))
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

func (db *DB) PrimaryKeys(database, table string) ([]string, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("sqlserver connection is not open")
	}
	query := `SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE OBJECTPROPERTY(OBJECT_ID(TABLE_CATALOG + '.' + TABLE_SCHEMA + '.' + TABLE_NAME), 'IsPrimaryKey') = 1
		AND TABLE_NAME = @p1
		AND TABLE_CATALOG = @p2
		ORDER BY ORDINAL_POSITION`

	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, table, database)
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

func (db *DB) ColumnsMeta(database, table string) ([]dbapi.ColumnMeta, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("sqlserver connection is not open")
	}
	query := `SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_NAME = @p1
		AND TABLE_CATALOG = @p2
		ORDER BY ORDINAL_POSITION`

	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, table, database)
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

func (db *DB) ColumnIndexTypes(database, table string) (map[string]string, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("sqlserver connection is not open")
	}
	query := `SELECT
		i.name AS index_name,
		COL_NAME(ic.object_id, ic.column_id) AS column_name,
		i.is_unique
		FROM sys.indexes i
		INNER JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		WHERE i.object_id = OBJECT_ID(@p1 + '.dbo.' + @p2)
		AND i.type > 0
		ORDER BY CASE WHEN i.is_primary_key = 1 THEN 0 WHEN i.is_unique = 1 THEN 1 ELSE 2 END`

	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var indexName, colName string
		var isUnique bool
		if err := rows.Scan(&indexName, &colName, &isUnique); err != nil {
			return nil, err
		}
		label := "INDEX"
		if isUnique {
			label = "UNIQUE"
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

func quoteIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
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
