package ui

// 语句历史的持久化：每条跑过的 SQL 记下来（附连接名），供历史面板与
// Ctrl+Up/Ctrl+Down 导航在重启之后仍然可用。
//
// 文件是一份 JSON 列表，上限 maxHistory 条，最老的先丢弃；写盘走
// writeFileAtomic（tmp + rename），崩溃不会留半份文件。
//
// 对齐 fynequery 的 cmdhistory.go / dbstudio 的 history.go——同一个语义：
// **历史记下所有跑过的**，收藏（下一步）是标出来要反复用的。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxHistory 是历史文件的条数上限：文件不能无限膨胀。
const maxHistory = 1000

// HistoryRec 是一条跑过的语句：SQL 文本 + 连接名 + 时间。
type HistoryRec struct {
	Stmt string `json:"stmt"`
	Conn string `json:"conn,omitempty"`
	At   string `json:"at,omitempty"`
}

// historyPath 是历史文件的落盘位置（用户配置目录下的 sqltui/history.json）。
func historyPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "sqltui", "history.json")
	}
	return "history.json"
}

// historyMu 串行化历史文件的读写（执行可能并发）。
var historyMu sync.Mutex

// LoadHistory 读历史。文件缺失或损坏时返回 nil——历史是便利，不为它报错。
func LoadHistory() []HistoryRec {
	historyMu.Lock()
	defer historyMu.Unlock()
	return loadHistoryLocked()
}

func loadHistoryLocked() []HistoryRec {
	raw, err := os.ReadFile(historyPath())
	if err != nil {
		return nil
	}
	var recs []HistoryRec
	if json.Unmarshal(raw, &recs) == nil {
		return recs
	}
	return nil
}

// AppendHistory 记一条语句并落盘。裁剪到 maxHistory（最老的先丢）。
func AppendHistory(stmt, conn string) {
	if stmt == "" {
		return
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	recs := loadHistoryLocked()
	recs = append(recs, HistoryRec{
		Stmt: stmt,
		Conn: conn,
		At:   time.Now().Format(time.RFC3339),
	})
	if len(recs) > maxHistory {
		recs = recs[len(recs)-maxHistory:]
	}
	raw, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return
	}
	path := historyPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path) // 原子替换：崩溃不留半份文件
}
