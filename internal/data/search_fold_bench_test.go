package data

// 一次性量尺（本轮 §9 的"①只量"）：给 SearchExactCols 的内层每格工作定价，
// 并现场比较几个候选实现的价格。跑完保留为回归基准，不进产品码。
// 关注三件事：ToLower 到底分不分堆（中文格尤其）、Contains 的占比、 needle 长度的影响。

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// --- 候选实现（只在量尺里，产品码暂不动）---

// baselineInner 与产品码 SearchExactCols 内层逐字同形。
func baselineInner(cell, lower string) bool {
	return strings.Contains(strings.ToLower(cell), lower)
}

// noLowerInner 不是等价实现（大小写敏感），只用于把 ToLower 那一步的价格从 Contains 里剥出来。
func noLowerInner(cell, lower string) bool {
	return strings.Contains(cell, lower)
}

// c1Inner：全 ASCII 且无大写的格跳过 ToLower；非 ASCII 走原路。
func c1Inner(cell, lower string, needleASCII bool) bool {
	allASCII, hasUpper := true, false
	for i := 0; i < len(cell); i++ {
		ch := cell[i]
		if ch >= utf8.RuneSelf {
			allASCII = false
			break
		}
		if ch >= 'A' && ch <= 'Z' {
			hasUpper = true
		}
	}
	if allASCII && !hasUpper {
		return strings.Contains(cell, lower) // ToLower(cell)==cell
	}
	if allASCII && !needleASCII {
		return false // ASCII  haystack 的下串仍全 ASCII，含非 ASCII 的 needle 必不中
	}
	if allASCII {
		return containsFoldASCII(cell, lower)
	}
	return strings.Contains(strings.ToLower(cell), lower)
}

// containsFoldASCII 逐字节折叠匹配：两侧都全 ASCII 时与 Contains(ToLower(s),lower) 等价。
func containsFoldASCII(s, lower string) bool {
	if len(lower) == 0 {
		return true
	}
	for i := 0; i+len(lower) <= len(s); i++ {
		ok := true
		for j := 0; j < len(lower); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != lower[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// --- 合成料 ---

var foldShapes = []struct {
	name string
	cell string
}{
	{"lower", "customer name here"},
	{"upper", "CUSTOMER NAME HERE"},
	{"mixed", "Customer_Name 42"},
	{"digits", "12345678901234"},
	{"cjk", "客户名称测试数据一二三"},
	{"cjkUpper", "客户Name测试数据"},
	{"long400", strings.Repeat("a", 399) + "b"},
	{"empty", ""},
}

func needleFor(shape string) string {
	switch shape {
	case "long400":
		return "zzz" // 不中：付全长扫描的价
	default:
		return "zz" // 不中
	}
}

func foldFrame(rows, cols int, cell string) *Frame {
	f := &Frame{Columns: make([]Column, cols)}
	for c := 0; c < cols; c++ {
		f.Columns[c] = Column{Name: fmt.Sprintf("c%d", c), Type: TypeString, cells: make([]any, rows)}
		for r := 0; r < rows; r++ {
			f.Columns[c].cells[r] = cell
		}
	}
	return f
}

// typedFrame 每列都是 int64：CellString 会走 strconv，用来把"格式化"那一项从"折叠"里剥出来。
func typedFrame(rows, cols int) *Frame {
	f := &Frame{Columns: make([]Column, cols)}
	for c := 0; c < cols; c++ {
		f.Columns[c] = Column{Name: fmt.Sprintf("c%d", c), Type: TypeInt, cells: make([]any, rows)}
		for r := 0; r < rows; r++ {
			f.Columns[c].cells[r] = int64(r*1000 + c)
		}
	}
	return f
}

// caseFreeRunes 逐 rune 证明 ToLower(cell)==cell（只在含非 ASCII 时才需要）。
func caseFreeRunes(s string) bool {
	for _, r := range s {
		if unicode.ToLower(r) != r {
			return false
		}
	}
	return true
}

// 短串阈值：ASCII 含大写时用一遍折叠扫描省掉分配；长串交回 ToLower+Contains，
// 避免朴素匹配在长格上退化成 Θ(n·m)。两侧语义相同，只是价格不同。
const foldScanMax = 128

// c3Inner：全 ASCII 无大写 → 直接 Contains；ASCII 含大写且短 → 折叠一遍扫；
// 非 ASCII 但逐 rune 证明不动它 → 直接 Contains；其余走原表达式。
func c3Inner(cell, lower string) bool {
	allASCII, hasUpper := true, false
	for i := 0; i < len(cell); i++ {
		ch := cell[i]
		if ch >= utf8.RuneSelf {
			allASCII = false
			break
		}
		if ch >= 'A' && ch <= 'Z' {
			hasUpper = true
		}
	}
	switch {
	case !hasUpper && allASCII:
		return strings.Contains(cell, lower)
	case allASCII && len(cell) <= foldScanMax:
		return containsFoldASCII(cell, lower)
	case allASCII:
		return strings.Contains(strings.ToLower(cell), lower)
	case caseFreeRunes(cell):
		return strings.Contains(cell, lower)
	default:
		return strings.Contains(strings.ToLower(cell), lower)
	}
}

// --- 单格价格 ---

func BenchmarkParts(b *testing.B) {
	for _, sh := range foldShapes {
		cell := sh.cell
		b.Run("caseFreeByteScan/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				allASCII := true
				for i := 0; i < len(cell); i++ {
					if cell[i] >= utf8.RuneSelf {
						allASCII = false
						break
					}
				}
				_ = allASCII
			}
		})
		b.Run("caseFreeRunes/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = caseFreeRunes(cell)
			}
		})
	}
}

