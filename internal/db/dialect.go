// Small presentation-layer helpers that predate the dbcore migration and are
// still used by the UI's file-mode fallbacks. Statement classification
// (RETURNING detection) moved to the kernel (dbcore.IsReadQuery), row reading
// to the kernel's typed Execute, and identifier quoting to dbcore/sqltext —
// the engine backends no longer carry their own copies of any of it.
package db

import "strings"

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

// QuoteIdentDouble quotes an identifier with double quotes, doubling any
// embedded double quote (the file-mode default rendering).
func QuoteIdentDouble(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
