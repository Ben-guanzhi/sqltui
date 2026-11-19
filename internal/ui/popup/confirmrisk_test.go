package popup

// 风险确认测试（§344）：destructive 语句回车时必须先弹确认 overlay 而不是
// 直接执行；确认后经 RunCommandMsg 交回 query 管道。

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func TestConfirmRiskOverlayConfirmRunsViaCommand(t *testing.T) {
	ov := newConfirmRiskOverlay(&qxCtx{}, "DELETE FROM t", nil)
	ov2, cmd := ov.Update(tea.KeyPressMsg{Code: 'y'})
	if ov2 != ov {
		t.Fatal("confirm must stay in the same overlay until closed")
	}
	msgs := confirmDrain(cmd)
	if !confirmHasClose(msgs) {
		t.Fatal("confirm must close the overlay")
	}
	found := false
	for _, m := range msgs {
		if rc, ok := m.(ui.RunCommandMsg); ok && rc.Name == "query" && strings.Contains(rc.Arg, "DELETE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("confirm must hand the statement back via RunCommandMsg, got %T", msgs)
	}
}

func TestConfirmRiskOverlayCancelCloses(t *testing.T) {
	ov := newConfirmRiskOverlay(&qxCtx{}, "DROP TABLE t", nil)
	_, cmd := ov.Update(tea.KeyPressMsg{Code: 'n'})
	msgs := confirmDrain(cmd)
	if !confirmHasClose(msgs) {
		t.Fatal("cancel must close the overlay")
	}
	for _, m := range msgs {
		if rc, ok := m.(ui.RunCommandMsg); ok {
			t.Fatalf("cancel must not run anything, got %v", rc)
		}
	}
}

func TestConfirmRiskViewShowsRiskSummary(t *testing.T) {
	// 无 WHERE 的 DELETE 才是 destructive——带 WHERE 的是 write（这正是
	// 内核分级要区分的）。View 把分级与摘要直呈给用户。
	ov := newConfirmRiskOverlay(&qxCtx{}, "DELETE FROM t", nil)
	out := ov.View(120, 40, confirmTheme())
	if !strings.Contains(out, "destructive") {
		t.Fatalf("view missing the risk level: %q", out)
	}
	if !strings.Contains(out, "没有 WHERE") {
		t.Fatal("view missing the destructive reason")
	}
	if !strings.Contains(out, "y") || !strings.Contains(out, "esc") {
		t.Fatal("view missing the key hints")
	}
}

// confirmTheme 构造测试主题（内置默认主题即可）。
func confirmTheme() *theme.Theme { return theme.Default() }

// 编译期确认 qxCtx 满足 AppContext（复用 queryexec_test 的 stub）。
var _ ui.AppContext = (*qxCtx)(nil)

var _ db.Backend = (*errConfirmBackend)(nil)
