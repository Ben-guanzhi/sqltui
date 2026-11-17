package writer

// CSV 公式注入防护（OWASP CSV Injection）：以 = + - @ 开头的单元格写进 CSV，
// 用户用 Excel 打开会当公式执行。必须前缀单引号；纯数字字面量豁免。

import (
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
)

func TestFormulaGuard(t *testing.T) {
	cases := []struct{ in, want string }{
		{"=cmd|' /C calc'!A0", "'=cmd|' /C calc'!A0"},
		{"+cmd", "'+cmd"},
		{"-cmd", "'-cmd"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"-5", "-5"},
		{"+3.14", "+3.14"},
		{"hello", "hello"},
		{"", ""},
	}
	for _, c := range cases {
		if got := formulaGuard(c.in); got != c.want {
			t.Errorf("formulaGuard(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 端到端：CSV 输出里危险值带单引号，纯数字不被误伤。
func TestCSVWriteGuardsFormulaInjection(t *testing.T) {
	f := data.New("a", "b")
	f.AppendRow([]any{"=1+1", "-5"})
	var sb strings.Builder
	w := csvWriter{}
	if err := w.Write(&sb, f, DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, "'=1+1") {
		t.Errorf("危险公式没有被转义:\n%s", out)
	}
	if !strings.Contains(out, "-5") || strings.Contains(out, "'-5") {
		t.Errorf("纯数字被误转义了:\n%s", out)
	}
}
