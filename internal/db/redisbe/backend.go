// Package redisbe connects to a Redis server through the dbcore kernel KV
// driver: the kernel owns the connection, the logical-db selection, scanning,
// and value decoding; this wrapper keeps the TUI presentation — indented JSON
// renders and the per-type key browser.
package redisbe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/redis" // registers the redis factory

	"github.com/LinPr/sqltui/internal/db"
)

// KeyTypes lists the key types offered by the key browser, in display order.
var KeyTypes = []string{
	"string",
	"list",
	"set",
	"zset",
	"hash",
	"stream",
}

// ScanLimit caps the keys collected when browsing one type, so a huge
// keyspace does not freeze the UI or build an unbounded tree.
const ScanLimit = 500

// queryTimeout keeps a wedged server from freezing the UI (the SQL backends
// use the kernel's 30s; KV ops are cheaper, 10s is plenty).
const queryTimeout = 10 * time.Second

// maxScanRounds bounds the scan loop so a pathological keyspace cannot spin
// forever between the cursor and the cap.
const maxScanRounds = 1000

// Config holds the connection parameters for a redis server. RdbNum is the
// logical database number as entered by the user (must parse as an integer).
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	RdbNum   string
}

// ConnSpec renders the config as a kernel connection spec; the logical
// database travels as the "db" param, the kernel's port for Redis.
func (c Config) ConnSpec() dbcore.ConnSpec {
	return dbcore.ConnSpec{
		Kind:     dbcore.KindRedis,
		Host:     strings.TrimSpace(c.Host),
		Port:     parsePort(c.Port),
		User:     c.UserName,
		Password: c.Password,
		Params:   map[string]string{"db": strings.TrimSpace(c.RdbNum)},
	}
}

func parsePort(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 65535 {
		return 0
	}
	return n
}

// Backend adapts the kernel KV driver to the db.KVBackend contract.
type Backend struct {
	kv      dbcore.KVDriver
	scanner dbcore.KeyScanner // optional: fused type-enriched scanning
	typed   dbcore.TypedCommander
	addr    string
	rdbNum  int
}

var _ db.KVBackend = (*Backend)(nil)

// Connect opens a redis connection through the kernel and pings it before
// returning.
func Connect(cfg Config) (*Backend, error) {
	rdbNum, err := strconv.Atoi(strings.TrimSpace(cfg.RdbNum))
	if err != nil {
		return nil, fmt.Errorf("invalid redis db number %q: %w", cfg.RdbNum, err)
	}
	spec := cfg.ConnSpec()
	drv, err := dbcore.Open(context.Background(), spec, dbcore.PoolConfig{})
	if err != nil {
		return nil, err
	}
	kv, ok := drv.AsKV()
	if !ok {
		drv.Close()
		return nil, fmt.Errorf("redis: kernel driver is not a KV driver")
	}
	b := &Backend{
		kv:     kv,
		addr:   spec.Addr(),
		rdbNum: rdbNum,
	}
	// Optional kernel capabilities; Do and ScanKeys degrade when absent.
	if t, ok := drv.(dbcore.TypedCommander); ok {
		b.typed = t
	}
	if s, ok := drv.(dbcore.KeyScanner); ok {
		b.scanner = s
	}
	return b, nil
}

func (b *Backend) Title() string {
	return fmt.Sprintf("redis://%s/%d", b.addr, b.rdbNum)
}

// Do runs a raw redis command; the reply is rendered as indented JSON (nil
// becomes null, error replies surface as errors, like the pre-kernel client).
func (b *Backend) Do(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("empty command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	if b.typed != nil {
		reply, err := b.typed.ExecCommandTyped(ctx, args)
		if err != nil {
			return "", err
		}
		if reply.Kind == dbcore.RedisReplyKindError {
			if s, ok := reply.Value.(string); ok {
				return "", errors.New(s)
			}
			return "", fmt.Errorf("%v", reply.Value)
		}
		return FormatJSON(typedToNative(reply))
	}
	reply, err := b.kv.ExecCommand(ctx, args)
	if err != nil {
		return "", err
	}
	return reply.Raw, nil
}

