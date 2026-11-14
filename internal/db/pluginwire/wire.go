// Package pluginwire is the newline-delimited JSON wire between sqltui and a
// database plugin process (波 100 对齐 dbstudio 的进程插件化).
//
// A plugin is one connection: the process is spawned per dial and dies with
// Close. The host implements db.Backend by translating each method into one
// Request; the plugin answers with a Response. Unlike dbcore's tagged cells,
// sqltui's data.Frame holds []any cells whose concrete types (int64, float64,
// bool, time.Time, nil) the TUI's rendering depends on — so each cell carries
// an explicit type tag. JSON alone would collapse them all into float64/string.

package pluginwire

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// ProtocolName / ProtocolVersion identify the wire in the banner. A stale
// binary fails at startup, not at the first query.
const (
	ProtocolName    = "sqltui-plugin"
	ProtocolVersion = 1
)

// Banner is the first line a plugin writes.
type Banner struct {
	Proto   string `json:"proto"`
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
}

// Request is one host→plugin call.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one plugin→host reply.
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Method names.
const (
	MethodKind             = "kind"
	MethodTitle            = "title"
	MethodRun              = "run"
	MethodNamespaces       = "namespaces"
	MethodTables           = "tables"
	MethodFetchTable       = "fetchTable"
	MethodPrimaryKeys      = "primaryKeys"
	MethodColumnsMeta      = "columnsMeta"
	MethodColumnIndexTypes = "columnIndexTypes"
	MethodClose            = "close"
)

// ---- parameter payloads ----

type StmtArgs struct {
	Stmt string `json:"stmt"`
}

type NSArgs struct {
	Namespace string `json:"namespace"`
}

type TableArgs struct {
	Namespace string `json:"namespace"`
	Table     string `json:"table"`
}

type FetchArgs struct {
	Namespace string `json:"namespace"`
	Table     string `json:"table"`
	Limit     int    `json:"limit"`
}

// ---- result payloads ----

// Cell is one tagged value. T: 0=null 1=string 2=int64 3=float64 4=bool
// 5=time (S holds RFC3339Nano).
type Cell struct {
	T byte    `json:"t"`
	S string  `json:"s,omitempty"`
	I int64   `json:"i,omitempty"`
	F float64 `json:"f,omitempty"`
	B bool    `json:"b,omitempty"`
}

// Column is one frame column: its name, DType and tagged cells.
type Column struct {
	Name  string `json:"name"`
	Type  uint8  `json:"type"`
	Cells []Cell `json:"cells"`
}

// Frame is data.Frame on the wire (column-oriented, matching the TUI).
type Frame struct {
	Columns []Column `json:"columns"`
}

// Exec is db.ExecResult on the wire.
type Exec struct {
	RowsAffected  int64 `json:"rowsAffected"`
	LastInsertID  int64 `json:"lastInsertId"`
	HasLastInsert bool  `json:"hasLastInsert"`
}

// Result is db.Result on the wire.
type Result struct {
	Frame     *Frame `json:"frame,omitempty"`
	Exec      *Exec  `json:"exec,omitempty"`
	Truncated bool   `json:"truncated"`
}

// ---- encoding ----

// EncodeResult turns a db.Result into its wire form.
func EncodeResult(r db.Result) Result {
	out := Result{Truncated: r.Truncated}
	if r.Exec != nil {
		out.Exec = &Exec{
			RowsAffected:  r.Exec.RowsAffected,
			LastInsertID:  r.Exec.LastInsertID,
			HasLastInsert: r.Exec.HasLastInsert,
		}
	}
	if r.Frame != nil {
		f := &Frame{Columns: make([]Column, len(r.Frame.Columns))}
		for i, c := range r.Frame.Columns {
			wc := Column{Name: c.Name, Type: uint8(c.Type)}
			vals := c.Values()
			wc.Cells = make([]Cell, len(vals))
			for j, v := range vals {
				wc.Cells[j] = EncodeCell(v)
			}
			f.Columns[i] = wc
		}
		out.Frame = f
	}
	return out
}

// EncodeCell tags one cell value so a round trip keeps its concrete type.
func EncodeCell(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{T: 0}
	case string:
		return Cell{T: 1, S: x}
	case int64:
		return Cell{T: 2, I: x}
	case int:
		return Cell{T: 2, I: int64(x)}
	case float64:
		return Cell{T: 3, F: x}
	case float32:
		return Cell{T: 3, F: float64(x)}
	case bool:
		return Cell{T: 4, B: x}
	case time.Time:
		return Cell{T: 5, S: x.UTC().Format(time.RFC3339Nano)}
	default:
		// Unknown concrete types degrade to their string form rather than
		// failing the whole result — the grid renders text fine.
		return Cell{T: 1, S: fmt.Sprint(x)}
	}
}

// DecodeCell rebuilds one cell value from its tag.
func DecodeCell(c Cell) (any, error) {
	switch c.T {
	case 0:
		return nil, nil
	case 1:
		return c.S, nil
	case 2:
		return c.I, nil
	case 3:
		return c.F, nil
	case 4:
		return c.B, nil
	case 5:
		t, err := time.Parse(time.RFC3339Nano, c.S)
		if err != nil {
			return nil, fmt.Errorf("decode time cell %q: %w", c.S, err)
		}
		return t, nil
	}
	return nil, fmt.Errorf("unknown cell tag %d", c.T)
}

// DecodeResult rebuilds a db.Result from its wire form.
func DecodeResult(r Result) (db.Result, error) {
	out := db.Result{Truncated: r.Truncated}
	if r.Exec != nil {
		out.Exec = &db.ExecResult{
			RowsAffected:  r.Exec.RowsAffected,
			LastInsertID:  r.Exec.LastInsertID,
			HasLastInsert: r.Exec.HasLastInsert,
		}
	}
	if r.Frame != nil {
		f := &data.Frame{Columns: make([]data.Column, len(r.Frame.Columns))}
		for i, c := range r.Frame.Columns {
			vals := make([]any, len(c.Cells))
			for j, cell := range c.Cells {
				v, err := DecodeCell(cell)
				if err != nil {
					return db.Result{}, err
				}
				vals[j] = v
			}
			f.Columns[i] = data.NewColumn(c.Name, data.DType(c.Type), vals)
		}
		out.Frame = f
	}
	return out, nil
}

// EncodeFrame / DecodeFrame carry a bare *data.Frame (FetchTable).
func EncodeFrame(f *data.Frame) *Frame {
	if f == nil {
		return nil
	}
	out := &Frame{Columns: make([]Column, len(f.Columns))}
	for i, c := range f.Columns {
		wc := Column{Name: c.Name, Type: uint8(c.Type)}
		vals := c.Values()
		wc.Cells = make([]Cell, len(vals))
		for j, v := range vals {
			wc.Cells[j] = EncodeCell(v)
		}
		out.Columns[i] = wc
	}
	return out
}

// DecodeFrame rebuilds a *data.Frame from its wire form.
func DecodeFrame(f *Frame) (*data.Frame, error) {
	if f == nil {
		return nil, nil
	}
	out := &data.Frame{Columns: make([]data.Column, len(f.Columns))}
	for i, c := range f.Columns {
		vals := make([]any, len(c.Cells))
		for j, cell := range c.Cells {
			v, err := DecodeCell(cell)
			if err != nil {
				return nil, err
			}
			vals[j] = v
		}
		out.Columns[i] = data.NewColumn(c.Name, data.DType(c.Type), vals)
	}
	return out, nil
}
