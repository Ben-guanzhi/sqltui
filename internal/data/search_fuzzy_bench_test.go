package data

// SearchFuzzy 的"改前"冻结件 + 差分等价测试 + 回归基准。
//
// 冻结件 searchFuzzyBefore 是产品码改之前的逐字拷贝（fuzzy.Find 全量打分 +
// 末尾排序 + 每键重拼 haystack），留在这里做三件事：
//   1. 差分锁定判据：新实现（只判中/不中、不打分）在成料上必须给出同一个索引集；
//      库升级或 unicode 表变化导致判据漂移时，先炸的是这条测试；
//   2. 缓存性质：一次会话连打多个键只该重拼一次 haystack；
//   3. 回归基准：head＝改前，prodCached／prodNil＝改后。

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/sahilm/fuzzy"
)

// searchFuzzyBefore 是产品码改之前的逐字拷贝，冻结在这里当差分基准。
func searchFuzzyBefore(f *Frame, needle string) []int {
	rows := f.NumRows()
	if needle == "" {
		all := make([]int, rows)
		for i := range all {
			all[i] = i
		}
		return all
	}
	haystack := make([]string, rows)
	parts := make([]string, len(f.Columns))
	for r := 0; r < rows; r++ {
		for c := range f.Columns {
			parts[c] = f.CellString(r, c)
		}
		haystack[r] = strings.Join(parts, " ")
	}
	matches := fuzzy.Find(needle, haystack)
	out := make([]int, len(matches))
	for i, m := range matches {
		out[i] = m.Index
	}
	sort.Ints(out)
	return out
}

// fuzzyHaystack 抽出 haystack 的构造本体，用于比对"缓存拼出的料是否与产品码同料"。
func fuzzyHaystack(f *Frame) []string {
	haystack := make([]string, f.NumRows())
	parts := make([]string, len(f.Columns))
	for r := 0; r < f.NumRows(); r++ {
		for c := range f.Columns {
			parts[c] = f.CellString(r, c)
		}
		haystack[r] = strings.Join(parts, " ")
	}
	return haystack
}

// --- 合成料 ---

func fuzzyShapes() []struct {
	name string
	cell string
} {
	return []struct {
		name string
		cell string
	}{
		{"mixed", "Customer_Name 42"},
		{"cjk", "客户Name测试数据"},
		{"int64", ""},
	}
}

// fuzzyKeystrokesFor 生成一次真实输入会话：needle 逐键增长。
func fuzzyKeystrokesFor(shape string) []string {
	var seed string
	switch shape {
	case "mixed":
		seed = "Customer_Name 42"
	case "cjk":
		seed = "客户Name测试数据"
	default:
		seed = "12345"
	}
	runes := []rune(seed)
	if len(runes) > foldKeystrokes {
		runes = runes[:foldKeystrokes]
	}
	out := make([]string, 0, len(runes))
	for i := 1; i <= len(runes); i++ {
		out = append(out, string(runes[:i]))
	}
	return out
}

// --- 差分等价测试 ---

// 判据必须与改前逐位一致：只省掉打分，不省掉"中/不中"。成料取自 §9 的折叠门
// 成料，含 ß/ẞ/İ/Ω/连写/ǰ 与 NUL 截断，以及数字/日期/布尔的格式化格。
func TestSearchFuzzyFoldMatchesBefore(t *testing.T) {
	cells := []string{
		"", "abc", "ABC", "aBc-123", "CUSTOMER NAME HERE", "客户Name测试数据",
		"客户名称测试数据一二三", string(rune(0x0130)) + "stanbul", string(rune(0x212A)) + "elvin",
		"K", "KELVIN", "Straße", "ẞ", "µ", "Ωω", "ﬁ", "ǰ", "ǆ", "mixedCASE中文",
		strings.Repeat("a", 399) + "B", "12345", "true", "2026-01-02", "ab\x00cd", "nope",
	}
	fc := make([]any, len(cells))
	for i, c := range cells {
		fc[i] = c
	}
	f := frameOf(Column{Name: "w", Type: TypeString, cells: fc})
	needles := []string{
		"", "a", "A", "ab", "ABC", "zz", "name", "NAME", "客户", "k", "K", "ß", "S", "str",
		"Ω", "ﬁ", "ǅ", "中文", "12", "b", "abC", string(rune(0x212A)), string(rune(0x0130)),
		"aB", "abc", "elvi", "cd", "KE", "name42", "客户名", "ab\x00", "cd",
	}
	for _, n := range needles {
		want := searchFuzzyBefore(f, n)
		got := SearchFuzzyFold(f, n, nil)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("needle %q: fold=%v before=%v", n, got, want)
		}
	}
}

