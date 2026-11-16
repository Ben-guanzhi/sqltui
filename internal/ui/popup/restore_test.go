package popup

// 恢复 overlay：确认的两步与语句切分。

import (
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/theme"
)

func TestSplitRestoreSQLTakesSemicolonLines(t *testing.T) {
	text := "-- 注释\nPRAGMA foreign_keys=OFF;\nCREATE TABLE t (id INTEGER);\nINSERT INTO t VALUES (1);\n"
	stmts := splitRestoreSQL(text)
	if len(stmts) != 3 {
		t.Fatalf("切出 %d 条，want 3：%v", len(stmts), stmts)
	}
	if stmts[0] != "PRAGMA foreign_keys=OFF;" || stmts[1] != "CREATE TABLE t (id INTEGER);" {
		t.Fatalf("切分错：%v", stmts)
	}
	if !strings.HasSuffix(stmts[2], "(1);") {
		t.Fatalf("最后一条不对：%q", stmts[2])
	}
	// 空文件 / 只有注释。
	if got := splitRestoreSQL("-- 只有注释\n"); len(got) != 0 {
		t.Fatalf("只有注释却切出 %d 条", len(got))
	}
}

func TestRestoreViewWarnsDestructive(t *testing.T) {
	o := &restoreOverlay{path: "backup.sql", stmts: []string{"A;", "B;", "C;"}}
	view := o.View(80, 30, theme.Default())
	for _, want := range []string{"backup.sql", "3 条语句", "替换现有表", "y 确认"} {
		if !strings.Contains(view, want) {
			t.Errorf("恢复预览没有 %q；\n%s", want, view)
		}
	}
}
