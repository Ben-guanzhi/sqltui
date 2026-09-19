package oobe

import (
	"fmt"
	"strings"
	"time"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
)

// Config holds connection parameters for an OpenObserve server.
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	Org      string
}

// Backend adapts OpenObserve to the db.Backend contract.
type Backend struct {
	cfg       Config
	client    *httpClient
	timeRange int // query time range in minutes, default 15
	page      int // current 0-based page index
	pageSize  int   // rows per page, default 200
	startUS   int64 // cached query window start (microseconds); 0 = not yet initialized
	endUS     int64 // cached query window end (microseconds)
}

var _ db.Backend = (*Backend)(nil)

// Connect creates a Backend and verifies connectivity by listing namespaces.
func Connect(cfg Config) (*Backend, error) {
	b := &Backend{
		cfg:       cfg,
		client:    newHTTPClient(cfg.Host, cfg.Port, cfg.UserName, cfg.Password),
		timeRange: 15,
		page:      0,
		pageSize:  200,
	}
	if _, err := b.Namespaces(); err != nil {
		return nil, fmt.Errorf("openobserve: connect: %w", err)
	}
	return b, nil
}

// SetTimeRange sets the query time range in minutes.
func (b *Backend) SetTimeRange(minutes int) {
	if minutes > 0 {
		b.timeRange = minutes
	}
}

// GetTimeRange returns the current time range in minutes.
func (b *Backend) GetTimeRange() int { return b.timeRange }

// SetPage sets the current 0-based page index.
func (b *Backend) SetPage(page int) {
	if page >= 0 {
		b.page = page
	}
}

// GetPage returns the current 0-based page index.
func (b *Backend) GetPage() int { return b.page }

// GetPageSize returns the page size.
func (b *Backend) GetPageSize() int { return b.pageSize }

// ResetTimeWindow recalculates the query time window to end NOW.
// Called on explicit refresh (r key) or when the time-range setting changes.
func (b *Backend) ResetTimeWindow() {
	b.endUS = nowMicros()
	b.startUS = b.endUS - int64(time.Duration(b.timeRange)*time.Minute/time.Microsecond)
}

func (b *Backend) SetPageSize(n int) {
	if n > 0 {
		b.pageSize = n
	}
}

func (b *Backend) Kind() string { return "openobserve" }

func (b *Backend) Title() string {
	return fmt.Sprintf("openobserve://%s:%s/%s", b.cfg.Host, b.cfg.Port, b.cfg.Org)
}

// CurrentNamespace satisfies the optional interface used by CurrentTableNamespace
// so that the refresh command can re-fetch the correct org after navigation.
func (b *Backend) CurrentNamespace() string { return b.cfg.Org }

// Namespaces returns the single configured org as the only namespace.
func (b *Backend) Namespaces() ([]string, error) {
	// Ping by fetching streams; if it fails, connection is broken.
	path := fmt.Sprintf("/api/%s/streams?type=logs&offset=0&limit=1&sort=name&asc=true", b.cfg.Org)
	var resp struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	}
	if err := b.client.getJSON(path, &resp); err != nil {
		return nil, err
	}
	return []string{b.cfg.Org}, nil
}

// Tables lists log streams in the given namespace (org).
func (b *Backend) Tables(namespace string) ([]string, error) {
	path := fmt.Sprintf("/api/%s/streams?type=logs&offset=0&limit=200&sort=name&asc=true", namespace)
	var resp struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	}
	if err := b.client.getJSON(path, &resp); err != nil {
		return nil, err
	}
	names := make([]string, len(resp.List))
	for i, s := range resp.List {
		names[i] = s.Name
	}
	return names, nil
}

// ColumnsMeta returns column metadata for a log stream via its schema endpoint.
func (b *Backend) ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error) {
	path := fmt.Sprintf("/api/%s/streams/%s/schema?type=logs", namespace, table)
	var resp struct {
		Schema []struct {
			Name     string `json:"name"`
			DataType string `json:"type"`
		} `json:"schema"`
	}
	if err := b.client.getJSON(path, &resp); err != nil {
		return nil, err
	}
	metas := make([]db.ColumnMeta, len(resp.Schema))
	for i, s := range resp.Schema {
		metas[i] = db.ColumnMeta{
			Name:       s.Name,
			DataType:   s.DataType,
			IsNullable: "YES",
		}
	}
	return metas, nil
}

// mapDType converts an OpenObserve field type to a data.DType.
func mapDType(ooType string) data.DType {
	switch {
	case ooType == "Int64" || strings.Contains(ooType, "Int"):
		return data.TypeInt
	case ooType == "Float64" || strings.Contains(ooType, "Float"):
		return data.TypeFloat
	default:
		return data.TypeString
	}
}

