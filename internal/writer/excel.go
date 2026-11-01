package writer

// Excel export. excelize is already a dependency (the reader parses
// workbooks with it); the writer had no Excel format at all, so a grid
// could not be saved as the one spreadsheet format every business user
// opens. Cells keep their native types — a number column stays a number in
// Excel, not text — which the string-only writers cannot offer.

import (
	"fmt"
	"io"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/LinPr/sqltui/internal/data"
)

const FormatExcel Format = "xlsx"

func init() {
	Register(FormatExcel, excelWriter{})
}

const excelSheetName = "Sheet1"

type excelWriter struct{}

// excelCellValue maps a frame cell onto what excelize's SetCellValue accepts
// natively; anything exotic falls back to its string form.
func excelCellValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return x
	case bool:
		return x
	case int:
		return x
	case int8:
		return x
	case int16:
		return x
	case int32:
		return x
	case int64:
		return x
	case uint:
		return x
	case uint8:
		return x
	case uint16:
		return x
	case uint32:
		return x
	case uint64:
		return x
	case float32:
		return x
	case float64:
		return x
	case time.Time:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func (excelWriter) Write(w io.Writer, f *data.Frame, opt Options) error {
	nCols := f.NumCols()
	if nCols == 0 {
		return nil
	}
	book := excelize.NewFile()
	defer book.Close()

	if opt.Header {
		for c, col := range f.Columns {
			name, err := excelize.CoordinatesToCellName(c+1, 1)
			if err != nil {
				return err
			}
			if err := book.SetCellValue(excelSheetName, name, col.Name); err != nil {
				return err
			}
		}
	}
	for r := 0; r < f.NumRows(); r++ {
		row := r + 1
		if opt.Header {
			row++
		}
		for c := 0; c < nCols; c++ {
			name, err := excelize.CoordinatesToCellName(c+1, row)
			if err != nil {
				return err
			}
			if err := book.SetCellValue(excelSheetName, name, excelCellValue(f.Cell(r, c))); err != nil {
				return err
			}
		}
	}
	return book.Write(w)
}
