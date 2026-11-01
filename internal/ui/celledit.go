// Package ui celledit holds the commit helpers for single-cell edits and
// row deletes. In database mode it builds UPDATE/DELETE statements keyed on
// the table's primary key; in file mode it rewrites the frame in place
// through WithCell/WithoutRows and pushes the new frame.
package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dbcore/dbcore/sqltext"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// PendingEdit captures an in-flight cell edit awaiting confirmation. The
// popup that collects the new value populates this and the app commits it
// via the "saveedit" command.
type PendingEdit struct {
	Frame     *data.Frame
	Row       int
	Col       int
	ColName   string
	OldValue  string
	NewValue  string
	Table     string
	Namespace string
}

// PendingDelete captures one or more rows awaiting deletion. The app populates
// it from the multi-select set (or the single cursor row) and commits it via
// the "deleterows" command.
type PendingDelete struct {
	Frame     *data.Frame
	Rows      []int
	Table     string
	Namespace string
}

// cellSavedMsg carries the outcome of an async UPDATE back to the app.
type cellSavedMsg struct {
	rows int64
	err  error
}

// rowsDeletedMsg carries the outcome of an async DELETE back to the app.
type rowsDeletedMsg struct {
	rows int64
	err  error
}

// quoteIdent quotes a SQL identifier for the backend's dialect: backticks for
// mysql, double quotes otherwise. Embedded quote characters are doubled.
func quoteIdent(be db.Backend, name string) string {
	if be == nil {
		return db.QuoteIdentDouble(name)
	}
	if q, ok := be.(db.Quoter); ok {
		return q.QuoteIdent(name)
	}
	if be.Kind() == "mysql" {
		return db.QuoteIdentBacktick(name)
	}
	return db.QuoteIdentDouble(name)
}

// quoteCol quotes a column identifier for the active dialect.
func quoteCol(be db.Backend, name string) string {
	return quoteIdent(be, name)
}

