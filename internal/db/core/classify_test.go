package core

import (
	"testing"
)

// classifyCursorType must not classify INTERVAL as int (it contains "INT"),
// and must handle length specifiers and modifier words.
func TestClassifyCursorType(t *testing.T) {
	cases := []struct {
		colType string
		want    cursorClass
	}{
		{"INT", cursorInt},
		{"INT(11)", cursorInt},
		{"BIGINT", cursorInt},
		{"bigint unsigned", cursorInt},
		{"INT8", cursorInt},
		{"INTERVAL", cursorText}, // contains "INT" but is not an int type
		{"INTERVAL DAY TO SECOND", cursorText},
		{"REAL", cursorFloat},
		{"FLOAT", cursorFloat},
		{"DOUBLE PRECISION", cursorFloat}, // Postgres spells float8 this way
		{"BINARY_DOUBLE", cursorFloat},
		{"BINARY_FLOAT", cursorFloat},
		{"NUMBER", cursorFloat}, // Oracle's dominant numeric type
		{"NUMERIC(10,2)", cursorFloat},
		{"DECIMAL(10,2)", cursorFloat},
		{"VARCHAR(255)", cursorText},
		{"DATE", cursorText},
		{"TIMESTAMP", cursorText},
		{"TEXT", cursorText},
		{"BLOB", cursorText},
		{"", cursorText},
	}
	for _, tc := range cases {
		got := classifyCursorType(tc.colType)
		if got != tc.want {
			t.Errorf("classifyCursorType(%q) = %v, want %v", tc.colType, got, tc.want)
		}
	}
}
