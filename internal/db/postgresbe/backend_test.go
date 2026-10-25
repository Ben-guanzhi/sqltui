package postgresbe

import (
	"testing"

	"github.com/dbcore/dbcore"
)

func TestConnSpec(t *testing.T) {
	spec := Config{UserName: "postgres", Password: "pw", Host: "127.0.0.1", Port: "5432", DbName: "app", SslMode: "disable"}.ConnSpec()
	if spec.Kind != dbcore.KindPostgres || spec.Host != "127.0.0.1" || spec.Port != 5432 ||
		spec.User != "postgres" || spec.Password != "pw" || spec.Database != "app" || spec.SSLMode != "disable" {
		t.Fatalf("ConnSpec = %+v", spec)
	}
	if spec.ConnectTimeout <= 0 {
		t.Fatal("ConnectTimeout must bound the dial")
	}
}

// An empty sslmode becomes "disable", matching the pre-kernel DSN builder.
func TestConnSpecSslModeDefault(t *testing.T) {
	if got := (Config{}).ConnSpec().SSLMode; got != "disable" {
		t.Fatalf("empty SslMode -> %q, want disable", got)
	}
}

func TestConnSpecPortFallback(t *testing.T) {
	if got := (Config{Port: "abc"}).ConnSpec().Port; got != 0 {
		t.Errorf("unparsable port -> %d, want 0 (engine default 5432)", got)
	}
}

func TestTarget(t *testing.T) {
	if got := Target("public", "orders"); got != `"public"."orders"` {
		t.Errorf("qualified = %s", got)
	}
	if got := Target("", "orders"); got != `"orders"` {
		t.Errorf("bare = %s", got)
	}
	if got := Target("s", `odd"name`); got != `"s"."odd""name"` {
		t.Errorf("escaped = %s", got)
	}
}
