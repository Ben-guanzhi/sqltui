package oraclebe

import (
	"testing"
)

func TestConfigDsn(t *testing.T) {
	cfg := Config{
		UserName: "system",
		Password: "password123",
		Host:     "localhost",
		Port:     "1521",
		DbName:   "xe",
	}
	dsn := cfg.Dsn()
	expected := "oracle://system:password123@localhost:1521/xe"
	if dsn != expected {
		t.Errorf("Dsn() = %q, want %q", dsn, expected)
	}
}

func TestConfigDsnWithSpecialChars(t *testing.T) {
	cfg := Config{
		UserName: "system",
		Password: "p@ss:word/123",
		Host:     "localhost",
		Port:     "1521",
		DbName:   "test/service",
	}
	dsn := cfg.Dsn()
	// Password and DbName should be URL encoded
	expected := "oracle://system:p%40ss%3Aword%2F123@localhost:1521/test%2Fservice"
	if dsn != expected {
		t.Errorf("Dsn() = %q, want %q", dsn, expected)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				UserName: "system",
				Password: "password123",
				Host:     "localhost",
				Port:     "1521",
				DbName:   "xe",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: Config{
				UserName: "system",
				Password: "password123",
				Port:     "1521",
				DbName:   "xe",
			},
			wantErr: true,
		},
		{
			name: "missing port",
			config: Config{
				UserName: "system",
				Password: "password123",
				Host:     "localhost",
				DbName:   "xe",
			},
			wantErr: true,
		},
		{
			name: "missing username",
			config: Config{
				Password: "password123",
				Host:     "localhost",
				Port:     "1521",
				DbName:   "xe",
			},
			wantErr: true,
		},
		{
			name: "missing database name",
			config: Config{
				UserName: "system",
				Password: "password123",
				Host:     "localhost",
				Port:     "1521",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"users", "\"users\""},
		{"my table", "\"my table\""},
		{"col\"name", "\"col\"\"name\""},
		{"SCHEMA.TABLE", "\"SCHEMA.TABLE\""},
	}
	for _, tt := range tests {
		got := quoteIdent(tt.input)
		if got != tt.expected {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIndexPriority(t *testing.T) {
	tests := []struct {
		label    string
		expected int
	}{
		{"PK", 3},
		{"UNIQUE", 2},
		{"INDEX", 1},
		{"UNKNOWN", 0},
	}
	for _, tt := range tests {
		got := indexPriority(tt.label)
		if got != tt.expected {
			t.Errorf("indexPriority(%q) = %d, want %d", tt.label, got, tt.expected)
		}
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{nil, "NULL"},
		{[]byte("hello"), "hello"},
		{"test", "test"},
		{int64(42), "42"},
		{float64(3.14), "3.14"},
		{true, "true"},
	}
	for _, tt := range tests {
		got := formatValue(tt.input)
		if got != tt.expected {
			t.Errorf("formatValue(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBackendKind(t *testing.T) {
	b := &Backend{host: "localhost", db: &DB{dbName: "testdb"}}
	if got := b.Kind(); got != "oracle" {
		t.Errorf("Kind() = %q, want %q", got, "oracle")
	}
}

func TestBackendTitle(t *testing.T) {
	b := &Backend{host: "localhost", db: &DB{dbName: "testdb"}}
	expected := "oracle://localhost/testdb"
	if got := b.Title(); got != expected {
		t.Errorf("Title() = %q, want %q", got, expected)
	}
}

func TestBackendCurrentNamespace(t *testing.T) {
	b := &Backend{db: &DB{dbName: "testdb"}}
	if got := b.CurrentNamespace(); got != "testdb" {
		t.Errorf("CurrentNamespace() = %q, want %q", got, "testdb")
	}
	
	b2 := &Backend{db: nil}
	if got := b2.CurrentNamespace(); got != "" {
		t.Errorf("CurrentNamespace() with nil db = %q, want empty", got)
	}
}