// 合成帧上的形状矩阵：空帧、单行、宽帧、类型列都要同答案。
func TestSearchFuzzyFoldMatchesBeforeOverShapes(t *testing.T) {
	for _, rows := range []int{0, 1, 40} {
		for _, cols := range []int{1, 7} {
			for _, sh := range fuzzyShapes() {
				var f *Frame
				if sh.name == "int64" {
					f = typedFrame(rows, cols)
				} else {
					f = foldFrame(rows, cols, sh.cell)
				}
				ns := append([]string{""}, fuzzyKeystrokesFor(sh.name)...)
				for _, n := range ns {
					want := searchFuzzyBefore(f, n)
					got := SearchFuzzyFold(f, n, nil)
					if !reflect.DeepEqual(want, got) {
						t.Errorf("rows=%d cols=%d %s needle %q: fold=%v before=%v",
							rows, cols, sh.name, n, got, want)
					}
				}
			}
		}
	}
}

// 缓存只该复用构造，不该换料。
func TestSearchFuzzyCacheBuildsTheSameHaystack(t *testing.T) {
	for _, rows := range []int{0, 1, 17} {
		for _, cols := range []int{1, 7} {
			for _, sh := range fuzzyShapes() {
				var f *Frame
				if sh.name == "int64" {
					f = typedFrame(rows, cols)
				} else {
					f = foldFrame(rows, cols, sh.cell)
				}
				var c FuzzyCache
				if !reflect.DeepEqual(c.build(f), fuzzyHaystack(f)) {
					t.Fatalf("rows=%d cols=%d %s: 缓存拼出的 haystack 与产品码不同料",
						rows, cols, sh.name)
				}
			}
		}
	}
}

// 缓存跨按键生效：连打 foldKeystrokes 键只该重拼一次。
func TestSearchFuzzyCachesAcrossKeystrokes(t *testing.T) {
	f := foldFrame(200, 6, "Customer_Name 42")
	var c FuzzyCache
	for _, n := range fuzzyKeystrokesFor("mixed") {
		if got, want := SearchFuzzyFold(f, n, &c), searchFuzzyBefore(f, n); !reflect.DeepEqual(got, want) {
			t.Fatalf("needle %q: cache=%v before=%v", n, got, want)
		}
	}
	if c.builds != 1 {
		t.Fatalf("连打 %d 键重拼了 %d 次 haystack，缓存没跨按键生效（应为 1）", foldKeystrokes, c.builds)
	}
}

// 每一维变了都要作废：换帧、改行数、改列数。
func TestSearchFuzzyFoldInvalidatesOnEveryKeyDim(t *testing.T) {
	f := foldFrame(100, 3, "Customer_Name 42")
	var c FuzzyCache
	for range foldKeystrokes {
		SearchFuzzyFold(f, "cus", &c)
	}
	if c.builds != 1 {
		t.Fatalf("同帧连打建了 %d 次（应为 1）", c.builds)
	}
	SearchFuzzyFold(foldFrame(100, 4, "Customer_Name 42"), "cus", &c) // 列数变
	if c.builds != 2 {
		t.Fatalf("列数变后建了 %d 次（应为 2）", c.builds)
	}
	SearchFuzzyFold(foldFrame(101, 4, "Customer_Name 42"), "cus", &c) // 行数变
	if c.builds != 3 {
		t.Fatalf("行数变后建了 %d 次（应为 3）", c.builds)
	}
	SearchFuzzyFold(&Frame{Columns: f.Columns}, "cus", &c) // 换帧指针（列内容不变）
	if c.builds != 4 {
		t.Fatalf("换帧指针后建了 %d 次（应为 4）", c.builds)
	}
}

