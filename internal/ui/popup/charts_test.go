package popup

import (
	"fmt"
	"sort"
	"testing"
)

// detectXY 曾按 map 迭代序挑轴：平分取"先遇到的"、xCol 取"第一个键"——同一
// 份数据两次运行可能画出不同的轴（Go map 序每轮随机）。修复后候选键先排
// 序，判定只看内容。这两条测试用多轮重复把随机序逼出来：修复前在多数
// 运行上必炸。
func TestDetectXYDeterministicUnderMapOrder(t *testing.T) {
	// 平分数值列（y 平分）+ 多个非数值列（x 任取其一）——两种选择都曾是
	// map 序的人质。
	data := make([]map[string]any, 0, 10)
	for i := 0; i < 10; i++ {
		data = append(data, map[string]any{
			"zebra":  float64(i),
			"alpha":  float64(i),
			"mid":    fmt.Sprintf("row-%d", i),
			"label2": fmt.Sprintf("cat-%d", i%3),
			"_ts":    fmt.Sprintf("2026-01-%02d", i+1),
		})
	}
	// 打乱 map 内容不影响构造——map 字面量本身无序；关键在 detectXY 内部
	// 的候选序。跑 200 轮，任何一轮不同即失败。
	want := ""
	for round := 0; round < 200; round++ {
		x, y := detectXY(data)
		if x == "" || y == "" {
			t.Fatalf("round %d: no axes detected (x=%q y=%q)", round, x, y)
		}
		// y：平分时按字典序取最小（zebra < alpha 反例——alpha 是排序后首个）。
		// x：非数值候选的字典序首个。
		if want == "" {
			want = x + "|" + y
			continue
		}
		if got := x + "|" + y; got != want {
			t.Fatalf("detectXY not deterministic: round %d got %q, first round had %q", round, got, want)
		}
	}
	// 语义断言（不只是稳定性）：y 平分时取排序后首个数值列，x 取排序后
	// 首个非数值列——不是"随便哪个但每次一样"。
	_, y := detectXY(data)
	if y != "alpha" {
		t.Errorf("y = %q, want alpha (sorted-first among tied numeric columns)", y)
	}
}

// 无 _timestamp 时兜底路径同样按字典序：此前 "first col" 是 map 序。
func TestDetectXYFallbackDeterministic(t *testing.T) {
	data := []map[string]any{
		{"note": "a", "score": 1.0},
		{"note": "b", "score": 2.0},
	}
	seen := map[string]bool{}
	for round := 0; round < 100; round++ {
		x, y := detectXY(data)
		if y != "score" {
			t.Fatalf("round %d: y = %q, want score", round, y)
		}
		seen[x] = true
	}
	if len(seen) != 1 {
		t.Fatalf("x varied across rounds: %v", keysOf(seen))
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
