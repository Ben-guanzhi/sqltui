// Package pluginsdk is the plugin-side SDK for sqltui's process plugins:
// implement Source, call Main, and the executable is a driver the TUI can
// dial. The wire is internal/db/pluginwire — the host's one-method-one-RPC
// loop is answered here.
//
// A plugin process IS one connection: it opens its handle from argv[1]'s
// ConnSpec before writing the banner, then serves until the host closes
// stdin. No connection ids, no process sharing.

package pluginsdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/pluginbe"
	"github.com/LinPr/sqltui/internal/db/pluginwire"
)

// Source is the plugin side: a live connection whose methods the host calls.
// It is db.Backend plus Open, which builds it from the dial spec.
type Source interface {
	db.Backend
	// Open connects from argv's spec. Called once before the banner.
	Open(spec pluginbe.ConnSpec) error
}

// Main is a plugin's whole main: decode argv[1], connect, banner, serve.
func Main(newSource func() Source, kind, name string) {
	spec, err := specFromArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(2)
	}
	s := newSource()
	if err := s.Open(spec); err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect: %v\n", name, err)
		os.Exit(3)
	}
	ban := pluginwire.Banner{
		Proto:   pluginwire.ProtocolName,
		Version: pluginwire.ProtocolVersion,
		Kind:    kind,
		Name:    name,
	}
	if err := Serve(s, ban, os.Stdin, os.Stdout); err != nil {
		log.Printf("%s: serve: %v", name, err)
	}
}

func specFromArgs() (pluginbe.ConnSpec, error) {
	var spec pluginbe.ConnSpec
	if len(os.Args) < 2 {
		return spec, fmt.Errorf("missing connection spec: launch through sqltui (argv[1] carries the JSON spec)")
	}
	if err := json.Unmarshal([]byte(os.Args[1]), &spec); err != nil {
		return spec, fmt.Errorf("decode connection spec: %w", err)
	}
	return spec, nil
}

// Serve answers requests until r closes. Each request runs in its own
// goroutine with a bound, so a slow query cannot wedge the reader (and a
// future Cancel method can reach an in-flight call).
func Serve(s Source, ban pluginwire.Banner, r io.Reader, w io.Writer) error {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if err := writeLine(w, ban); err != nil {
		return fmt.Errorf("write banner: %w", err)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // SQL can be large
	inflight := make(chan struct{}, 8)
	var wmu sync.Mutex
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req pluginwire.Request
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("bad request line: %v", err)
			continue
		}
		// Close has no reply (the host closes the pipe right after); exit the
		// serve loop cleanly so deferred cleanup runs.
		if req.Method == pluginwire.MethodClose {
			_ = s.Close()
			return nil
		}
		inflight <- struct{}{}
		go func(req pluginwire.Request) {
			defer func() { <-inflight }()
			result, err := dispatch(s, req)
			res := pluginwire.Response{ID: req.ID}
			if err != nil {
				res.Error = err.Error()
			} else if result != nil {
				b, jerr := json.Marshal(result)
				if jerr != nil {
					res.Error = fmt.Sprintf("marshal result: %v", jerr)
				} else {
					res.Result = b
				}
			}
			frame, _ := json.Marshal(res)
			wmu.Lock()
			fmt.Fprintf(w, "%s\n", frame)
			wmu.Unlock()
		}(req)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read requests: %w", err)
	}
	_ = s.Close()
	return nil
}

func dispatch(s Source, req pluginwire.Request) (any, error) {
	switch req.Method {
	case pluginwire.MethodKind:
		return s.Kind(), nil
	case pluginwire.MethodTitle:
		return s.Title(), nil
	case pluginwire.MethodRun:
		var a pluginwire.StmtArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		r, err := s.Run(a.Stmt)
		if err != nil {
			return nil, err
		}
		return pluginwire.EncodeResult(r), nil
	case pluginwire.MethodNamespaces:
		return s.Namespaces()
	case pluginwire.MethodTables:
		var a pluginwire.NSArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return s.Tables(a.Namespace)
	case pluginwire.MethodFetchTable:
		var a pluginwire.FetchArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		f, err := s.FetchTable(a.Namespace, a.Table, a.Limit)
		if err != nil {
			return nil, err
		}
		return pluginwire.EncodeFrame(f), nil
	case pluginwire.MethodPrimaryKeys:
		var a pluginwire.TableArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return s.PrimaryKeys(a.Namespace, a.Table)
	case pluginwire.MethodColumnsMeta:
		var a pluginwire.TableArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return s.ColumnsMeta(a.Namespace, a.Table)
	case pluginwire.MethodColumnIndexTypes:
		var a pluginwire.TableArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return s.ColumnIndexTypes(a.Namespace, a.Table)
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
}

func writeLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
