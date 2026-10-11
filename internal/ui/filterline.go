package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/theme"
)

// FilterLine renders the unified filter input line used across all views.
// When !filtering && filter=="" the caller should skip rendering this line.
// Hint is the placeholder text shown when filtering but no text typed yet.
func FilterLine(filter string, filtering bool, width int, hint string, th *theme.Theme) string {
	prefix := th.Subtle.Render(" filter ")
	var body string
	switch {
	case filtering && filter == "":
		body = th.Placeholder.Render(hint)
	case filtering:
		body = th.Input.Render(filter) + th.ListSelected.Render(" ")
	default:
		body = th.Input.Render(filter)
	}
	line := prefix + body
	w := ansi.StringWidth(line)
	if w > width {
		// 过滤文本比可用宽度还长：截断。不截断的话整行越界、终端硬回绕，
		// 所有调用方（搜索条、sheet 过滤、redis 过滤、schema/标签切换弹窗）都会中招。
		return ansi.Truncate(line, width, "")
	}
	if w < width {
		line += th.Text.Render(strings.Repeat(" ", width-w))
	}
	return line
}