func BenchmarkPerCell(b *testing.B) {
	for _, sh := range foldShapes {
		lower := strings.ToLower(needleFor(sh.name))
		nASCII := isAllASCIIStr(needleFor(sh.name))
		b.Run("baseline/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = baselineInner(sh.cell, lower)
			}
		})
		b.Run("noLower/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = noLowerInner(sh.cell, lower)
			}
		})
		b.Run("c1/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = c1Inner(sh.cell, lower, nASCII)
			}
		})
		b.Run("c3/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = c3Inner(sh.cell, lower)
			}
		})
		// 对照臂：与 baseline 逐字同码，只是排在 c1/c3 之后。
		// 若它明显快于 baseline，说明"第一个臂偏慢"是测量伪影，不能拿跨臂比值当结论。
		b.Run("baselineRe/"+sh.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = baselineInner(sh.cell, lower)
			}
		})
	}
}

func isAllASCIIStr(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// TestToLowerAllocates 直接回答本轮最关键的一问：哪些形状会让 ToLower 分堆。
func TestToLowerAllocates(t *testing.T) {
	for _, sh := range foldShapes {
		cell := sh.cell
		n := testing.AllocsPerRun(200, func() {
			_ = strings.ToLower(cell)
		})
		same := strings.ToLower(cell) == cell
		t.Logf("shape=%-9s len=%3d allocs/op=%.2f 折叠后不变=%v 字节变=%d→%d",
			sh.name, len(cell), n, same, len(cell), len(strings.ToLower(cell)))
	}
}

// --- 整路径价格：一次按键 = 全帧重扫；连打 K 键才是用户实际付的钱 ---

const foldKeystrokes = 5

func keystrokesFor(shape string) []string {
	switch shape {
	case "cjk", "cjkUpper":
		return []string{"zz", "zzz", "zzzz", "zzzzz", "zzzzzz"}
	case "int64":
		return []string{"999999", "9999999", "99999999", "999999999", "9999999999"}
	default:
		return []string{"zz", "zzy", "zzyy", "zzyyx", "zzyyxw"}
	}
}

func searchColsFuse(f *Frame, cols []int, needle string) []int {
	lower := strings.ToLower(needle)
	nASCII := isAllASCIIStr(lower)
	rows := f.NumRows()
	out := make([]int, 0, rows)
	for r := 0; r < rows; r++ {
		for _, c := range cols {
			if c < len(f.Columns) && c1Inner(f.CellString(r, c), lower, nASCII) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func searchColsC3(f *Frame, cols []int, needle string) []int {
	lower := strings.ToLower(needle)
	rows := f.NumRows()
	out := make([]int, 0, rows)
	for r := 0; r < rows; r++ {
		for _, c := range cols {
			if c < len(f.Columns) && c3Inner(f.CellString(r, c), lower) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// searchExactColsHEAD 是本轮改前 SearchExactCols 的函数体，逐字抄在这里当"改前"臂：
// 产品码的 SearchExactCols 现在是带缓存路径的 wrapper，拿它当基线会把改动前的
// 一部分开销算进基线。
func searchExactColsHEAD(f *Frame, cols []int, needle string) []int {
	lower := strings.ToLower(needle)
	rows := f.NumRows()
	out := make([]int, 0, rows)
	for r := 0; r < rows; r++ {
		for _, c := range cols {
			if c < len(f.Columns) && strings.Contains(strings.ToLower(f.CellString(r, c)), lower) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// 一次迭代 ＝ 一次搜索会话：连打 foldKeystrokes 键。
//
//	head        改前码
//	headRe      与 head 逐字同码，排在最后当对照臂
//	c3/fuse     选型期间被否掉的"每格快路径"候选（tier 1），留在这是为了可复现那个否决价
//	prodNil     改后产品码、cache 传 nil（wrapper 白付的开销必须是 0）
//	prodSession 改后产品码，每会话一个新 FoldCache（1 次建 + 4 次命中）＝用户实际付的钱
//	prodWarm    改后产品码，缓存跨迭代复用（键入又退格的稳态）
func BenchmarkKeystrokeSession(b *testing.B) {
	arms := []string{"head", "c3", "fuse", "prodNil", "prodSession", "prodWarm", "headRe"}
	shapes := []string{"lower", "mixed", "cjk", "int64"}
	for _, rows := range []int{200, 5000} {
		for _, cols := range []int{6, 20} {
			for _, shape := range shapes {
				var f *Frame
				if shape == "int64" {
					f = typedFrame(rows, cols)
				} else {
					cell := ""
					for _, s := range foldShapes {
						if s.name == shape {
							cell = s.cell
						}
					}
					f = foldFrame(rows, cols, cell)
				}
				idx := make([]int, cols)
				for c := range idx {
					idx[c] = c
				}
				ns := keystrokesFor(shape)
				for _, arm := range arms {
					b.Run(fmt.Sprintf("rows=%d/cols=%d/%s/%s", rows, cols, shape, arm), func(b *testing.B) {
						b.ReportAllocs()
						b.ResetTimer()
						var shared FoldCache
						var session FoldCache
						for range b.N {
							session = FoldCache{}
							for k := 0; k < foldKeystrokes; k++ {
								var got []int
								switch arm {
								case "head":
									got = searchExactColsHEAD(f, idx, ns[k])
								case "c3":
									got = searchColsC3(f, idx, ns[k])
								case "fuse":
									got = searchColsFuse(f, idx, ns[k])
								case "headRe":
									got = searchExactColsHEAD(f, idx, ns[k])
								case "prodNil":
									got = SearchExactColsFold(f, idx, ns[k], nil)
								case "prodSession":
									got = SearchExactColsFold(f, idx, ns[k], &session)
								case "prodWarm":
									got = SearchExactColsFold(f, idx, ns[k], &shared)
								}
								if len(got) != 0 {
									b.Fatalf("needle %q 意外命中 %d 行，合成料失效", ns[k], len(got))
								}
							}
						}
						// 量的同时钉住"缓存确实生效"：一次会话连打 foldKeystrokes 键只该建一次。
						// 缓存其实没命中时这里要炸，而不是安静地给出一个"看起来像收益"的数。
						switch arm {
						case "prodSession":
							if session.builds != 1 {
								b.Fatalf("prodSession 末次会话建了 %d 次缓存（应为 1：一次会话＝%d 键只建一次）",
									session.builds, foldKeystrokes)
							}
							if shared.builds != 0 {
								b.Fatalf("prodSession 误用了共享缓存")
							}
						case "prodWarm":
							if shared.builds != 1 {
								b.Fatalf("prodWarm 跨迭代建了 %d 次缓存，没命中", shared.builds)
							}
						}
						b.ReportMetric(float64(foldKeystrokes), "keystroke")
						b.ReportMetric(float64(rows*cols*foldKeystrokes), "cellVisit")
					})
				}
			}
		}
	}
}

// TestFuseMatchesBaseline 是选型期间的等价抽查：融合匹配器与现写法在成料上必须同答案。
func TestFuseMatchesBaseline(t *testing.T) {
	cells := []string{
		"", "abc", "ABC", "aBc-123", "CUSTOMER NAME HERE", "客户Name测试数据", "客户名称测试数据一二三",
		string(rune(0x0130)) + "stanbul", "kelp", string(rune(0x212A)) + "elvin", "K", "KELVIN", "Straße", "ẞ", "µ", "Ωω", "ﬁ", "ǰ", "ﬅ", "mixedCASE中文",
		strings.Repeat("a", 399) + "B", "12345", "true", "2026-01-02",
	}
	needles := []string{"", "a", "A", "ab", "ABC", "zz", "name", "NAME", "客户", "i̇", "k", "ß", "S",
		"str", "Ω", "ﬁ", "ǅ", "中文", "12", "b", "abC",
		string(rune(0x212A)), // 开尔文符号：needle 非 ASCII 但 ToLower 后是 ASCII 'k'——判据必须落在 lower 上
		string(rune(0x0130)), // 带点的 I：ToLower 后变长（'i'+U+0307），ASCII 格与非 ASCII 格两侧都要走
	}
	for _, cell := range cells {
		for _, n := range needles {
			lower := strings.ToLower(n)
			want := strings.Contains(strings.ToLower(cell), lower)
			if got := c1Inner(cell, lower, isAllASCIIStr(lower)); got != want {
				t.Fatalf("c1 与现写法分叉 cell=%q needle=%q 现=%v 候选=%v", cell, n, want, got)
			}
			if got := c3Inner(cell, lower); got != want {
				t.Fatalf("c3 与现写法分叉 cell=%q needle=%q 现=%v 候选=%v", cell, n, want, got)
			}
		}
	}
}

// TestFoldRandomDifferential 固定种子随机几千例差分：候选与现写法必须逐个同答案。
// 码点表刻意覆盖 ASCII 大小写、CJK、会变长的 İ、折叠成 ASCII 的 K、组合符号、4 字节码点。
func TestFoldRandomDifferential(t *testing.T) {
	alphabet := []rune{
		'a', 'b', 'c', 'Z', 'Q', '0', '9', ' ', '-', '_', '.', '/', '\t',
		'客', '户', '名', '称', '测', '试', '中', '文',
		'İ', 'ı', 'ẞ', 'ß', 'K', 'Å', 'å', 'Ω', 'ω', 'ﬁ', 'ǅ', 'ǆ',
		'́', '­', '€', '🙂', 'é', 'É', 'Ⱥ', 'ⱳ',
	}
	rng := rand.New(rand.NewSource(20261007))
	mk := func(maxLen int) string {
		n := rng.Intn(maxLen + 1)
		b := make([]rune, n)
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(b)
	}
	cases := 0
	for iter := 0; iter < 40000; iter++ {
		cell := mk(24)
		needle := mk(4)
		if rng.Intn(4) == 0 { // 四分之一用"格内的真子串"当 needle，逼出命中路径
			if len(cell) > 0 {
				r := []rune(cell)
				i := rng.Intn(len(r))
				j := min(i+1+rng.Intn(3), len(r))
				needle = string(r[i:j])
			}
		}
		lower := strings.ToLower(needle)
		want := strings.Contains(strings.ToLower(cell), lower)
		if got := c1Inner(cell, lower, isAllASCIIStr(lower)); got != want {
			t.Fatalf("c1 分叉 iter=%d cell=%q(U+%X) needle=%q 现=%v 候选=%v",
				iter, cell, []rune(cell), needle, want, got)
		}
		if got := c3Inner(cell, lower); got != want {
			t.Fatalf("c3 分叉 iter=%d cell=%q(U+%X) needle=%q 现=%v 候选=%v",
				iter, cell, []rune(cell), needle, want, got)
		}
		cases++
	}
	t.Logf("差分 %d 例，候选 c1/c3 与现写法零分叉", cases)
}
