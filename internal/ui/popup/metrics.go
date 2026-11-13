package popup

// 可用性指标（S-01）：契约在内核仓库 `dbcore/spec/usability-metrics.json`，
// 实现共用 `dbcore/usability`。TUI 侧只装一个包级记录器：main 装上后，
// qxRun（唯一的语句执行收口）自动把每条语句记成 query 任务。

import (
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dbcore/dbcore/usability"
)

var (
	metricsMu sync.RWMutex
	metrics   *usability.Recorder
)

// UseMetrics 装上可用性记录器；nil 表示不采集（默认，POLYDB_USABILITY 未设）。
func UseMetrics(r *usability.Recorder) {
	metricsMu.Lock()
	metrics = r
	metricsMu.Unlock()
}

func recordMetric(task, outcome string, started time.Time, fields map[string]string) {
	metricsMu.RLock()
	r := metrics
	metricsMu.RUnlock()
	if r == nil {
		return
	}
	r.Record(task, outcome, time.Since(started), fields)
}

// stmtKeyword 取语句首个关键字（SELECT/INSERT/…）作为 statement_type。
func stmtKeyword(sql string) string {
	s := strings.TrimLeftFunc(sql, func(r rune) bool {
		return unicode.IsSpace(r) || r == '(' || r == '/' || r == '*' || r == '-'
	})
	i := 0
	for i < len(s) && (s[i] == '_' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
		i++
	}
	if i == 0 {
		return "unknown"
	}
	if i > 12 {
		i = 12
	}
	return strings.ToUpper(s[:i])
}
