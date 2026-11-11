package writer

import (
	"encoding/csv"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LinPr/sqltui/internal/data"
)

func init() {
	Register(FormatCSV, csvWriter{})
	Register(FormatTSV, csvWriter{tab: true})
}

// csvWriter serializes frames as delimiter-separated values.
type csvWriter struct {
	tab bool // force tab separator (tsv)
}

func (cw csvWriter) Write(w io.Writer, f *data.Frame, opt Options) error {
	sep := opt.Separator
	if sep == 0 {
		sep = ','
	}
	if cw.tab {
		sep = '\t'
	}
	quote := opt.Quote
	if quote == 0 {
		quote = '"'
	}

	// Go 1.27 的 encoding/csv 移除了 Writer.Use 字段（引号字符写死为 '"'），
	// Options.Quote 因此无法再交给标准库落地。默认双引号仍走 encoding/csv，
	// 保证既有导出输出逐字节不变；自定义引号走下面的本地 RFC 4180 序列化。
	if quote == '"' {
		return writeStdCsv(w, f, opt, sep)
	}
	return writeCustomQuoteCsv(w, f, opt, sep, quote)
}

// writeStdCsv 用 encoding/csv 写出默认双引号 CSV/TSV（保持原有实现与
// golden 输出不变）。
func writeStdCsv(w io.Writer, f *data.Frame, opt Options, sep rune) error {
	enc := csv.NewWriter(w)
	enc.Comma = sep

	if opt.Header {
		if err := enc.Write(f.ColumnNames()); err != nil {
			return err
		}
	}
	record := make([]string, f.NumCols())
	for r := 0; r < f.NumRows(); r++ {
		for c := range f.Columns {
			record[c] = data.FormatValue(f.Cell(r, c))
		}
		if err := enc.Write(record); err != nil {
			return err
		}
	}
	enc.Flush()
	return enc.Error()
}

// writeCustomQuoteCsv 在 encoding/csv 无法指定引号字符时，按 RFC 4180 本地
// 序列化：含分隔符/引号/换行或以空白开头的字段加引号，字段内的引号字符
// 双写转义；字段间与行尾分别用分隔符和 '\n' 结束。
func writeCustomQuoteCsv(w io.Writer, f *data.Frame, opt Options, sep, quote rune) error {
	var b strings.Builder
	writeRecord := func(record []string) {
		for i, field := range record {
			if i > 0 {
				b.WriteRune(sep)
			}
			if !customNeedsQuotes(field, sep, quote) {
				b.WriteString(field)
				continue
			}
			b.WriteRune(quote)
			for _, r := range field {
				if r == quote {
					b.WriteRune(quote) // 引号字符双写转义
				}
				b.WriteRune(r)
			}
			b.WriteRune(quote)
		}
		b.WriteByte('\n')
	}
	if opt.Header {
		writeRecord(f.ColumnNames())
	}
	record := make([]string, f.NumCols())
	for r := 0; r < f.NumRows(); r++ {
		for c := range f.Columns {
			record[c] = data.FormatValue(f.Cell(r, c))
		}
		writeRecord(record)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// customNeedsQuotes 判定字段是否需要加引号，语义对齐 encoding/csv 的
// fieldNeedsQuotes：空字段不加；含分隔符/引号/换行或以空白开头的字段加。
func customNeedsQuotes(field string, sep, quote rune) bool {
	if field == "" {
		return false
	}
	if field == `\.` { // 与 encoding/csv 一致，保留 Postgres COPY 终止符语义
		return true
	}
	for _, r := range field {
		if r == '\n' || r == '\r' || r == sep || r == quote {
			return true
		}
	}
	r1, _ := utf8.DecodeRuneInString(field)
	return unicode.IsSpace(r1)
}
