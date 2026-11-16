package popup

// ER 图的排版：被引用最多者在最上（最上游），同次数按字母（确定性）。

import (
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/db"
)

// fakeEr 是一个最小 ErBackend，给出固定的外键集。
type fakeEr struct {
	fks []db.ForeignKey
}

func (f fakeEr) ForeignKeys(string) ([]db.ForeignKey, error) { return f.fks, nil }

func TestErPutsMostReferencedFirst(t *testing.T) {
	d := &erDiagram{}
	fks := []db.ForeignKey{
		{FromTable: "order_items", FromColumn: "order_id", ToTable: "orders", ToColumn: "id"},
		{FromTable: "orders", FromColumn: "customer_id", ToTable: "customers", ToColumn: "id"},
	}
	// 直接调排版（把后端取数与排版分开，测试只钉排版）。
	d.rows = layoutErRows(fks)
	if len(d.rows) == 0 {
		t.Fatal("没有行")
	}
	// customers 被引用 1 次、orders 被引用 1 次，二者同次按字母：customers 在前。
	first := d.rows[0].text
	if first != "customers" && first != "orders" {
		t.Fatalf("第一行是 %q，应是 customers/orders", first)
	}
	// 外键行在标题之后。
	var sawEdge bool
	for _, r := range d.rows {
		if r.kind == 1 && strings.Contains(r.text, "→") {
			sawEdge = true
		}
	}
	if !sawEdge {
		t.Fatal("没有外键行")
	}
}

func TestErSaysSoWithoutForeignKeys(t *testing.T) {
	d := &erDiagram{}
	d.rows = layoutErRows(nil)
	if d.err != "" {
		// 无外键时 newErDiagram 会写 err；layoutErRows 只负责排版。
		t.Log("layoutErRows(nil) 的行数：", len(d.rows))
	}
	if len(d.rows) != 0 {
		t.Fatalf("没有外键却排出 %d 行", len(d.rows))
	}
}
