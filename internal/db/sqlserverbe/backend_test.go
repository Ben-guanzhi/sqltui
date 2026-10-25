package sqlserverbe

import (
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	dbcoremssql "github.com/dbcore/dbcore/mssql"
)

func TestConnSpec(t *testing.T) {
	spec := Config{UserName: "sa", Password: "pw", Host: "127.0.0.1", Port: "1433", DbName: "master"}.ConnSpec()
	if spec.Kind != dbcore.KindMSSQL || spec.Host != "127.0.0.1" || spec.Port != 1433 ||
		spec.User != "sa" || spec.Password != "pw" || spec.Database != "master" {
		t.Fatalf("ConnSpec = %+v", spec)
	}
	if spec.SSLMode != "disable" {
		t.Fatalf("SSLMode = %q, want disable (encrypt=disable parity)", spec.SSLMode)
	}
}

// The kernel DSN must keep encryption off: sqltui historically connected with
// encrypt=disable for servers running older TLS versions.
func TestConnSpecDsnKeepsEncryptionOff(t *testing.T) {
	dsn := dbcoremssql.DSN(Config{UserName: "sa", Password: "p@ss", Host: "127.0.0.1", Port: "1433", DbName: "master"}.ConnSpec())
	if !strings.Contains(dsn, "encrypt=false") {
		t.Fatalf("DSN lost encrypt=false: %s", dsn)
	}
	if !strings.Contains(dsn, "database=master") {
		t.Fatalf("DSN lost database param: %s", dsn)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"valid", Config{Host: "h", Port: "1433", UserName: "sa", DbName: "d"}, ""},
		{"missing host", Config{Port: "1433", UserName: "sa", DbName: "d"}, "host is required"},
		{"missing port", Config{Host: "h", UserName: "sa", DbName: "d"}, "port is required"},
		{"missing user", Config{Host: "h", Port: "1433", DbName: "d"}, "username is required"},
		{"missing db", Config{Host: "h", Port: "1433", UserName: "sa"}, "database name is required"},
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
	if got := Target("sales", "orders"); got != "[sales].dbo.[orders]" {
		t.Errorf("qualified = %s, want [sales].dbo.[orders]", got)
	}
	if got := Target("", "orders"); got != "[orders]" {
		t.Errorf("bare = %s, want [orders]", got)
	}
	if got := Target("s", "od]d"); got != "[s].dbo.[od]]d]" {
		t.Errorf("escaped = %s, want bracket-doubled", got)
	}
}
