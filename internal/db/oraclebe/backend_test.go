package oraclebe

import (
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	dbcoreoracle "github.com/dbcore/dbcore/oracle"
)

func TestConnSpec(t *testing.T) {
	spec := Config{UserName: "system", Password: "password123", Host: "localhost", Port: "1521", DbName: "xe"}.ConnSpec()
	if spec.Kind != dbcore.KindOracle || spec.Host != "localhost" || spec.Port != 1521 ||
		spec.User != "system" || spec.Password != "password123" || spec.Database != "xe" {
		t.Fatalf("ConnSpec = %+v", spec)
	}
	if spec.ConnectTimeout <= 0 {
		t.Fatal("ConnectTimeout must bound the dial")
	}
}

// Special characters in the password must survive the kernel DSN (net/url
// escapes them; the old hand-rolled DSN used QueryEscape the same way).
func TestConnSpecDsnEscapesCredentials(t *testing.T) {
	spec := Config{UserName: "system", Password: "p@ss:word/123", Host: "localhost", Port: "1521", DbName: "test/service"}.ConnSpec()
	dsn := dbcoreoracle.DSN(spec)
	if !strings.Contains(dsn, "p%40ss%3Aword%2F123") {
		t.Fatalf("password not escaped in DSN: %s", dsn)
	}
	if !strings.Contains(dsn, "@localhost:1521/") {
		t.Fatalf("unexpected host spelling in DSN: %s", dsn)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"valid", Config{Host: "localhost", Port: "1521", UserName: "system", DbName: "xe"}, ""},
		{"missing host", Config{Port: "1521", UserName: "system", DbName: "xe"}, "host is required"},
		{"missing port", Config{Host: "localhost", UserName: "system", DbName: "xe"}, "port is required"},
		{"missing user", Config{Host: "localhost", Port: "1521", DbName: "xe"}, "username is required"},
		{"missing db", Config{Host: "localhost", Port: "1521", UserName: "system"}, "database name is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTarget(t *testing.T) {
	if got := Target("HR", "EMPLOYEES"); got != `"HR"."EMPLOYEES"` {
		t.Errorf("qualified = %s", got)
	}
	if got := Target("", "EMPLOYEES"); got != `"EMPLOYEES"` {
		t.Errorf("bare = %s", got)
	}
	if got := Target("HR", `odd"name`); got != `"HR"."odd""name"` {
		t.Errorf("escaped = %s", got)
	}
}
