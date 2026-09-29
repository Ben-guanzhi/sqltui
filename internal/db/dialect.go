// Shared statement-classification, quoting, and record-reading helpers used
// by the SQL backends (mysqlbe, postgresbe, sqlitebe, sqlserverbe, oraclebe).
// Each backend delegates to these instead of carrying its own copy.
package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// HasReturningClause reports whether the statement, tokenized as
// space-separated words, contains a RETURNING clause. Such a statement
// produces a result set that a plain Exec would silently discard.
func HasReturningClause(words []string) bool {
	for _, word := range words[1:] {
		if strings.EqualFold(word, "returning") {
			return true
		}
	}
	return false
}

// IndexPriority ranks index-type labels; when a column belongs to several
// indexes the highest priority wins.
func IndexPriority(label string) int {
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

// QuoteIdentBacktick quotes an identifier with MySQL backticks, doubling any
// embedded backtick.
func QuoteIdentBacktick(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// QuoteIdentBracket quotes an identifier with SQL Server [brackets],
// doubling any embedded ].
func QuoteIdentBracket(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// QuoteIdentDouble quotes an identifier with double quotes, doubling any
// embedded double quote (PostgreSQL, SQLite, Oracle).
func QuoteIdentDouble(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// FormatNativeCell formats a cell scanned into an *any driver value: NULL
// renders as "NULL", byte slices as their text form, times as "YYYY-MM-DD
// HH:MM:SS", everything else via %v.
func FormatNativeCell(v any) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(val)
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ReadNativeRows reads all rows of rows, scanning each cell into an any
// value and formatting it with FormatNativeCell.
func ReadNativeRows(rows *sql.Rows) ([][]string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var records [][]string
	for rows.Next() {
		record := make([]any, len(columns))
		for i := range record {
			record[i] = new(any)
		}
		if err := rows.Scan(record...); err != nil {
			return nil, err
		}
		currentRow := make([]string, 0, len(columns))
		for _, rawValue := range record {
			currentRow = append(currentRow, FormatNativeCell(*(rawValue.(*any))))
		}
		records = append(records, currentRow)
	}
	return records, rows.Err()
}

// ReadRawBytesRows reads all rows of rows, scanning each cell into
// sql.RawBytes; NULL renders as the empty string.
func ReadRawBytesRows(rows *sql.Rows) ([][]string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var records [][]string
	for rows.Next() {
		record := make([]any, len(columns))
		for i := range record {
			record[i] = &sql.RawBytes{}
		}
		if err := rows.Scan(record...); err != nil {
			return nil, err
		}
		currentRow := make([]string, 0, len(columns))
		for _, rawValue := range record {
			currentRow = append(currentRow, string(*rawValue.(*sql.RawBytes)))
		}
		records = append(records, currentRow)
	}
	return records, rows.Err()
}
