package sqlitebe

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	dbapi "github.com/LinPr/sqltui/internal/db"
)

const (
	// DQL
	SELECT  = "select"
	PRAGMA  = "pragma"
	WITH    = "with"
	EXPLAIN = "explain"
	VALUES  = "values"

	// DDL & DML & DCL & TCL ....

	// max rows fetched when browsing a table from the tree view
	FetchLimit = 200
)

var (
	DbClinet *DB
)

type DB struct {
	*sql.DB
	File string
}

func NewDB(file string) (*DB, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil, fmt.Errorf("sqlite file path is empty")
	}

	// reuse the cached client only if it points to the same file
	if DbClinet != nil && DbClinet.File == file {
		return DbClinet, nil
	}

	if _, err := os.Stat(file); err != nil {
		return nil, err
	}

	dbc, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}

	if err := dbc.Ping(); err != nil {
		dbc.Close()
		return nil, err
	}

	// replace the previously cached client (re-login with another file)
	if DbClinet != nil {
		DbClinet.DB.Close()
	}

	DbClinet = &DB{
		DB:   dbc,
		File: file,
	}
	return DbClinet, nil
}

type RawCommandResult struct {
	Fields  []string
	Records [][]string
	Result  sql.Result
	IsDQL   bool
}

func (db *DB) RawSqlCommand(query string) (rawCmdResult RawCommandResult, err error) {
	words := strings.Fields(strings.TrimSpace(query))
	if len(words) == 0 {
		return rawCmdResult, fmt.Errorf("empty query")
	}

	switch strings.ToLower(words[0]) {
	case SELECT, PRAGMA, WITH, EXPLAIN, VALUES:
		rawCmdResult.IsDQL = true
	default:
		// INSERT/UPDATE/DELETE ... RETURNING (SQLite >= 3.35) also produces
		// a result set that db.Exec would silently discard.
		rawCmdResult.IsDQL = dbapi.HasReturningClause(words)
	}

	if rawCmdResult.IsDQL {
		rawCmdResult.Fields, rawCmdResult.Records, err = db.RawQuery(query)
		return rawCmdResult, err
	}
	rawCmdResult.Result, err = db.RawExec(query)
	return rawCmdResult, err
}

func (db *DB) RawQuery(query string) (fields []string, records [][]string, err error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	fields, err = rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	records, err = dbapi.ReadRawBytesRows(rows)
	if err != nil {
		return nil, nil, err
	}

	return fields, records, nil
}

func (db *DB) RawExec(query string) (sql.Result, error) {
	res, err := db.Exec(query)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ListTables lists all user tables of the sqlite database file.
func (db *DB) ListTables() ([]string, error) {
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
	rows, err := db.Query(query)
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

// FetchTableFields returns the column names of a table via PRAGMA table_info.
func (db *DB) FetchTableFields(table string) ([]string, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", dbapi.QuoteIdentDouble(table))
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records, err := dbapi.ReadRawBytesRows(rows)
	if err != nil {
		return nil, err
	}

	// PRAGMA table_info columns: cid, name, type, notnull, dflt_value, pk
	fields := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) > 1 {
			fields = append(fields, record[1])
		}
	}
	return fields, nil
}

func (db *DB) FetchTableRecords(table string) ([][]string, error) {
	query := fmt.Sprintf("select * from %s limit %d", dbapi.QuoteIdentDouble(table), FetchLimit)
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return dbapi.ReadRawBytesRows(rows)
}

// ListPrimaryKeys lists the primary-key column names of table, in pk-ordinal
// order. Empty when the table has no primary key. The namespace argument is
// ignored: sqlite files have no schema level.
func (db *DB) ListPrimaryKeys(table string) ([]string, error) {
	query := fmt.Sprintf("SELECT name FROM pragma_table_info(%s) WHERE pk > 0 ORDER BY pk", quoteString(table))
	_, records, err := db.RawQuery(query)
	if err != nil {
		return nil, err
	}
	pks := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) > 0 {
			pks = append(pks, record[0])
		}
	}
	return pks, nil
}

// ColumnsMeta returns column metadata for table, in ordinal order. The
// namespace argument is ignored: sqlite files have no schema level. Column
// comments are not supported and stay empty.
func (db *DB) ColumnsMeta(table string) ([]dbapi.ColumnMeta, error) {
	query := fmt.Sprintf(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info(%s) ORDER BY cid`, quoteString(table))
	_, records, err := db.RawQuery(query)
	if err != nil {
		return nil, err
	}
	cols := make([]dbapi.ColumnMeta, 0, len(records))
	for _, record := range records {
		if len(record) < 4 {
			continue
		}
		c := dbapi.ColumnMeta{
			Name:       record[0],
			DataType:   record[1],
			IsNullable: "NO",
			Default:    record[3],
		}
		// pragma_table_info reports notnull as 0 (nullable) or 1 (not null).
		if record[2] == "0" {
			c.IsNullable = "YES"
		}
		cols = append(cols, c)
	}
	return cols, nil
}

// ColumnIndexTypes returns a column→index-type map for table.
// Priority: PK > UNIQUE > INDEX.
// SQLite uses PRAGMA table_info for PKs and PRAGMA index_list + index_info for other indexes.
func (db *DB) ColumnIndexTypes(table string) (map[string]string, error) {
	result := make(map[string]string)

	// Primary keys from pragma_table_info
	pkQuery := fmt.Sprintf("SELECT name FROM pragma_table_info(%s) WHERE pk > 0", quoteString(table))
	_, pkRecords, err := db.RawQuery(pkQuery)
	if err != nil {
		return nil, err
	}
	for _, r := range pkRecords {
		if len(r) > 0 {
			result[r[0]] = "PK"
		}
	}

	// Other indexes from index_list + index_info
	listQuery := fmt.Sprintf("PRAGMA index_list(%s)", dbapi.QuoteIdentDouble(table))
	_, idxList, err := db.RawQuery(listQuery)
	if err != nil {
		return result, nil // silently degrade if pragma fails
	}
	// index_list columns: seq, name, unique, origin, partial
	for _, idx := range idxList {
		if len(idx) < 4 {
			continue
		}
		idxName := idx[1]
		isUnique := idx[2] == "1"
		origin := idx[3] // 'pk', 'u', 'c'
		if origin == "pk" {
			continue // already captured above
		}
		label := "INDEX"
		if isUnique {
			label = "UNIQUE"
		}

		infoQuery := fmt.Sprintf("PRAGMA index_info(%s)", dbapi.QuoteIdentDouble(idxName))
		_, infoRecords, err := db.RawQuery(infoQuery)
		if err != nil {
			continue
		}
		// index_info columns: seqno, cid, name
		for _, col := range infoRecords {
			if len(col) < 3 {
				continue
			}
			colName := col[2]
			if existing, ok := result[colName]; !ok || dbapi.IndexPriority(existing) < dbapi.IndexPriority(label) {
				result[colName] = label
			}
		}
	}
	return result, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}

// quoteString wraps name in a single-quoted SQL string literal, doubling any
// embedded single quote. Used for the table argument of pragma_table_info.
func quoteString(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}
