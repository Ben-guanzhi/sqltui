package data

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

// SearchExact returns the indices of rows where at least one cell's display
// string contains needle (case-sensitive). An empty needle matches every row.
func SearchExact(f *Frame, needle string) []int {
	rows := f.NumRows()
	out := make([]int, 0, rows)
	for r := 0; r < rows; r++ {
		for c := range f.Columns {
			if strings.Contains(f.CellString(r, c), needle) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// SearchExactCols returns the indices of rows where at least one of the given
// column cells contains needle (case-insensitive). Use this when you want to
// restrict search to a known subset of columns (e.g. only the visible ones).
func SearchExactCols(f *Frame, cols []int, needle string) []int {
	return SearchExactColsFold(f, cols, needle, nil)
}

// FoldCache 复用"每格折叠成小写"的结果，键＝帧指针 + 行数 + 列数 + 被搜索的列集合。
//
// 搜索框每敲一个可打印键都会把同一帧重扫一遍，而折叠只与帧有关、与 needle 无关：
// 第一键付全价，之后的键跳过 Θ(行×列) 的 ToLower 遍历。中文格尤其贵——ToLower 对
// 非 ASCII 要逐 rune 查 unicode 表（实测约 22 ns/rune；只有折叠出变化时才分配）。
//
// 键里没有每格内容：帧的不可变由类型系统强制（Column.cells 非导出，见 frame.go 顶部
// 注释），包外改不了格值；改一格走 WithCell 换新帧，换页/换结果也都是新帧；append 会
// 改行数（AppendRow 是构建期工具，发布后的帧不再追加）。
type FoldCache struct {
	f      *Frame
	rows   int
	ncols  int
	cols   []int
	vals   [][]string
	builds int // 建了几次；测试用它钉"必须命中"这条性质
}

// foldCacheMaxCells 是缓存的规模上限（格数）。reader 侧没有行数上限，
// 超限的大帧按老路走：不缓存也不会比改前慢。
const foldCacheMaxCells = 200_000

// lookup 键相符时返回缓存的折叠结果；键变了就把旧料放掉（避免大帧滞留）。
func (c *FoldCache) lookup(f *Frame, cols []int) [][]string {
	if c == nil || c.vals == nil {
		return nil
	}
	if c.f != f || c.rows != f.NumRows() || c.ncols != len(f.Columns) || !sameCols(c.cols, cols) {
		*c = FoldCache{builds: c.builds}
		return nil
	}
	return c.vals
}

// build 扫一遍被搜索的列，存下每格的折叠结果。
func (c *FoldCache) build(f *Frame, cols []int) [][]string {
	if c == nil || f == nil {
		return nil
	}
	rows := f.NumRows()
	vals := make([][]string, len(cols))
	for i, col := range cols {
		if col >= len(f.Columns) {
			continue
		}
		v := make([]string, rows)
		for r := 0; r < rows; r++ {
			v[r] = strings.ToLower(f.CellString(r, col))
		}
		vals[i] = v
	}
	*c = FoldCache{f: f, rows: rows, ncols: len(f.Columns), cols: append([]int(nil), cols...), vals: vals, builds: c.builds + 1}
	return vals
}

func sameCols(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SearchExactColsFold 是 SearchExactCols 的带缓存版本：cache 为 nil 或帧过大时
// 逐格现算（＝改前行为）。
func SearchExactColsFold(f *Frame, cols []int, needle string, cache *FoldCache) []int {
	lower := strings.ToLower(needle)
	rows := f.NumRows()
	vals := cache.lookup(f, cols)
	if vals == nil && rows*len(cols) <= foldCacheMaxCells {
		vals = cache.build(f, cols)
	}
	out := make([]int, 0, rows)
	for r := 0; r < rows; r++ {
		for i, c := range cols {
			if c >= len(f.Columns) {
				continue
			}
			var cell string
			if vals != nil {
				cell = vals[i][r]
			} else {
				cell = strings.ToLower(f.CellString(r, c))
			}
			if strings.Contains(cell, lower) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// SearchFuzzy returns the indices of rows whose joined display string fuzzily
// matches needle, in frame order. An empty needle matches every row.
func SearchFuzzy(f *Frame, needle string) []int {
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
	sort.Ints(out) // fuzzy.Find sorts by score; restore frame order
	return out
}
