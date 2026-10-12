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

// HasReturningClause reports whether the statement contains a RETURNING
// clause. Such a statement produces a result set that a plain Exec would
// silently discard.
//
// The scan skips string literals, quoted identifiers, and comments, so a value
// like UPDATE t SET note = 'returning' is not misrouted to the query path
// (which then fails on a statement that returns nothing). Only a whole word
// outside any quoted region counts as a clause.
func HasReturningClause(sql string) bool {
	var quote byte // active quote character; 0 outside a quoted region
	i := 0
	for i < len(sql) {
		c := sql[i]
		if quote != 0 {
			end := quote // brackets close on a different character
			if quote == '[' {
				end = ']'
			}
			switch {
			case c == end && i+1 < len(sql) && sql[i+1] == end:
				i += 2 // doubled quote escapes a single literal quote
				continue
			case c == end:
				quote = 0
				i++
			case quote == '\'' && c == '\\' && i+1 < len(sql):
				i += 2 // backslash escape, MySQL default
			default:
				i++
			}
			continue
		}
		switch c {
		case '\'', '"', '`', '[':
			quote = c
			i++
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				if nl := strings.IndexByte(sql[i:], '\n'); nl >= 0 {
					i += nl + 1
				} else {
					i = len(sql)
				}
				continue
			}
			i++
		case '/':
			if i+1 < len(sql) && sql[i+1] == '*' {
				if end := strings.Index(sql[i:], "*/"); end >= 0 {
					i += end + 2
				} else {
					i = len(sql)
				}
				continue
			}
			i++
		default:
			if isIdentChar(c) {
				start := i
				for i < len(sql) && isIdentChar(sql[i]) {
					i++
				}
				if strings.EqualFold(sql[start:i], "returning") {
					return true
				}
			} else {
				i++
			}
		}
	}
	return false
}

// isIdentChar reports whether c may occur inside a SQL word. ASCII only: the
// RETURNING keyword is ASCII, so a non-ASCII run can never match and is stepped
// over one byte at a time.
func isIdentChar(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
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
