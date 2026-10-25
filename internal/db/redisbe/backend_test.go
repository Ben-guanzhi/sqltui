package redisbe

import (
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestConnectRejectsInvalidDbNumber(t *testing.T) {
	tests := []struct {
		name   string
		rdbNum string
	}{
		{"not a number", "abc"},
		{"empty", ""},
		{"float", "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Connect(Config{Host: "127.0.0.1", Port: "6379", RdbNum: tt.rdbNum})
			if err == nil {
				t.Fatal("expected error for invalid db number")
			}
			if !strings.Contains(err.Error(), "invalid redis db number") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFormatJson(t *testing.T) {
	got, err := FormatJSON(map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	want := "{\n  \"k\": \"v\"\n}"
	if got != want {
		t.Fatalf("FormatJSON = %q, want %q", got, want)
	}
}

// newTestBackend runs the full kernel KV path against an in-process server.
func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	s := miniredis.RunT(t)
	be, err := Connect(Config{Host: s.Host(), Port: s.Port(), RdbNum: "0"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { be.Close() })
	return be
}

func TestScanKeysFiltersByType(t *testing.T) {
	be := newTestBackend(t)

	// Seed through the backend's own Do (kernel typed command path).
	for _, cmd := range [][]string{
		{"SET", "s:a", "1"},
		{"LPUSH", "l:a", "x"},
		{"SADD", "se:a", "m"},
		{"ZADD", "z:a", "1.5", "m"},
		{"HSET", "h:a", "f", "v"},
	} {
		if _, err := be.Do(cmd); err != nil {
			t.Fatalf("Do(%v): %v", cmd, err)
		}
	}
	strs, err := be.ScanKeys("string")
	if err != nil {
		t.Fatalf("ScanKeys(string): %v", err)
	}
	if len(strs) != 1 || strs[0] != "s:a" {
		t.Fatalf("string keys = %v", strs)
	}
	zsets, err := be.ScanKeys("zset")
	if err != nil {
		t.Fatalf("ScanKeys(zset): %v", err)
	}
	if len(zsets) != 1 || zsets[0] != "z:a" {
		t.Fatalf("zset keys = %v", zsets)
	}
}

func TestDoRendersJsonAndSurfacesErrors(t *testing.T) {
	be := newTestBackend(t)

	got, err := be.Do([]string{"SET", "k", "v"})
	if err != nil {
		t.Fatalf("SET: %v", err)
	}
	if !strings.Contains(got, "OK") {
		t.Fatalf("SET reply = %q", got)
	}

	got, err = be.Do([]string{"INCR", "counter"})
	if err != nil {
		t.Fatalf("INCR: %v", err)
	}
	if strings.TrimSpace(got) != "1" {
		t.Fatalf("INCR reply = %q, want 1", got)
	}

	// An error reply surfaces as an error, matching the pre-kernel client.
	if _, err := be.Do([]string{"SET", "s:a", "hello"}); err != nil {
		t.Fatalf("SET s:a: %v", err)
	}
	if _, err := be.Do([]string{"INCR", "s:a"}); err == nil {
		t.Fatal("INCR on a non-numeric string key must fail")
	}
}

func TestValueRendersPerType(t *testing.T) {
	be := newTestBackend(t)
	for _, cmd := range [][]string{
		{"SET", "str:1", "hello"},
		{"RPUSH", "list:1", "a", "b"},
		{"HSET", "hash:1", "f1", "v1", "f2", "v2"},
		{"ZADD", "zset:1", "1.5", "one", "2.5", "two"},
		{"XADD", "stream:1", "*", "k", "v"},
	} {
		if _, err := be.Do(cmd); err != nil {
			t.Fatalf("Do(%v): %v", cmd, err)
		}
	}

	got, err := be.Value("str:1")
	if err != nil || strings.TrimSpace(got) != `"hello"` {
		t.Fatalf("string value = %q, %v", got, err)
	}

	got, err = be.Value("list:1")
	if err != nil || !strings.Contains(got, `"a"`) || !strings.Contains(got, `"b"`) {
		t.Fatalf("list value = %q, %v", got, err)
	}

	got, err = be.Value("hash:1")
	if err != nil || !strings.Contains(got, `"f1": "v1"`) {
		t.Fatalf("hash value = %q, %v", got, err)
	}

	got, err = be.Value("zset:1")
	if err != nil || !strings.Contains(got, `"member": "one"`) || !strings.Contains(got, `"score": 1.5`) {
		t.Fatalf("zset value = %q, %v", got, err)
	}

	got, err = be.Value("stream:1")
	if err != nil || !strings.Contains(got, `"k": "v"`) || !strings.Contains(got, `"values"`) {
		t.Fatalf("stream value = %q, %v", got, err)
	}

	// A missing key renders the friendly placeholder.
	got, err = be.Value("no:such:key")
	if err != nil || got != "(key does not exist)" {
		t.Fatalf("missing key = %q, %v", got, err)
	}
}

func TestTitleCarriesDbNumber(t *testing.T) {
	be := newTestBackend(t)
	if !strings.HasPrefix(be.Title(), "redis://") || !strings.HasSuffix(be.Title(), "/0") {
		t.Fatalf("Title = %q", be.Title())
	}
}
