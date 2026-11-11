package reader

import (
	"fmt"
	"math"
	"strings"
)

// fwfReader parses fixed-width text. Column layout comes from Options.Widths
// (plus Options.SeparatorLength gaps) or, when Widths is empty, from
// auto-detected all-space column boundaries.
type fwfReader struct{}

func init() { Register(FormatFWF, fwfReader{}) }

// fwfDetectSample caps how many lines the boundary auto-detection scans.
const fwfDetectSample = 128

func (fwfReader) Read(src *Source, opt Options) ([]NamedFrame, error) {
	b, err := src.Bytes()
	if err != nil {
		return nil, err
	}
	b = stripBOMBytes(b)
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
	}

	var spans [][2]int
	if len(opt.Widths) > 0 {
		pos := 0
		for _, w := range opt.Widths {
			if w <= 0 {
				return nil, fmt.Errorf("fwf: column width must be positive, got %d", w)
			}
			spans = append(spans, [2]int{pos, pos + w})
			pos += w + opt.SeparatorLength
		}
	} else {
		spans = detectFwfSpans(lines)
	}
	if len(spans) == 0 {
		return []NamedFrame{{Name: src.Name(), Frame: applyInfer(buildFrame(nil, nil), opt)}}, nil
	}
	if opt.FlexibleWidth {
		spans[len(spans)-1][1] = math.MaxInt32 // last column absorbs overflow
	}

	var (
		header []string
		rows   [][]any
	)
	if opt.NoHeader {
		header = synthesizeHeader(len(spans))
	} else if len(lines) > 0 {
		fields := fwfFields(lines[0], spans)
		header = make([]string, len(spans))
		for i, v := range fields {
			if s, ok := v.(string); ok && s != "" {
				header[i] = s
			} else {
				header[i] = fmt.Sprintf("column_%d", i+1)
			}
		}
		lines = lines[1:]
	}
	for _, l := range lines {
		rows = append(rows, fwfFields(l, spans))
	}

	frame := buildFrame(header, rows)
	frame = applyInfer(frame, opt)
	return []NamedFrame{{Name: src.Name(), Frame: frame}}, nil
}

// detectFwfSpans finds column boundaries by locating byte positions that are
// spaces in every sampled line; maximal non-space runs become columns.
// Each span is widened up to the start of the next one so slightly wider
// values in unsampled lines still land in the right column.
//
// 全程按 []byte 度量与切分：列边界只在 ASCII 空格处断开，多字节字符（CJK
// 等）的字节都 >= 0x80，绝不会被当成空格，因此 rune 边界天然对齐字节边界。
// 若改用 []rune 计算偏移，spans 存的就是 rune 偏移，而 fwfFields 用它对原始
// 字节切片，切点会落在多字节字符中间，产出残缺 UTF-8（静默数据损坏）。
// detectFwfSpans finds column boundaries by locating byte positions that are
// spaces in every sampled line; maximal non-space runs become columns.
// Each span is widened up to the start of the next one so slightly wider
// values in unsampled lines still land in the right column.
//
// 全程按 []byte 度量与切分：列边界只在 ASCII 空格处断开，多字节字符（CJK
// 等）的字节都 >= 0x80，绝不会被当成空格，因此 rune 边界天然对齐字节边界。
// 若改用 []rune 计算偏移，spans 存的就是 rune 偏移，而 fwfFields 用它对原始
// 字节切片，切点会落在多字节字符中间，产出残缺 UTF-8（静默数据损坏）。
func detectFwfSpans(lines []string) [][2]int {
	sample := lines
	if len(sample) > fwfDetectSample {
		sample = sample[:fwfDetectSample]
	}
	byteLines := make([][]byte, 0, len(sample))
	maxLen := 0
	for _, l := range sample {
		b := []byte(l)
		byteLines = append(byteLines, b)
		if len(b) > maxLen {
			maxLen = len(b)
		}
	}
	if maxLen == 0 {
		return nil
	}
	allSpace := make([]bool, maxLen)
	for p := 0; p < maxLen; p++ {
		allSpace[p] = true
		for _, b := range byteLines {
			if p < len(b) && b[p] != ' ' {
				allSpace[p] = false
				break
			}
		}
	}
	var spans [][2]int
	for p := 0; p < maxLen; {
		if allSpace[p] {
			p++
			continue
		}
		start := p
		for p < maxLen && !allSpace[p] {
			p++
		}
		spans = append(spans, [2]int{start, p})
	}
	// widen each span to the start of the next (the gap is all spaces anyway,
	// and fields are trimmed on extraction)
	for i := 0; i+1 < len(spans); i++ {
		spans[i][1] = spans[i+1][0]
	}
	if len(spans) > 0 {
		spans[len(spans)-1][1] = maxLen
	}
	return spans
}

// fwfFields slices one line into trimmed fields; spans entirely beyond the
// end of the line yield nil. Byte-oriented: spans are byte offsets so we
// slice the string directly (FWF columns are byte-aligned by definition).
func fwfFields(line string, spans [][2]int) []any {
	fields := make([]any, len(spans))
	for i, sp := range spans {
		start, end := sp[0], sp[1]
		if start >= len(line) {
			fields[i] = nil
			continue
		}
		if end > len(line) {
			end = len(line)
		}
		fields[i] = strings.TrimSpace(line[start:end])
	}
	return fields
}
