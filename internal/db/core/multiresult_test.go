package core

// 多结果集能力（§328）：Backend.RunMulti 优先走内核 MultiResultReader，
// 无该能力的驱动回退单结果（"引擎只能返回一个集"是真实情况不是降级）。

import (
	"context"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	dbcoresqlite "github.com/Ben-guanzhi/dbcore/sqlite"
)

func TestRunMultiSingleResultFallback(t *testing.T) {
	drv, err := dbcoresqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close()
	b := New(drv, Options{Kind: "sqlite"})
	sets, err := b.RunMulti(context.Background(), "SELECT 1 AS id, 'Alice' AS name")
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || len(sets[0].Columns) != 2 || len(sets[0].Rows) != 1 {
		t.Fatalf("single result wrong: %+v", sets)
	}
	if sets[0].Rows[0][1].String() != "Alice" {
		t.Fatalf("cell wrong: %+v", sets[0].Rows)
	}
}

// multiFake 是一个声明多结果能力的驱动：只覆盖 ExecuteMulti，其余方法
// 由嵌入的 nil SQLDriver 提供（本测试只走多集分支，不会碰到它们）。
type multiFake struct {
	dbcore.SQLDriver
	sets []*dbcore.QueryResult
	err  error
}

func (m *multiFake) ExecuteMulti(context.Context, string, ...dbcore.Value) ([]*dbcore.QueryResult, error) {
	return m.sets, m.err
}

func TestRunMultiCapableDriver(t *testing.T) {
	first := &dbcore.QueryResult{Columns: []dbcore.ColumnInfo{{Name: "id"}}}
	second := &dbcore.QueryResult{Columns: []dbcore.ColumnInfo{{Name: "name"}}}
	b := New(&multiFake{sets: []*dbcore.QueryResult{first, second}}, Options{Kind: "sqlserver"})
	sets, err := b.RunMulti(context.Background(), "SELECT 1; SELECT 'Alice'")
	if err != nil || len(sets) != 2 {
		t.Fatalf("multi sets wrong: %d sets, err=%v", len(sets), err)
	}
}
