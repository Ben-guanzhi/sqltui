package popup

// 导出被截断的结果时必须说明——否则用户以为拿到全量，实际少了后面的行。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportWarnsWhenFrameTruncated(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("exporter.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "frame.Truncated") {
		t.Error("导出逻辑没有读 frame.Truncated")
	}
	if !strings.Contains(s, "result was truncated") {
		t.Error("导出完成的提示没有说明结果被截断")
	}
}

// Frame 的 Truncated 标记必须由内核结果携带过来（FrameFromResult）。
func TestFrameCarriesTruncationFromKernel(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "core", "backend.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "f.Truncated = res.Truncated") {
		t.Error("FrameFromResult 没有把内核的 Truncated 标记带到 frame")
	}
}
