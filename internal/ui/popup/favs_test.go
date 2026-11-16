package popup

// 收藏列表：空态说清楚、条目带连接前缀、超宽截断。

import (
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func TestFavsEmptyStateSaysHow(t *testing.T) {
	f := &favsList{}
	v := f.View(80, 30, theme.Default())
	if !strings.Contains(v, "还没有收藏") || !strings.Contains(v, ":fav") {
		t.Errorf("空态没有说怎么收藏：\n%s", v)
	}
}

func TestFavsListsItemsNewestFirstWithConn(t *testing.T) {
	f := &favsList{items: []ui.HistoryRec{
		{Stmt: "SELECT 1", Conn: "dev"},
		{Stmt: strings.Repeat("UPDATE big SET x = 1, y = 2, z = 3, ", 6), Conn: "prod"},
	}}
	v := f.View(60, 30, theme.Default())
	if !strings.Contains(v, "[dev] SELECT 1") {
		t.Errorf("条目没有连接前缀：\n%s", v)
	}
	if !strings.Contains(v, "…") {
		t.Errorf("超宽条目没有被截断：\n%s", v)
	}
	if !strings.Contains(v, "2 条") {
		t.Errorf("没有条数：\n%s", v)
	}
}

// 工厂注册的命令名要齐：fav 与 favs（对齐「命令要列进速查」的不变量精神）。
func TestFavCommandsRegistered(t *testing.T) {
	for _, name := range []string{"fav", "favs"} {
		if _, ok := ui.Factories[name]; !ok {
			t.Errorf("命令 %q 没有注册进 Factories", name)
		}
	}
}
