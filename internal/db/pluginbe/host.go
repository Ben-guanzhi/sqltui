// Package pluginbe is the host side of sqltui's process plugins (波 100
// 对齐 dbstudio): it implements db.Backend by translating each method into
// one newline-delimited JSON request over the child process's stdio.
//
// One process is one connection: it is spawned at Dial and dies at Close, so
// a plugin crash is contained to the tab that owns it and the pool never
// needs a connection id in the protocol. The plugin's stderr is dropped.

package pluginbe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/pluginwire"
)

// ConnSpec is a connection's dial parameters — the same fields the form
// collects. A plugin decodes it from argv[1] to open its own handle.
type ConnSpec struct {
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"sslMode"`
	Org      string `json:"org,omitempty"`
}

// Backend is one live plugin connection.
type Backend struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[int64]chan pluginwire.Response
	nextID  atomic.Int64
	readErr error
	closed  atomic.Bool

	kind  string
	title string
}

// Dial starts a plugin executable with the connection spec on argv[1],
// verifies its banner, and returns a ready Backend.
func Dial(ctx context.Context, exe string, spec ConnSpec) (*Backend, error) {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode connection spec: %w", err)
	}
	cmd := exec.Command(exe, string(specJSON))
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	b := &Backend{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: map[int64]chan pluginwire.Response{},
	}
	// Banner first. A timeout must not hang the TUI on a silent executable.
	type bannerResult struct {
		banner pluginwire.Banner
		err    error
	}
	ch := make(chan bannerResult, 1)
	go func() {
		line, err := b.stdout.ReadBytes('\n')
		if err != nil {
			ch <- bannerResult{err: err}
			return
		}
		var ban pluginwire.Banner
		if err := json.Unmarshal(line, &ban); err != nil {
			ch <- bannerResult{err: fmt.Errorf("decode banner: %w", err)}
			return
		}
		ch <- bannerResult{banner: ban}
	}()
	var ban pluginwire.Banner
	select {
	case r := <-ch:
		if r.err != nil {
			_ = b.kill()
			return nil, fmt.Errorf("plugin banner: %w", r.err)
		}
		ban = r.banner
	case <-time.After(10 * time.Second):
		_ = b.kill()
		return nil, fmt.Errorf("plugin banner timeout (no banner in 10s)")
	case <-ctx.Done():
		_ = b.kill()
		return nil, ctx.Err()
	}
	if ban.Proto != pluginwire.ProtocolName || ban.Version != pluginwire.ProtocolVersion {
		_ = b.kill()
		return nil, fmt.Errorf("plugin protocol %q v%d is not %q v%d",
			ban.Proto, ban.Version, pluginwire.ProtocolName, pluginwire.ProtocolVersion)
	}
	if want := spec.Kind; ban.Kind != want {
		_ = b.kill()
		return nil, fmt.Errorf("plugin %q serves kind %q, not %q", exe, ban.Kind, want)
	}
	b.kind = ban.Kind
	// readLoop must be running before the first call: the title round trip
	// below is an ordinary request/response, not part of the banner.
	go b.readLoop()
	// The banner's Name is a display fallback; the connection's own Title
	// comes from the plugin's Title() method over the wire, cached at dial
	// so the TUI never pays a round trip for a status-bar string.
	if err := b.call(context.Background(), pluginwire.MethodTitle, nil, &b.title); err != nil {
		_ = b.kill()
		return nil, fmt.Errorf("plugin title: %w", err)
	}
	return b, nil
}

func (b *Backend) readLoop() {
	for {
		line, err := b.stdout.ReadBytes('\n')
		if len(line) > 0 {
			var res pluginwire.Response
			if json.Unmarshal(line, &res) == nil {
				b.mu.Lock()
				ch := b.pending[res.ID]
				delete(b.pending, res.ID)
				b.mu.Unlock()
				if ch != nil {
					ch <- res
				}
			}
		}
		if err != nil {
			b.mu.Lock()
			b.readErr = err
			for id, ch := range b.pending {
				ch <- pluginwire.Response{ID: id, Error: "plugin process exited: " + err.Error()}
				delete(b.pending, id)
			}
			b.mu.Unlock()
			return
		}
	}
}

// call performs one request/response round trip.
func (b *Backend) call(ctx context.Context, method string, params, result any) error {
	if b.closed.Load() {
		return fmt.Errorf("plugin connection is closed")
	}
	var raw json.RawMessage
	if params != nil {
		var err error
		raw, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode params: %w", err)
		}
	}
	id := b.nextID.Add(1)
	ch := make(chan pluginwire.Response, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	frame, err := json.Marshal(pluginwire.Request{ID: id, Method: method, Params: raw})
	if err != nil {
		return err
	}
	b.writeMu.Lock()
	_, err = fmt.Fprintf(b.stdin, "%s\n", frame)
	b.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write to plugin: %w", err)
	}
	select {
	case res := <-ch:
		if res.Error != "" {
			return fmt.Errorf("%s", res.Error)
		}
		if result != nil && len(res.Result) > 0 {
			return json.Unmarshal(res.Result, result)
		}
		return nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return ctx.Err()
	case <-time.After(60 * time.Second):
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return fmt.Errorf("plugin %s timed out", method)
	}
}

var _ db.Backend = (*Backend)(nil)

func (b *Backend) Kind() string  { return b.kind }
func (b *Backend) Title() string { return b.title }

func (b *Backend) Run(stmt string) (db.Result, error) {
	var wire pluginwire.Result
	if err := b.call(context.Background(), pluginwire.MethodRun, pluginwire.StmtArgs{Stmt: stmt}, &wire); err != nil {
		return db.Result{}, err
	}
	return pluginwire.DecodeResult(wire)
}

func (b *Backend) Namespaces() ([]string, error) {
	var out []string
	err := b.call(context.Background(), pluginwire.MethodNamespaces, nil, &out)
	return out, err
}

func (b *Backend) Tables(namespace string) ([]string, error) {
	var out []string
	err := b.call(context.Background(), pluginwire.MethodTables, pluginwire.NSArgs{Namespace: namespace}, &out)
	return out, err
}

func (b *Backend) FetchTable(namespace, table string, limit int) (*data.Frame, error) {
	var wire *pluginwire.Frame
	if err := b.call(context.Background(), pluginwire.MethodFetchTable,
		pluginwire.FetchArgs{Namespace: namespace, Table: table, Limit: limit}, &wire); err != nil {
		return nil, err
	}
	return pluginwire.DecodeFrame(wire)
}

func (b *Backend) PrimaryKeys(namespace, table string) ([]string, error) {
	var out []string
	err := b.call(context.Background(), pluginwire.MethodPrimaryKeys,
		pluginwire.TableArgs{Namespace: namespace, Table: table}, &out)
	return out, err
}

func (b *Backend) ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error) {
	var out []db.ColumnMeta
	err := b.call(context.Background(), pluginwire.MethodColumnsMeta,
		pluginwire.TableArgs{Namespace: namespace, Table: table}, &out)
	return out, err
}

func (b *Backend) ColumnIndexTypes(namespace, table string) (map[string]string, error) {
	out := map[string]string{}
	err := b.call(context.Background(), pluginwire.MethodColumnIndexTypes,
		pluginwire.TableArgs{Namespace: namespace, Table: table}, &out)
	return out, err
}

func (b *Backend) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	// Best effort: tell the plugin to release its handle, then close the pipe
	// and reap the process so nothing is left behind.
	_, _ = fmt.Fprintf(b.stdin, "%s\n", mustJSON(pluginwire.Request{
		ID: b.nextID.Add(1), Method: pluginwire.MethodClose,
	}))
	_ = b.stdin.Close()
	return b.cmd.Wait()
}

func (b *Backend) kill() error {
	_ = b.stdin.Close()
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	return b.cmd.Wait()
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
