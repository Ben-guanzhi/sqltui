package postgresbe

import (
	"strings"
	"testing"
)

func TestConfigDsn(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "typical",
			cfg:  Config{UserName: "postgres", Password: "pw", Host: "127.0.0.1", Port: "5432", DbName: "app", SslMode: "disable"},
			want: "postgres://postgres:pw@127.0.0.1:5432/app?sslmode=disable",
		},
		{
			name: "empty sslmode falls back to disable",
			cfg:  Config{UserName: "u", Password: "p", Host: "h", Port: "5433", DbName: "d"},
			want: "postgres://u:p@h:5433/d?sslmode=disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Dsn(); got != tt.want {
				t.Fatalf("Dsn() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCurrentNamespaceFallback(t *testing.T) {
	if got := (&Backend{}).CurrentNamespace(); got != "public" {
		t.Errorf("CurrentNamespace() without connection = %q, want public", got)
	}
	if got := (&Backend{db: &DB{}}).CurrentNamespace(); got != "public" {
		t.Errorf("CurrentNamespace() with dead handle = %q, want public", got)
	}
}

func TestHasReturningClause(t *testing.T) {
	tests := []struct {
		stmt string
		want bool
	}{
		{"insert into t values (1) returning id", true},
		{"UPDATE t SET x = 1 RETURNING *", true},
		{"delete from t where id = 1", false},
	}
	for _, tt := range tests {
		words := strings.Fields(tt.stmt)
		if got := hasReturningClause(words); got != tt.want {
			t.Errorf("hasReturningClause(%q) = %v, want %v", tt.stmt, got, tt.want)
		}
	}
}

func TestPrimaryKeysWithoutConnection(t *testing.T) {
	b := &Backend{db: &DB{}}
	pks, err := b.PrimaryKeys("public", "users")
	if err == nil {
		t.Fatalf("expected error from PrimaryKeys with no connection, got pks=%v", pks)
	}
}

func TestColumnsMetaWithoutConnection(t *testing.T) {
	b := &Backend{db: &DB{}}
	cols, err := b.ColumnsMeta("public", "users")
	if err == nil {
		t.Fatalf("expected error from ColumnsMeta with no connection, got cols=%v", cols)
	}
}

func TestFormatAny(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{nil, ""},
		{"hello", "hello"},
		{int16(42), "42"},
		{int32(100), "100"},
		{int64(9999), "9999"},
		{float64(3.14), "3.14"},
		{true, "true"},
		{false, "false"},
		{[]byte("raw"), "raw"},
	}
	for _, tt := range tests {
		if got := formatAny(tt.input); got != tt.want {
			t.Errorf("formatAny(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
