package mysqlbe

import (
	"testing"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/go-sql-driver/mysql"
)

func TestConfigDsn(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "typical",
			cfg:  Config{UserName: "root", Password: "secret", Host: "127.0.0.1", Port: "3306", DbName: "shop"},
			want: "root:secret@tcp(127.0.0.1:3306)/shop?charset=utf8&parseTime=true",
		},
		{
			name: "empty password",
			cfg:  Config{UserName: "u", Password: "", Host: "db.local", Port: "3307", DbName: "d"},
			want: "u@tcp(db.local:3307)/d?charset=utf8&parseTime=true",
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

// TestConfigDsnRoundTrip pins the property that matters: the DSN must be
// parseable by the driver with every field intact. Regression guard for the
// old fmt.Sprintf concatenation, which broke on passwords containing
// @ : / ( ) and let a '?' in the db name inject DSN params.
func TestConfigDsnRoundTrip(t *testing.T) {
	cases := []Config{
		{UserName: "root", Password: "secret", Host: "127.0.0.1", Port: "3306", DbName: "shop"},
		{UserName: "u@ser", Password: "p@ss:wo/rd(1)", Host: "db.local", Port: "3307", DbName: "d?loc=1"},
		{UserName: "u", Password: `p\ss`, Host: "[::1]", Port: "3307", DbName: "d"},
	}
	for _, c := range cases {
		got, err := mysql.ParseDSN(c.Dsn())
		if err != nil {
			t.Fatalf("ParseDSN(%q): %v", c.Dsn(), err)
		}
		if got.User != c.UserName || got.Passwd != c.Password {
			t.Errorf("credentials round-trip: user = %q passwd = %q, want %q / %q", got.User, got.Passwd, c.UserName, c.Password)
		}
		if got.Addr != c.Host+":"+c.Port {
			t.Errorf("addr round-trip: %q, want %q", got.Addr, c.Host+":"+c.Port)
		}
		if got.DBName != c.DbName {
			t.Errorf("dbname round-trip: %q, want %q", got.DBName, c.DbName)
		}
		// '?' in the db name must be encoded, not injected as a DSN param.
		if _, injected := got.Params["loc"]; injected {
			t.Errorf("dbname %q injected DSN params", c.DbName)
		}
	}
}

func TestCurrentNamespace(t *testing.T) {
	b := &Backend{db: &DB{dbName: "shop"}}
	if got := b.CurrentNamespace(); got != "shop" {
		t.Errorf("CurrentNamespace() = %q, want shop", got)
	}
	if got := (&Backend{}).CurrentNamespace(); got != "" {
		t.Errorf("CurrentNamespace() without connection = %q, want empty", got)
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"users", "`users`"},
		{"odd`name", "`odd``name`"},
	}
	for _, tt := range tests {
		if got := db.QuoteIdentBacktick(tt.in); got != tt.want {
			t.Errorf("QuoteIdentBacktick(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHasReturningClause(t *testing.T) {
	tests := []struct {
		stmt string
		want bool
	}{
		{"INSERT INTO t (a) VALUES (1) RETURNING a", true},
		{"delete from t where a = 1 returning *", true},
		{"UPDATE t SET a = 2", false},
		{"INSERT INTO returning_log (a) VALUES (1)", false},
		// Words inside literals, quoted identifiers, and comments are not
		// clauses: none of those statements produces a result set.
		{"UPDATE t SET note = 'returning'", false},
		{"UPDATE t SET note = 'no returning here'", false},
		{"UPDATE t SET \"returning\" = 1", false},
		{"UPDATE `t` SET `returning` = 1", false},
		{"UPDATE t SET [returning] = 1", false},
		{"UPDATE t SET a = 1 -- returning", false},
		{"UPDATE t SET a = 1 /* returning */", false},
		// Escaped quotes do not terminate the literal early.
		{"UPDATE t SET a = 'it''s returning'", false},
		{"UPDATE t SET a = 'it\\'s returning'", false},
		// Matching resumes after a comment.
		{"UPDATE t SET a = 1 -- returning\nDELETE FROM t RETURNING *", true},
		// Whole words only, never substrings.
		{"UPDATE t SET returning_flag = 1", false},
		{"UPDATE t SET a = 1 RETURNINGX", false},
	}
	for _, tt := range tests {
		if got := db.HasReturningClause(tt.stmt); got != tt.want {
			t.Errorf("HasReturningClause(%q) = %v, want %v", tt.stmt, got, tt.want)
		}
	}
}

// TestPrimaryKeysWithoutConnection exercises the method on a Backend whose
// underlying handle is nil: it must return an error rather than panic.
func TestPrimaryKeysWithoutConnection(t *testing.T) {
	b := &Backend{db: &DB{}}
	pks, err := b.PrimaryKeys("shop", "users")
	if err == nil {
		t.Fatalf("expected error from PrimaryKeys with no connection, got pks=%v", pks)
	}
}

// TestColumnsMetaWithoutConnection exercises the method on a Backend whose
// underlying handle is nil: it must return an error rather than panic.
func TestColumnsMetaWithoutConnection(t *testing.T) {
	b := &Backend{db: &DB{}}
	cols, err := b.ColumnsMeta("shop", "users")
	if err == nil {
		t.Fatalf("expected error from ColumnsMeta with no connection, got cols=%v", cols)
	}
}

func TestTableTarget(t *testing.T) {
	b := &Backend{}
	if got := b.TableTarget("shop", "orders"); got != "`shop`.`orders`" {
		t.Errorf("qualified = %s, want '`shop'`.`orders`", got)
	}
	if got := b.TableTarget("", "orders"); got != "`orders`" {
		t.Errorf("bare = %s, want '`orders`'", got)
	}
	if got := b.TableTarget("shop", "we`ird"); got != "`shop`.`we``ird`" {
		t.Errorf("escaped = %s, want backtick-doubled", got)
	}
}
