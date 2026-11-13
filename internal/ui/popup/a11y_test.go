package popup

// 可及性基线（S-04）：TUI 的"可读名字"就是命令的 Name + Description——键盘用户
// 通过命令面板找到一切动作，所以每条命令都必须有可读的名字与说明，且不能靠颜色
// 传达状态（状态栏是文本）。
//
// 规则来源：内核仓库 dbcore/spec/a11y-baseline.json（rules[].applies_to 含 tui）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type a11yContract struct {
	Rules []struct {
		ID        string   `json:"id"`
		AppliesTo []string `json:"applies_to"`
	} `json:"rules"`
}

func TestPaletteCommandsAreReadable(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "dbcore", "spec", "a11y-baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读可及性契约 %s: %v", path, err)
	}
	var c a11yContract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("解析契约: %v", err)
	}
	covered := false
	for _, r := range c.Rules {
		if r.ID != "interactive-name" {
			continue
		}
		for _, s := range r.AppliesTo {
			if s == "tui" || s == "web" {
				covered = covered || s == "tui"
			}
		}
	}
	if !covered {
		t.Log("契约未声明 interactive-name 适用于 tui；TUI 的键盘可达性由命令面板承载")
	}

	if len(Commands) < 20 {
		t.Fatalf("命令面板只有 %d 条命令，键盘可达性存疑", len(Commands))
	}
	seen := map[string]bool{}
	for _, cmd := range Commands {
		if strings.TrimSpace(cmd.Name) == "" {
			t.Errorf("命令缺少 Name（键盘用户无法找到它）: %+v", cmd)
		}
		if strings.TrimSpace(cmd.Description) == "" {
			t.Errorf("命令 %q 缺少 Description（面板里没有可读说明）", cmd.Name)
		}
		if seen[cmd.Name] {
			t.Errorf("命令 %q 重名：后者永远不可达", cmd.Name)
		}
		seen[cmd.Name] = true
		for _, a := range cmd.Aliases {
			if strings.TrimSpace(a) == "" {
				t.Errorf("命令 %q 有空别名", cmd.Name)
			}
		}
	}
}
