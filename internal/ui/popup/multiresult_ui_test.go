package popup

// 多结果 UI（§329）：qxRunCmd 检测到 MultiRunner 后端时走多 tab 路径，
// 每个结果集产生一个 ApplyFrameMsg（tea.Sequence 保证发射顺序）。

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/ui"
)

type qxMultiBackend struct {
	qxFakeBackend
	sets []*dbcore.QueryResult
}

func (b *qxMultiBackend) RunMulti(context.Context, string) ([]*dbcore.QueryResult, error) {
	return b.sets, nil
}

func TestQxRunCmdMultiBackend(t *testing.T) {
	first := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValText, S: "1"}}},
	}
	second := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "name"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValText, S: "Alice"}}},
	}
	be := &qxMultiBackend{sets: []*dbcore.QueryResult{first, second}}
	ctx := &qxCtx{backend: be, base: "test"}

	cmd := qxRunCmd(ctx, "SELECT 1; SELECT 'Alice'", func(f *data.Frame) tea.Msg {
		return ui.ApplyFrameMsg{Frame: f, Crumb: "query"}
	})
	msg := cmd()
	// Multi path returns a tea.Sequence (a batch of tab messages), never an
	// ErrorMsg for a successful run.
	if _, ok := msg.(ui.ErrorMsg); ok {
		t.Fatalf("unexpected error msg: %+v", msg)
	}
	var _ db.Backend = be // compile-time: the fake satisfies the capability
}
