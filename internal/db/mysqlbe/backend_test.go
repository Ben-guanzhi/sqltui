package mysqlbe

import (
	"testing"

	drvmysql "github.com/go-sql-driver/mysql"

	"github.com/dbcore/dbcore"
	dbcoremysql "github.com/dbcore/dbcore/mysql"
)

func TestConnSpec(t *testing.T) {
	spec := Config{UserName: "root", Password: "secret", Host: "127.0.0.1", Port: "3306", DbName: "shop"}.ConnSpec()
	if spec.Kind != dbcore.KindMySQL || spec.Host != "127.0.0.1" || spec.Port != 3306 ||
		spec.User != "root" || spec.Password != "secret" || spec.Database != "shop" {
		t.Fatalf("ConnSpec = %+v", spec)
	}
	if spec.Params["charset"] != "utf8" {
		t.Fatalf("charset param = %q, want utf8", spec.Params["charset"])
	}
	if spec.ConnectTimeout <= 0 {
		t.Fatal("ConnectTimeout must bound the dial")
	}
}

func TestConnSpecPortFallback(t *testing.T) {
	for _, in := range []string{"", "abc", "-1", "70000"} {
		if got := (Config{Port: in}).ConnSpec().Port; got != 0 {
			t.Errorf("port %q -> %d, want 0 (engine default)", in, got)
		}
	}
}

// TestConnSpecDsnRoundTrip pins the property that matters: the kernel DSN
// built from the spec must be parseable by the driver with every field
// intact. Regression guard for the old fmt.Sprintf concatenation, which broke
// on passwords containing @ : / ( ) and let a '?' in the db name inject DSN
// params.
func TestConnSpecDsnRoundTrip(t *testing.T) {
	cases := []Config{
		{UserName: "root", Password: "secret", Host: "127.0.0.1", Port: "3306", DbName: "shop"},
		{UserName: "u@ser", Password: "p@ss:wo/rd(1)", Host: "db.local", Port: "3307", DbName: "d?loc=1"},
		{UserName: "u", Password: `p\ss`, Host: "db.local", Port: "3307", DbName: "d"},
	}
	for _, c := range cases {
		got, err := drvmysql.ParseDSN(dbcoremysql.DSN(c.ConnSpec()))
		if err != nil {
			t.Fatalf("ParseDSN: %v", err)
		}
		if got.User != c.UserName || got.Passwd != c.Password || got.DBName != c.DbName {
			t.Fatalf("round trip lost fields: %+v", got)
		}
		if !got.ParseTime {
			t.Fatal("kernel DSN must enable parseTime for typed scanning")
		}
		// '?' in the db name must be encoded, not injected as a DSN param.
		if _, injected := got.Params["loc"]; injected {
			t.Errorf("dbname %q injected DSN params", c.DbName)
		}
	}
}

func TestTarget(t *testing.T) {
	if got := Target("shop", "orders"); got != "`shop`.`orders`" {
		t.Errorf("qualified = %s, want `shop`.`orders`", got)
	}
	if got := Target("", "orders"); got != "`orders`" {
		t.Errorf("bare = %s, want `orders`", got)
	}
	if got := Target("shop", "we`ird"); got != "`shop`.`we``ird`" {
		t.Errorf("escaped = %s, want backtick-doubled", got)
	}
}
