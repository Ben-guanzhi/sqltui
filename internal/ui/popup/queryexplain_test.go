package popup

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/ui"
)

// 波 97：":explain" 编辑器把语句包进引擎的 EXPLAIN 配方再执行。计划永不执行
// 语句，所以文件模式（SQLite 引擎）下 EXPLAIN QUERY PLAN 必须真的跑通，且
// 页签标题是包过的计划语句；方言前缀由内核 sqltext.ExplainQuery 决定。

func TestQxExplainSQLPrefixesSQLite(t *testing.T) {
	ctx := qxFileCtx(t)
	got, err := qxExplainSQL(ctx, "SELECT * FROM _")
	if err != nil {
		t.Fatalf("qxExplainSQL: %v", err)
	}
	if !strings.HasPrefix(got, "EXPLAIN QUERY PLAN ") {
		t.Errorf("prefix = %q, want EXPLAIN QUERY PLAN prefix", got)
	}
}

func TestQxExplainSQLBackendKind(t *testing.T) {
	ctx := qxFileCtx(t)
	ctx.backend = &qxFakeBackend{}
	// qxFakeBackend.Kind() 是定值 "test"，不在内核方言表 → 期望报错而不是
	// 静默按原语句跑（不支持的引擎必须显式失败）。
	if _, err := qxExplainSQL(ctx, "SELECT 1"); err == nil {
		t.Error("unknown dialect must error, not run plain")
	}
}

func TestQxExplainSQLRejectsEmpty(t *testing.T) {
	ctx := qxFileCtx(t)
	if _, err := qxExplainSQL(ctx, ";"); err == nil {
		t.Error("empty statement must error")
	}
}

func TestExplainEditorRunsPlanNotStatement(t *testing.T) {
	ed := qxOpenEditor(t, qxFileCtx(t), "")
	ed.explain = true
	ov, _ := qxTypeText(ed, "SELECT * FROM _")
	ov, _ = qxSend(ov, "esc") // close any suggestion list
	_, cmd := qxSend(ov, "enter")
	msgs := qxDrain(cmd)

	apply, ok := qxFindMsg[ui.ApplyFrameMsg](msgs)
	if !ok {
		t.Fatalf("no ApplyFrameMsg in %v", msgs)
	}
	// 页签标题是包过的计划语句，不是原语句。
	if !strings.HasPrefix(apply.TabTitle, "EXPLAIN") {
		t.Errorf("TabTitle = %q, want EXPLAIN-wrapped title", apply.TabTitle)
	}
	if apply.Frame == nil || apply.Frame.NumRows() == 0 {
		t.Errorf("plan frame empty: %+v", apply.Frame)
	}
}

func TestExplainEditorRegisteredInPalette(t *testing.T) {
	found := false
	for _, c := range Commands {
		if c.Name == "explain" {
			found = true
		}
	}
	if !found {
		t.Error(":explain missing from palette Commands")
	}
	if _, ok := ui.Factories["explain"]; !ok {
		t.Error("Factories['explain'] not registered")
	}
}

var _ = tea.Batch // keep import if assertions change
