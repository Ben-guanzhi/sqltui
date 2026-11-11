package reader

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/LinPr/sqltui/internal/data"
)

func TestFWFAutoDetect(t *testing.T) {
	nf := readOne(t, "data.fwf", noInferOpts(FormatFWF))
	checkGrid(t, nf.Frame,
		[]string{"id", "name", "city"},
		[][]any{
			{"1", "alice", "paris"},
			{"2", "bob", "oslo"},
			{"30", "carola", "lisbon"},
		})
}

func TestFWFAutoDetectNoHeader(t *testing.T) {
	opt := noInferOpts(FormatFWF)
	opt.NoHeader = true
	nf := readOne(t, "data.fwf", opt)
	f := nf.Frame
	want := []string{"column_1", "column_2", "column_3"}
	if got := f.ColumnNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	if f.NumRows() != 4 {
		t.Fatalf("rows = %d, want 4", f.NumRows())
	}
	if got := f.Row(0); !reflect.DeepEqual(got, []any{"id", "name", "city"}) {
		t.Errorf("row 0 = %#v", got)
	}
}

func TestFWFExplicitWidths(t *testing.T) {
	opt := noInferOpts(FormatFWF)
	opt.Widths = []int{2, 2, 2}
	opt.SeparatorLength = 1
	nf := readOne(t, "widths.fwf", opt)
	checkGrid(t, nf.Frame,
		[]string{"aa", "bb", "cc"},
		[][]any{
			{"11", "x2", "33"},
			{"44", "y5", "66"},
		})
}

func TestFWFTypeInference(t *testing.T) {
	opt := DefaultOptions()
	opt.Format = FormatFWF
	nf := readOne(t, "data.fwf", opt)
	if got := nf.Frame.Columns[0].Type; got != data.TypeInt {
		t.Errorf("id column type = %v, want TypeInt", got)
	}
	if got := nf.Frame.Cell(2, 0); got != int64(30) {
		t.Errorf("cell(2,0) = %#v, want int64(30)", got)
	}
}

func TestDetectFwfSpans(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  [][2]int
	}{
		{
			name:  "two columns",
			lines: []string{"ab  cd", "xy  zw"},
			want:  [][2]int{{0, 4}, {4, 6}},
		},
		{
			name:  "empty input",
			lines: nil,
			want:  nil,
		},
		{
			name:  "single column",
			lines: []string{"abc", "de"},
			want:  [][2]int{{0, 3}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectFwfSpans(tt.lines)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("detectFwfSpans(%q) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

// TestFWFCJKByteSpans 钉住定宽表自动检测在 CJK（多字节 UTF-8）下的行为：
// detectFwfSpans 必须用字节偏移，fwfFields 才能按同一坐标系切片。若检测改用
// []rune，spans 存的 rune 偏移会被当字节偏移切进多字节字符中间，产出残缺
// UTF-8 而不报错（静默数据损坏）。
func TestFWFCJKByteSpans(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cjk.fwf")
	content := "名称  数量\n苹果  10\n梨子  20\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := FromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := (fwfReader{}).Read(src, noInferOpts(FormatFWF))
	if err != nil {
		t.Fatal(err)
	}
	f := frames[0].Frame
	checkGrid(t, f,
		[]string{"名称", "数量"},
		[][]any{
			{"苹果", "10"},
			{"梨子", "20"},
		})

	// 每个字段都必须是合法 UTF-8：rune 偏移被当字节偏移切分时会留下残缺序列。
	for r := 0; r < f.NumRows(); r++ {
		for c := 0; c < f.NumCols(); c++ {
			if s, ok := f.Cell(r, c).(string); ok && !utf8.ValidString(s) {
				t.Errorf("cell(%d,%d) = %q 不是合法 UTF-8", r, c, s)
			}
		}
	}
}