// sqlLiteral wraps a string value in single quotes with embedded quotes
// doubled. On MySQL (default sql_mode) the backslash is an escape character,
// so backslashes are doubled too — otherwise a value ending in a backslash
// would swallow the closing quote and rewrite the statement (worst case a
// corrupted WHERE clause on DELETE). Everywhere else (PostgreSQL standard
// conforming strings, SQL Server, Oracle, SQLite) the backslash is literal
// and must be left alone.
func sqlLiteral(be db.Backend, s string) string {
	if be != nil {
		if e, ok := be.(db.Escaper); ok {
			return e.EscapeLiteral(s)
		}
		if be.Kind() == "mysql" {
			s = strings.ReplaceAll(s, `\`, `\\`)
		}
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// parseTyped converts the edited string to the cell's native Go type so
// WithCell preserves the column type.
func parseTyped(val string, t data.DType) (any, error) {
	switch t {
	case data.TypeString:
		return val, nil
	case data.TypeBool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("expected bool, got %q", val)
		}
		return b, nil
	case data.TypeDate, data.TypeDatetime:
		// The file-mode infer path (ParseAs) stores these as time.Time over
		// the date/datetime layouts; parsing them as int64 here either
		// rejected "2024-01-01" or silently stored an epoch int into a date
		// cell (display corruption). Parse with the same layouts the
		// inferred frame uses, so an edit round-trips as a date.
		ts, ok := data.ParseAs(val, t)
		if !ok {
			return nil, fmt.Errorf("expected a %s, got %q", t, val)
		}
		return ts, nil
	case data.TypeInt:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected integer, got %q", val)
		}
		return n, nil
	case data.TypeFloat:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("expected float, got %q", val)
		}
		return f, nil
	default:
		return val, nil
	}
}

// buildUpdate returns a single-row UPDATE keyed on the table's primary key.
// The table identifier is dialect-quoted; PK values are read from frame. It
// errors when a named PK column is absent from the frame.
//
// This is the inline-literal fallback for backends that cannot bind
// parameters (the OpenObserve backend is the one live case); kernel-backed
// backends use buildUpdateBound.
func buildUpdate(be db.Backend, table string, pks []string, frame *data.Frame, row int, colName, val string) (string, error) {
	setClause := quoteCol(be, colName) + "=" + sqlLiteral(be, val)
	var where []string
	for _, pk := range pks {
		idx := frame.ColumnIndex(pk)
		if idx < 0 {
			return "", fmt.Errorf("primary key column not found: %s", pk)
		}
		where = append(where, quoteCol(be, pk)+"="+sqlLiteral(be, frame.CellString(row, idx)))
	}
	return "UPDATE " + quoteIdent(be, table) + " SET " + setClause + " WHERE " + strings.Join(where, " AND "), nil
}

// buildDelete returns a DELETE keyed on the table's primary key. Single-PK
// tables produce a "WHERE pk IN (...)" clause; multi-PK tables produce a
// "WHERE (pk0=v AND pk1=v) OR (...)" clause. It errors when there are no PKs
// (the caller checks len(pks)==0 first) or a PK column is missing.
//
// Inline-literal fallback, like buildUpdate; kernel-backed backends use
// buildDeleteBound.
func buildDelete(be db.Backend, table string, pks []string, frame *data.Frame, rows []int) (string, error) {
	if len(pks) == 0 {
		return "", fmt.Errorf("no primary key")
	}
	pkIdx := make([]int, len(pks))
	for i, pk := range pks {
		idx := frame.ColumnIndex(pk)
		if idx < 0 {
			return "", fmt.Errorf("primary key column not found: %s", pk)
		}
		pkIdx[i] = idx
	}
	tbl := quoteIdent(be, table)
	if len(pks) == 1 {
		vals := make([]string, 0, len(rows))
		for _, r := range rows {
			vals = append(vals, sqlLiteral(be, frame.CellString(r, pkIdx[0])))
		}
		return "DELETE FROM " + tbl + " WHERE " + quoteCol(be, pks[0]) + " IN (" + strings.Join(vals, ",") + ")", nil
	}
	ors := make([]string, 0, len(rows))
	for _, r := range rows {
		ands := make([]string, 0, len(pks))
		for i, pk := range pks {
			ands = append(ands, quoteCol(be, pk)+"="+sqlLiteral(be, frame.CellString(r, pkIdx[i])))
		}
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	return "DELETE FROM " + tbl + " WHERE " + strings.Join(ors, " OR "), nil
}

// bindValue converts the edited text into the value to bind for a column of
// the frame's inferred type. Unlike the file-mode parseTyped, date/datetime
// stay text: engines parse date literals from strings, and binding keeps the
// user's exact spelling instead of coercing it to an epoch number.
func bindValue(val string, t data.DType) (any, error) {
	switch t {
	case data.TypeBool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("expected bool, got %q", val)
		}
		return b, nil
	case data.TypeInt:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected integer, got %q", val)
		}
		return n, nil
	case data.TypeFloat:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("expected float, got %q", val)
		}
		return f, nil
	default:
		return val, nil
	}
}

// placeholder spells the idx-th (1-based) bind marker for the backend's
// engine ("?" / "$n" / "@pn" / ":n", from the kernel dialect).
func placeholder(be db.Backend, idx int) string {
	return sqltext.Placeholder(be.Kind(), idx)
}

// buildUpdateBound returns a single-row UPDATE with every value bound as a
// driver parameter: the SET value converted by the frame's column type, and
// the PK values in their native frame types (an int64 id binds as int64, not
// as a quoted string). No user text ever reaches the statement text.
func buildUpdateBound(be db.Backend, table string, pks []string, frame *data.Frame, row int, colName, val string) (string, []any, error) {
	if len(pks) == 0 {
		return "", nil, fmt.Errorf("no primary key")
	}
	ci := frame.ColumnIndex(colName)
	if ci < 0 {
		return "", nil, fmt.Errorf("column not found: %s", colName)
	}
	setVal, err := bindValue(val, frame.Columns[ci].Type)
	if err != nil {
		return "", nil, err
	}
	args := []any{setVal}
	var where []string
	for _, pk := range pks {
		pi := frame.ColumnIndex(pk)
		if pi < 0 {
			return "", nil, fmt.Errorf("primary key column not found: %s", pk)
		}
		where = append(where, quoteCol(be, pk)+"="+placeholder(be, len(args)+1))
		args = append(args, frame.Cell(row, pi))
	}
	stmt := "UPDATE " + quoteIdent(be, table) + " SET " + quoteCol(be, colName) + "=" + placeholder(be, 1) +
		" WHERE " + strings.Join(where, " AND ")
	return stmt, args, nil
}

// buildDeleteBound returns a parameter-bound DELETE keyed on the primary
// key, mirroring buildDelete's shapes: single-PK tables get a "WHERE pk IN
// (?,?,...)", multi-PK tables a "WHERE (pk=? AND pk=?) OR (...)".
func buildDeleteBound(be db.Backend, table string, pks []string, frame *data.Frame, rows []int) (string, []any, error) {
	if len(pks) == 0 {
		return "", nil, fmt.Errorf("no primary key")
	}
	pkIdx := make([]int, len(pks))
	for i, pk := range pks {
		idx := frame.ColumnIndex(pk)
		if idx < 0 {
			return "", nil, fmt.Errorf("primary key column not found: %s", pk)
		}
		pkIdx[i] = idx
	}
	tbl := quoteIdent(be, table)
	var args []any
	if len(pks) == 1 {
		marks := make([]string, 0, len(rows))
		for _, r := range rows {
			args = append(args, frame.Cell(r, pkIdx[0]))
			marks = append(marks, placeholder(be, len(args)))
		}
		stmt := "DELETE FROM " + tbl + " WHERE " + quoteCol(be, pks[0]) + " IN (" + strings.Join(marks, ",") + ")"
		return stmt, args, nil
	}
	ors := make([]string, 0, len(rows))
	for _, r := range rows {
		ands := make([]string, 0, len(pks))
		for i, pk := range pks {
			ands = append(ands, quoteCol(be, pk)+"="+placeholder(be, len(args)+1))
			args = append(args, frame.Cell(r, pkIdx[i]))
		}
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	return "DELETE FROM " + tbl + " WHERE " + strings.Join(ors, " OR "), args, nil
}

// stmtRunner executes built statements on one backend: parameter-bound when
// the backend supports it, inline-literal otherwise.
type stmtRunner struct {
	be    db.Backend
	bound db.BoundRunner // nil when the backend does not bind
}

// runnerFor probes the backend's binding capability once.
func runnerFor(be db.Backend) stmtRunner {
	r := stmtRunner{be: be}
	if br, ok := be.(db.BoundRunner); ok {
		r.bound = br
	}
	return r
}

// binds reports whether the runner takes parameter-bound statements.
func (r stmtRunner) binds() bool { return r.bound != nil }

// run executes stmt with its bind arguments.
func (r stmtRunner) run(stmt string, args []any) (db.Result, error) {
	if r.bound != nil {
		return r.bound.RunBound(stmt, args)
	}
	return r.be.Run(stmt)
}

// commitCellEdit runs the edit described by pe. In database mode it issues a
// parameter-bound UPDATE (inline literals only for backends that cannot
// bind) and returns a command producing cellSavedMsg; in file mode it
// rewrites the cell locally and returns a command producing ApplyFrameMsg.
func commitCellEdit(ctx AppContext, pe PendingEdit) tea.Cmd {
	be := ctx.Backend()
	if be != nil {
		// PrimaryKeys resolves through a catalog query (30s timeout): running it
		// here means the Update loop blocks on a dead connection every time the
		// user confirms an edit. Everything up to the statement execution moves
		// into the command's closure, on the same goroutine as the UPDATE.
		return func() tea.Msg {
			pks, err := be.PrimaryKeys(pe.Namespace, pe.Table)
			if err != nil {
				return ErrorMsg{Err: err}
			}
			if len(pks) == 0 {
				return ErrorMsg{Err: fmt.Errorf("no primary key, cannot edit")}
			}
			r := runnerFor(be)
			var stmt string
			var args []any
			if r.binds() {
				stmt, args, err = buildUpdateBound(be, pe.Table, pks, pe.Frame, pe.Row, pe.ColName, pe.NewValue)
			} else {
				stmt, err = buildUpdate(be, pe.Table, pks, pe.Frame, pe.Row, pe.ColName, pe.NewValue)
			}
			if err != nil {
				return ErrorMsg{Err: err}
			}
			res, err := r.run(stmt, args)
			if err != nil {
				return cellSavedMsg{err: err}
			}
			n := int64(0)
			if res.Exec != nil {
				n = res.Exec.RowsAffected
			}
			return cellSavedMsg{rows: n}
		}
	}
	// file mode
	typed, err := parseTyped(pe.NewValue, pe.Frame.Columns[pe.Col].Type)
	if err != nil {
		e := err
		return func() tea.Msg { return ErrorMsg{Err: e} }
	}
	newFrame := pe.Frame.WithCell(pe.Row, pe.Col, typed)
	paneID := ctx.ActivePaneID()
	return func() tea.Msg { return ApplyFrameMsg{Frame: newFrame, Crumb: "edit", PaneID: paneID} }
}

// commitRowDelete runs the delete described by pd. In database mode it issues
// a parameter-bound DELETE (inline literals only for backends that cannot
// bind) and returns a command producing rowsDeletedMsg; in file mode it drops
// the rows locally and returns a command producing ApplyFrameMsg.
func commitRowDelete(ctx AppContext, pd PendingDelete) tea.Cmd {
	be := ctx.Backend()
	if be != nil {
		// Same closure discipline as commitCellEdit: the catalog read must not
		// run inside the Update loop.
		return func() tea.Msg {
			pks, err := be.PrimaryKeys(pd.Namespace, pd.Table)
			if err != nil {
				return ErrorMsg{Err: err}
			}
			r := runnerFor(be)
			var stmt string
			var args []any
			if r.binds() {
				stmt, args, err = buildDeleteBound(be, pd.Table, pks, pd.Frame, pd.Rows)
			} else {
				stmt, err = buildDelete(be, pd.Table, pks, pd.Frame, pd.Rows)
			}
			if err != nil {
				return ErrorMsg{Err: err}
			}
			res, err := r.run(stmt, args)
			if err != nil {
				return rowsDeletedMsg{err: err}
			}
			n := int64(0)
			if res.Exec != nil {
				n = res.Exec.RowsAffected
			}
			return rowsDeletedMsg{rows: n}
		}
	}
	// file mode
	newFrame := pd.Frame.WithoutRows(pd.Rows)
	paneID := ctx.ActivePaneID()
	return func() tea.Msg { return ApplyFrameMsg{Frame: newFrame, Crumb: "delete", PaneID: paneID} }
}