// 超限的大帧不缓存，但仍要给出正确答案（＝改前行为）。
func TestSearchFuzzyFoldOverCapSkipsCache(t *testing.T) {
	rows := foldCacheMaxCells/7 + 2
	f := foldFrame(rows, 7, "Customer_Name 42")
	if rows*7 <= foldCacheMaxCells {
		t.Fatalf("测试料没有超限：rows=%d", rows)
	}
	var c FuzzyCache
	want := searchFuzzyBefore(f, "cus")
	got := SearchFuzzyFold(f, "cus", &c)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("超限帧：cache=%v before=%v", got, want)
	}
	if c.builds != 0 {
		t.Fatalf("超限帧建了 %d 次缓存（应为 0）", c.builds)
	}
}

// nil 缓存是纯 no-op：答案不变，也不 panic。
func TestSearchFuzzyFoldNilCacheIsNoOp(t *testing.T) {
	f := foldFrame(50, 4, "Customer_Name 42")
	for _, n := range fuzzyKeystrokesFor("mixed") {
		if got, want := SearchFuzzyFold(f, n, nil), searchFuzzyBefore(f, n); !reflect.DeepEqual(got, want) {
			t.Fatalf("needle %q: nil=%v before=%v", n, got, want)
		}
	}
	if got := SearchFuzzy(f, "cus"); !reflect.DeepEqual(got, searchFuzzyBefore(f, "cus")) {
		t.Fatalf("SearchFuzzy: %v vs %v", got, searchFuzzyBefore(f, "cus"))
	}
}

// --- 回归基准 ---

// 一次迭代 ＝ 一次搜索会话：连打 foldKeystrokes 键。
// head＝改前逐键重拼＋打分；prodNil＝改后但不缓存；prodCached＝改后＋会话缓存。
func BenchmarkFuzzyHaystackSession(b *testing.B) {
	for _, rows := range []int{200, 5000} {
		for _, cols := range []int{6, 20} {
			for _, sh := range fuzzyShapes() {
				var f *Frame
				if sh.name == "int64" {
					f = typedFrame(rows, cols)
				} else {
					f = foldFrame(rows, cols, sh.cell)
				}
				ns := fuzzyKeystrokesFor(sh.name)
				for _, arm := range []string{"head", "prodNil", "prodCached"} {
					b.Run(fmt.Sprintf("rows=%d/cols=%d/%s/%s", rows, cols, sh.name, arm), func(b *testing.B) {
						b.ReportAllocs()
						b.ResetTimer()
						var session FuzzyCache
						for range b.N {
							session = FuzzyCache{}
							for k := 0; k < foldKeystrokes; k++ {
								switch arm {
								case "head":
									searchFuzzyBefore(f, ns[k])
								case "prodNil":
									SearchFuzzyFold(f, ns[k], nil)
								case "prodCached":
									SearchFuzzyFold(f, ns[k], &session)
								}
							}
						}
						if arm == "prodCached" && session.builds != 1 {
							b.Fatalf("prodCached 末次会话建了 %d 次 haystack（应为 1：一次会话＝%d 键只拼一次）",
								session.builds, foldKeystrokes)
						}
						b.ReportMetric(float64(foldKeystrokes), "keystroke")
						b.ReportMetric(float64(rows*cols*foldKeystrokes), "cellVisit")
					})
				}
			}
		}
	}
}