// ScanKeys lists keys of one type, capped at ScanLimit. The kernel scanner
// fuses the type lookup into the scan loop (D5), so the type filter costs no
// per-key TYPE round trip.
func (b *Backend) ScanKeys(keyType string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	want := dbcore.RedisKeyType(keyType)
	keys := make([]string, 0, 64)
	if b.scanner != nil {
		cursor := uint64(0)
		for round := 0; round < maxScanRounds; round++ {
			page, err := b.scanner.ScanKeysEnriched(ctx, cursor, "*", 100)
			if err != nil {
				return nil, err
			}
			for _, e := range page.Keys {
				if e.Kind == want {
					keys = append(keys, e.Key)
					if len(keys) >= ScanLimit {
						return keys, nil
					}
				}
			}
			if page.Cursor == 0 {
				return keys, nil
			}
			cursor = page.Cursor
		}
		return keys, nil
	}
	// Graceful degradation for a KV driver without enriched scanning.
	cursor := uint64(0)
	for round := 0; round < maxScanRounds; round++ {
		page, err := b.kv.ScanKeys(ctx, cursor, "*", 100)
		if err != nil {
			return nil, err
		}
		for _, k := range page.Keys {
			t, err := b.kv.KeyType(ctx, k)
			if err != nil {
				return nil, err
			}
			if t == want {
				keys = append(keys, k)
				if len(keys) >= ScanLimit {
					return keys, nil
				}
			}
		}
		if page.Cursor == 0 {
			return keys, nil
		}
		cursor = page.Cursor
	}
	return keys, nil
}

// Value fetches the value stored at key and renders it per key type as
// indented JSON. Stream fields render in their stored order (the pre-kernel
// client rendered a map, whose order was Go's key sort).
func (b *Backend) Value(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	v, err := b.kv.GetValue(ctx, key)
	if err != nil {
		// The kernel reports a missing key as an error; the browser shows the
		// friendly placeholder the pre-kernel client did.
		if t, terr := b.kv.KeyType(ctx, key); terr == nil && t == dbcore.RedisNone {
			return "(key does not exist)", nil
		}
		return "", err
	}
	switch v.Type {
	case dbcore.RedisStr:
		return FormatJSON(v.Str)
	case dbcore.RedisList, dbcore.RedisSet:
		return FormatJSON(v.Items)
	case dbcore.RedisHash:
		return FormatJSON(v.Hash)
	case dbcore.RedisZSet:
		pairs := make([]zsetMember, len(v.Items))
		for i, m := range v.Items {
			pairs[i] = zsetMember{Member: m}
			if i < len(v.Scores) {
				pairs[i].Score = v.Scores[i]
			}
		}
		return FormatJSON(pairs)
	case dbcore.RedisStream:
		entries := make([]streamEntry, len(v.Stream))
		for i, e := range v.Stream {
			fields := make(map[string]string, len(e.Fields))
			for _, f := range e.Fields {
				fields[f.Name] = f.Value
			}
			entries[i] = streamEntry{ID: e.ID, Values: fields}
		}
		return FormatJSON(entries)
	default:
		return "sqltui has not implemented type: " + string(v.Type), nil
	}
}

func (b *Backend) Close() error { return b.kv.Close() }

type zsetMember struct {
	Member string  `json:"member"`
	Score  float64 `json:"score"`
}

type streamEntry struct {
	ID     string            `json:"id"`
	Values map[string]string `json:"values"`
}

// typedToNative converts a structured RESP reply into JSON-marshalable Go
// values: strings, int64s, nil, and nested slices.
func typedToNative(r dbcore.RedisTypedReply) any {
	switch r.Kind {
	case dbcore.RedisReplyKindNull:
		return nil
	case dbcore.RedisReplyKindBulkString:
		if s, ok := r.Value.(string); ok {
			return s
		}
		return fmt.Sprint(r.Value)
	case dbcore.RedisReplyKindInteger:
		if n, ok := r.Value.(int64); ok {
			return n
		}
	case dbcore.RedisReplyKindArray:
		if items, ok := r.Value.([]dbcore.RedisTypedReply); ok {
			out := make([]any, len(items))
			for i, e := range items {
				out[i] = typedToNative(e)
			}
			return out
		}
	}
	return fmt.Sprint(r.Value)
}

// FormatJSON renders any value as indented JSON.
func FormatJSON(a any) (string, error) {
	j, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return "", err
	}
	return string(j), nil
}
