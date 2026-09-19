package oobe

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// nowMicros returns current Unix time in microseconds.
func nowMicros() int64 {
	return time.Now().UnixMicro()
}

// buildSearchBody constructs the SSE search request body.
func buildSearchBody(sql string, limit int, startUS, endUS int64) map[string]any {
	return buildSearchBodyWithOffset(sql, limit, 0, startUS, endUS)
}

// buildSearchBodyWithOffset constructs the SSE search request body with a pagination offset.
func buildSearchBodyWithOffset(sql string, limit, from int, startUS, endUS int64) map[string]any {
	return map[string]any{
		"query": map[string]any{
			"sql":        sql,
			"start_time": startUS,
			"end_time":   endUS,
			"from":       from,
			"size":       limit,
			"quick_mode": false,
		},
	}
}

// formatTimestampShort formats microsecond timestamp as "2006-01-02 15:04:05.000"
// (no raw value) for table column display.
func formatTimestampShort(v any) string {
	var us int64
	switch x := v.(type) {
	case float64:
		us = int64(x)
	case int64:
		us = x
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	return time.UnixMicro(us).Local().Format("2006-01-02 15:04:05.000")
}

// readSSEHits reads an SSE stream and accumulates hits from all
// search_response_hits events until [[DONE]] or EOF.
func readSSEHits(r io.ReadCloser) ([]map[string]any, error) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	// Allow large lines (hits can be huge JSON blobs)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	var hits []map[string]any
	expectHitData := false

	for scanner.Scan() {
		line := scanner.Text()

		if line == "event: search_response_hits" {
			expectHitData = true
			continue
		}

		if expectHitData && strings.HasPrefix(line, "data: ") {
			expectHitData = false
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[[DONE]]" {
				break
			}
			var resp struct {
				Hits []map[string]any `json:"hits"`
			}
			if err := json.Unmarshal([]byte(payload), &resp); err != nil {
				return hits, fmt.Errorf("sse: parse hits: %w", err)
			}
			hits = append(hits, resp.Hits...)
			continue
		}

		if strings.HasPrefix(line, "data: [[DONE]]") {
			break
		}

		if !strings.HasPrefix(line, "event:") && !strings.HasPrefix(line, "data:") {
			expectHitData = false
		}
	}
	return hits, scanner.Err()
}
