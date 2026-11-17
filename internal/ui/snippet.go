package ui

// SQL 代码片段库（对齐 Navicat 片段 / DataGrip live templates；与 dbstudio 的
// snippet.go、fynequery 的 snippet.go 同一语义在 TUI 的落地）：
//
// 与收藏的区别——收藏是**历史条目的标记**（名字 = SQL 首行自动生成，存不了
// 没跑过的 SQL）；片段是**用户自己命名的、跨连接复用的 SQL 块**，可以先写好
// 存起来，之后在任何连接里载入。
//
// 存储：~/.config/sqltui/snippets.json，0600，tmp+rename 原子写；名字唯一键
// （同名覆盖 = 更新），列表按名字排序。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snippet 是一条用户命名的可复用 SQL 块。
type Snippet struct {
	Name string    `json:"name"`
	SQL  string    `json:"sql"`
	At   time.Time `json:"at"`
}

var errSnippetName = errors.New("片段名不能为空")

var (
	snippetMu     sync.Mutex
	snippetLoaded bool
	snippetItems  []Snippet
)

// snippetPath 是片段文件的位置（与 history.json 同目录）。
func snippetPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "sqltui", "snippets.json")
}

// loadSnippetsIfCold 首次访问时读盘（后续走内存，写时再落盘）。
func loadSnippetsIfCold() {
	if snippetLoaded {
		return
	}
	snippetLoaded = true
	raw, err := os.ReadFile(snippetPath())
	if err != nil {
		snippetItems = nil
		return
	}
	var v struct {
		Snippets []Snippet `json:"snippets"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		snippetItems = nil
	}
}

// ListSnippets 返回按名字排序的副本。
func ListSnippets() []Snippet {
	snippetMu.Lock()
	defer snippetMu.Unlock()
	loadSnippetsIfCold()
	out := append([]Snippet(nil), snippetItems...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PutSnippet 保存片段：同名覆盖（= 更新），空白名拒绝。
func PutSnippet(name, sql string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errSnippetName
	}
	sql = strings.TrimRight(sql, "\n")
	snippetMu.Lock()
	defer snippetMu.Unlock()
	loadSnippetsIfCold()
	sn := Snippet{Name: name, SQL: sql, At: time.Now()}
	for i := range snippetItems {
		if snippetItems[i].Name == name {
			snippetItems[i] = sn
			return saveSnippetsLocked()
		}
	}
	snippetItems = append(snippetItems, sn)
	return saveSnippetsLocked()
}

// DeleteSnippet 按名字删除；不存在报错。
func DeleteSnippet(name string) error {
	snippetMu.Lock()
	defer snippetMu.Unlock()
	loadSnippetsIfCold()
	for i := range snippetItems {
		if snippetItems[i].Name == name {
			snippetItems = append(snippetItems[:i], snippetItems[i+1:]...)
			return saveSnippetsLocked()
		}
	}
	return errSnippetName
}

// saveSnippetsLocked 原子落盘。调用方必须持锁。
func saveSnippetsLocked() error {
	raw, err := json.MarshalIndent(struct {
		Snippets []Snippet `json:"snippets"`
	}{snippetItems}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(snippetPath()), 0o755); err != nil {
		return err
	}
	tmp := snippetPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, snippetPath())
}

// SnippetFirstLine 是列表里的单行预览。
func SnippetFirstLine(sql string) string {
	first := strings.TrimSpace(strings.SplitN(sql, "\n", 2)[0])
	first = strings.ReplaceAll(first, "\t", " ")
	r := []rune(first)
	if len(r) > 44 {
		first = string(r[:44]) + "…"
	}
	return first
}
