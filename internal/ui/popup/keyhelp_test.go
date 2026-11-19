package popup

// 速查 overlay：内容齐全、能开能关。

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func TestKeyHelpListsTheCommands(t *testing.T) {
	k := NewKeyHelp()
	th := theme.Default()
	view := k.View(80, 30, th)
	// 表里的关键条目都要出现——加键却漏了速查，正是这个模式要防的。
	for _, want := range []string{":help", ":quit", ":toggleborders", ":dict", ":indexes", ":gendata", "ctrl+s", "esc", "j / ↓"} {
		if !strings.Contains(view, want) {
			t.Errorf("速查里没有 %q；\n%s", want, view)
		}
	}
}

func TestKeyHelpClosesOnEsc(t *testing.T) {
	k := NewKeyHelp()
	_, cmd := k.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc 没有产生关闭命令")
	}
	if _, isClose := cmd().(ui.CloseOverlayMsg); !isClose {
		t.Errorf("esc 的命令不是 CloseOverlay：%T", cmd())
	}
}

func TestKeyHelpSwallowsOtherKeys(t *testing.T) {
	k := NewKeyHelp()
	// 普通键不该传给下层（速查开着时）。
	_, cmd := k.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil {
		t.Errorf("普通键产生了命令 %v——速查会漏键给下层", cmd)
	}
}