// buildFrame constructs a data.Frame from column metadata and SSE hits.
func buildFrame(metas []db.ColumnMeta, hits []map[string]any) *data.Frame {
	if len(metas) == 0 {
		// Derive columns from first hit when no schema is available.
		if len(hits) == 0 {
			return data.New()
		}
		keys := make([]string, 0, len(hits[0]))
		for k := range hits[0] {
			keys = append(keys, k)
		}
		f := data.New(keys...)
		for _, hit := range hits {
			row := make([]any, len(keys))
			for i, k := range keys {
				row[i] = cellValue(hit[k], data.TypeString)
			}
			f.AppendRow(row)
		}
		return f
	}

	names := make([]string, len(metas)+1)
	types := make([]data.DType, len(metas)+1)
	// index 0: timestamp — human-readable formatted time (no raw value)
	names[0] = "timestamp"
	types[0] = data.TypeString
	for i, m := range metas {
		names[i+1] = m.Name
		types[i+1] = mapDType(m.DataType)
	}

	f := data.New(names...)
	for i, t := range types {
		f.Columns[i].Type = t
	}

	for _, hit := range hits {
		row := make([]any, len(metas)+1)
		row[0] = formatTimestampShort(hit["_timestamp"])
		for i, m := range metas {
			row[i+1] = cellValue(hit[m.Name], types[i+1])
		}
		f.AppendRow(row)
	}
	return f
}

// cellValue coerces a raw JSON value to the expected data.DType.
func cellValue(v any, t data.DType) any {
	if v == nil {
		return nil
	}
	switch t {
	case data.TypeInt:
		switch x := v.(type) {
		case float64:
			return int64(x)
		case int64:
			return x
		case string:
			var i int64
			fmt.Sscanf(x, "%d", &i)
			return i
		}
	case data.TypeFloat:
		switch x := v.(type) {
		case float64:
			return x
		case int64:
			return float64(x)
		case string:
			var f float64
			fmt.Sscanf(x, "%f", &f)
			return f
		}
	}
	// TypeString or unknown — stringify
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// sseSearch runs an SSE search over the given time window and returns hits.
func (b *Backend) sseSearch(namespace, sql string, limit int, startUS, endUS int64) ([]map[string]any, error) {
	path := fmt.Sprintf("/api/%s/_search_stream?type=logs&search_type=ui&use_cache=true", namespace)
	body := buildSearchBody(sql, limit, startUS, endUS)
	rc, err := b.client.postSSE(path, body)
	if err != nil {
		return nil, err
	}
	return readSSEHits(rc)
}

// FetchTable loads up to limit rows of namespace.table, newest first.
func (b *Backend) FetchTable(namespace, table string, limit int) (*data.Frame, error) {
	metas, err := b.ColumnsMeta(namespace, table)
	if err != nil {
		metas = nil
	}

	if b.startUS == 0 {
		b.ResetTimeWindow()
	}
	startUS, endUS := b.startUS, b.endUS

	offset := b.page * b.pageSize

	sql := fmt.Sprintf(`SELECT * FROM "%s" ORDER BY _timestamp DESC`, table)
	body := buildSearchBodyWithOffset(sql, b.pageSize, offset, startUS, endUS)
	rc, err := b.client.postSSE(
		fmt.Sprintf("/api/%s/_search_stream?type=logs&search_type=ui&use_cache=true", namespace),
		body,
	)
	if err != nil {
		return nil, err
	}
	hits, err := readSSEHits(rc)
	if err != nil {
		return nil, err
	}
	return buildFrame(metas, hits), nil
}

// Run executes a SQL statement. For OpenObserve all statements are read-only
// queries sent over the SSE search endpoint against the last 15 minutes.
func (b *Backend) Run(stmt string) (db.Result, error) {
	endUS := nowMicros()
	startUS := endUS - int64(15*time.Minute/time.Microsecond)

	hits, err := b.sseSearch(b.cfg.Org, stmt, 1000, startUS, endUS)
	if err != nil {
		return db.Result{}, err
	}
	frame := buildFrame(nil, hits)
	return db.Result{Frame: frame}, nil
}

// PrimaryKeys returns ["_timestamp"] as the logical primary key for every stream.
func (b *Backend) PrimaryKeys(namespace, table string) ([]string, error) {
	return []string{"_timestamp"}, nil
}

// ColumnIndexTypes returns {"_timestamp": "INDEX"} for every stream.
func (b *Backend) ColumnIndexTypes(namespace, table string) (map[string]string, error) {
	return map[string]string{"_timestamp": "INDEX"}, nil
}

// Close is a no-op; the HTTP client has no persistent connections to release.
func (b *Backend) Close() error { return nil }
