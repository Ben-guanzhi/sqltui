package pluginwire

import (
	"testing"
	"time"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// 波 100 进程插件化的核心契约：data.Frame 的 []any 单元格过线后保持具体
// 类型（JSON 裸传会把 int64/float64/时间全压成 float64/string）。

func TestCellRoundTrip(t *testing.T) {
	when := time.Date(2026, 11, 14, 15, 4, 5, 123456789, time.UTC)
	cases := []any{
		nil, "hello", int64(9223372036854775807), float64(1.5), true, when,
	}
	for _, want := range cases {
		got, err := DecodeCell(EncodeCell(want))
		if err != nil {
			t.Fatalf("round trip of %#v: %v", want, err)
		}
		switch w := want.(type) {
		case time.Time:
			g, ok := got.(time.Time)
			if !ok || !g.Equal(w) {
				t.Errorf("time cell = %#v, want %v", got, w)
			}
		default:
			if got != want {
				t.Errorf("cell = %#v (%T), want %#v (%T)", got, got, want, want)
			}
		}
	}
}

func TestResultRoundTrip(t *testing.T) {
	frame := &data.Frame{Columns: []data.Column{
		data.NewColumn("id", data.TypeInt, []any{int64(1), int64(2)}),
		data.NewColumn("name", data.TypeString, []any{"ann", nil}),
	}}
	in := db.Result{Frame: frame, Truncated: true}
	out, err := DecodeResult(EncodeResult(in))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || out.Frame == nil || out.Frame.NumCols() != 2 {
		t.Fatalf("frame lost: %+v", out)
	}
	if got := out.Frame.Columns[0].Cell(0); got != int64(1) {
		t.Errorf("int cell = %#v", got)
	}
	if got := out.Frame.Columns[1].Cell(1); got != nil {
		t.Errorf("null cell = %#v", got)
	}
}

func TestExecResultRoundTrip(t *testing.T) {
	in := db.Result{Exec: &db.ExecResult{RowsAffected: 7, LastInsertID: 3, HasLastInsert: true}}
	out, err := DecodeResult(EncodeResult(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.Exec == nil || out.Exec.RowsAffected != 7 || out.Exec.LastInsertID != 3 || !out.Exec.HasLastInsert {
		t.Errorf("exec = %+v", out.Exec)
	}
}
