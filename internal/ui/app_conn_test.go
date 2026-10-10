package ui

import (
	"testing"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// connFakeBackend records Close calls so ownership tests can assert who
// closed the connection.
type connFakeBackend struct{ closed int }

func (b *connFakeBackend) Kind() string  { return "sqlite" }
func (b *connFakeBackend) Title() string { return "fake" }
func (b *connFakeBackend) Run(string) (db.Result, error) {
	return db.Result{}, nil
}
func (b *connFakeBackend) Namespaces() ([]string, error)   { return nil, nil }
func (b *connFakeBackend) Tables(string) ([]string, error) { return nil, nil }
func (b *connFakeBackend) FetchTable(string, string, int) (*data.Frame, error) {
	return &data.Frame{}, nil
}
func (b *connFakeBackend) PrimaryKeys(string, string) ([]string, error) { return nil, nil }
func (b *connFakeBackend) ColumnsMeta(string, string) ([]db.ColumnMeta, error) {
	return nil, nil
}
func (b *connFakeBackend) ColumnIndexTypes(string, string) (map[string]string, error) {
	return nil, nil
}
func (b *connFakeBackend) Close() error {
	b.closed++
	return nil
}

// connFakeKV is the key-value counterpart of connFakeBackend.
type connFakeKV struct{ closed int }

func (k *connFakeKV) Title() string                     { return "fake-kv" }
func (k *connFakeKV) Do([]string) (string, error)       { return "", nil }
func (k *connFakeKV) ScanKeys(string) ([]string, error) { return nil, nil }
func (k *connFakeKV) Value(string) (string, error)      { return "", nil }
func (k *connFakeKV) Close() error {
	k.closed++
	return nil
}

func TestSetBackendMsgClosesReplacedBackend(t *testing.T) {
	be1 := &connFakeBackend{}
	a := New(Options{Backend: be1})

	// Re-attaching the same pointer is not a reconnect and must not close.
	a.Update(SetBackendMsg{Backend: be1})
	if be1.closed != 0 {
		t.Fatalf("same backend closed on re-set: %d", be1.closed)
	}

	// A different connection supersedes the old one, which is closed.
	be2 := &connFakeBackend{}
	a.Update(SetBackendMsg{Backend: be2})
	if be1.closed != 1 {
		t.Errorf("replaced backend closed %d times, want 1", be1.closed)
	}
	if be2.closed != 0 {
		t.Errorf("live backend closed %d times, want 0", be2.closed)
	}
}

func TestSetBackendMsgClosesReplacedKV(t *testing.T) {
	kv1 := &connFakeKV{}
	a := New(Options{KV: kv1})

	kv2 := &connFakeKV{}
	a.Update(SetBackendMsg{KV: kv2})
	if kv1.closed != 1 {
		t.Errorf("replaced KV closed %d times, want 1", kv1.closed)
	}
	if kv2.closed != 0 {
		t.Errorf("live KV closed %d times, want 0", kv2.closed)
	}
}

func TestCloseConnections(t *testing.T) {
	be := &connFakeBackend{}
	kv := &connFakeKV{}
	a := New(Options{})
	a.Update(SetBackendMsg{Backend: be, KV: kv})

	a.CloseConnections()
	if be.closed != 1 || kv.closed != 1 {
		t.Errorf("closed backend=%d kv=%d, want 1 each", be.closed, kv.closed)
	}
	if a.backend != nil || a.kv != nil {
		t.Error("CloseConnections left a live connection on the app")
	}

	// A second call is a no-op, not a double close.
	a.CloseConnections()
	if be.closed != 1 || kv.closed != 1 {
		t.Errorf("double close: backend=%d kv=%d", be.closed, kv.closed)
	}
}
