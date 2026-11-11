package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// useTempConfig points the package at a config file inside a temp dir
// (standing in for a temp HOME) and restores the original paths afterwards.
func useTempConfig(t *testing.T) string {
	t.Helper()

	oldDataPath, oldConfigFile := dataPath, ConfigFile
	dir := t.TempDir()
	dataPath = dir
	ConfigFile = filepath.Join(dir, "config.yaml")
	t.Cleanup(func() {
		dataPath = oldDataPath
		ConfigFile = oldConfigFile
	})
	return dir
}

func TestUIConfigRoundtrip(t *testing.T) {
	useTempConfig(t)

	want := &UIConfig{Theme: "nord", ShowBorders: false, ShowRowNumbers: true}
	if err := WriteUIConfig(want); err != nil {
		t.Fatalf("WriteUIConfig: %v", err)
	}

	got, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	if *got != *want {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, want)
	}

	info, err := os.Stat(ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	// POSIX perms don't exist on Windows (0600 files report 0666).
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("config file perm = %o, want 600", perm)
		}
	}
}

func TestWritersProduceYAML(t *testing.T) {
	useTempConfig(t)

	if err := WriteUIConfig(&UIConfig{Theme: "nord", ShowBorders: true, ShowRowNumbers: true}); err != nil {
		t.Fatalf("WriteUIConfig: %v", err)
	}

	raw, err := os.ReadFile(ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	// valid YAML mapping, not JSON
	var asYAML map[string]any
	if err := yaml.Unmarshal(raw, &asYAML); err != nil {
		t.Fatalf("config file is not valid yaml: %v\n%s", err, raw)
	}
	if _, ok := asYAML["ui"]; !ok {
		t.Fatalf("yaml config missing ui section:\n%s", raw)
	}
	var asJSON map[string]any
	if err := json.Unmarshal(raw, &asJSON); err == nil {
		t.Fatalf("config file still looks like json:\n%s", raw)
	}
}

func TestReadUIConfigDefaultsWhenSectionMissing(t *testing.T) {
	useTempConfig(t)

	// write a config that has no ui section
	if err := WriteMysqlConfig(&MysqlConfig{UserName: "u", Host: "h", Port: "1", DbName: "d"}); err != nil {
		t.Fatalf("WriteMysqlConfig: %v", err)
	}

	got, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	want := DefaultUIConfig()
	if *got != *want {
		t.Fatalf("defaults mismatch: got %+v, want %+v", got, want)
	}
}

func TestReadUIConfigMissingFile(t *testing.T) {
	useTempConfig(t)

	if _, err := ReadUIConfig(); err == nil {
		t.Fatal("expected error when config file is missing")
	}
}

func TestWriteUIConfigPreservesOtherSections(t *testing.T) {
	useTempConfig(t)

	mysql := &MysqlConfig{UserName: "root", Password: "pw", Host: "127.0.0.1", Port: "3306", DbName: "db"}
	if err := WriteMysqlConfig(mysql); err != nil {
		t.Fatalf("WriteMysqlConfig: %v", err)
	}
	if err := WriteUIConfig(&UIConfig{Theme: "dracula", ShowBorders: true, ShowRowNumbers: false}); err != nil {
		t.Fatalf("WriteUIConfig: %v", err)
	}

	gotMysql, err := ReadMySqlConfig()
	if err != nil {
		t.Fatalf("ReadMySqlConfig: %v", err)
	}
	if *gotMysql != *mysql {
		t.Fatalf("mysql section clobbered: got %+v, want %+v", gotMysql, mysql)
	}

	gotUI, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	if gotUI.Theme != "dracula" || !gotUI.ShowBorders || gotUI.ShowRowNumbers {
		t.Fatalf("unexpected ui section: %+v", gotUI)
	}
}

// writeLegacyJSON drops a JSON config (the pre-YAML format) into the temp
// config dir.
func writeLegacyJSON(t *testing.T, dir string, conf SqlConfig) string {
	t.Helper()
	j, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacy, j, 0600); err != nil {
		t.Fatal(err)
	}
	return legacy
}

func TestMigrateLegacyJSONOnRead(t *testing.T) {
	dir := useTempConfig(t)

	legacyConf := SqlConfig{
		Mysql: &MysqlConfig{UserName: "legacy", Password: "pw", Host: "10.0.0.1", Port: "3307", DbName: "old"},
		UI:    &UIConfig{Theme: "gruvbox", ShowBorders: false, ShowRowNumbers: true},
	}
	legacy := writeLegacyJSON(t, dir, legacyConf)
	legacyBefore, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}

	// a plain read triggers the migration and sees the legacy values
	gotMysql, err := ReadMySqlConfig()
	if err != nil {
		t.Fatalf("ReadMySqlConfig: %v", err)
	}
	if *gotMysql != *legacyConf.Mysql {
		t.Fatalf("migrated mysql mismatch: got %+v, want %+v", gotMysql, legacyConf.Mysql)
	}
	gotUI, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	if *gotUI != *legacyConf.UI {
		t.Fatalf("migrated ui mismatch: got %+v, want %+v", gotUI, legacyConf.UI)
	}

	// the yaml file now exists with tight permissions...
	info, err := os.Stat(ConfigFile)
	if err != nil {
		t.Fatalf("config.yaml not created by migration: %v", err)
	}
	// POSIX perms don't exist on Windows (0600 files report 0666).
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("migrated config perm = %o, want 600", perm)
		}
	}

	// ...and the json file is untouched
	legacyAfter, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatalf("legacy json removed by migration: %v", err)
	}
	if string(legacyAfter) != string(legacyBefore) {
		t.Fatal("legacy json modified by migration")
	}
}

func TestMigrationSkippedWhenYAMLExists(t *testing.T) {
	dir := useTempConfig(t)

	if err := WriteUIConfig(&UIConfig{Theme: "nord", ShowBorders: true, ShowRowNumbers: true}); err != nil {
		t.Fatalf("WriteUIConfig: %v", err)
	}
	writeLegacyJSON(t, dir, SqlConfig{
		UI: &UIConfig{Theme: "gruvbox", ShowBorders: false, ShowRowNumbers: false},
	})

	got, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	if got.Theme != "nord" {
		t.Fatalf("yaml config overridden by stale json: %+v", got)
	}
}

func TestSetDefaultConfigMigratesLegacyJSON(t *testing.T) {
	dir := useTempConfig(t)

	writeLegacyJSON(t, dir, SqlConfig{
		Postgres: &PostgresConfig{UserName: "u", Host: "h", Port: "5433", DbName: "d", SslMode: "require"},
	})

	if err := SetDefaultConfig(); err != nil {
		t.Fatalf("SetDefaultConfig: %v", err)
	}

	got, err := ReadPostgresConfig()
	if err != nil {
		t.Fatalf("ReadPostgresConfig: %v", err)
	}
	if got.Port != "5433" || got.SslMode != "require" {
		t.Fatalf("legacy postgres values lost in migration: %+v", got)
	}
}

func TestSetDefaultConfigCreatesYAMLDefaults(t *testing.T) {
	useTempConfig(t)

	if err := SetDefaultConfig(); err != nil {
		t.Fatalf("SetDefaultConfig: %v", err)
	}

	if filepath.Ext(ConfigFile) != ".yaml" {
		t.Fatalf("canonical config file is not yaml: %s", ConfigFile)
	}
	got, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	want := DefaultUIConfig()
	if *got != *want {
		t.Fatalf("default ui mismatch: got %+v, want %+v", got, want)
	}
}

func TestMigrationIgnoresCorruptLegacyJSON(t *testing.T) {
	dir := useTempConfig(t)

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := SetDefaultConfig(); err != nil {
		t.Fatalf("SetDefaultConfig with corrupt legacy json: %v", err)
	}
	got, err := ReadUIConfig()
	if err != nil {
		t.Fatalf("ReadUIConfig: %v", err)
	}
	want := DefaultUIConfig()
	if *got != *want {
		t.Fatalf("defaults mismatch after corrupt migration: got %+v, want %+v", got, want)
	}
}

// TestUpdateSectionWritesAtomic 钉住配置写入的原子性：目标文件必须是临时文件
// + rename 替换，而不是 os.WriteFile 的 truncate+write（中途崩溃会截断/清空
// 含明文口令的配置）。用硬链接充当"旧备份"：原子替换后旧链接仍指向旧 inode、
// 旧内容；truncate+write 会改写同一 inode，旧链接立刻看到新内容，从而暴露
// 非原子写。
func TestUpdateSectionWritesAtomic(t *testing.T) {
	useTempConfig(t)

	// 第一次写入建立基线。
	if err := updateSection(func(c *SqlConfig) {
		c.Mysql = &MysqlConfig{UserName: "u1", Password: "pw1", Host: "h", Port: "3306", DbName: "d1"}
	}); err != nil {
		t.Fatalf("first updateSection: %v", err)
	}

	backup := ConfigFile + ".bak"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Link(ConfigFile, backup); err != nil {
		t.Fatalf("os.Link 不可用，无法验证原子替换语义: %v", err)
	}
	before, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}

	// 第二次写入只改口令。
	if err := updateSection(func(c *SqlConfig) {
		c.Mysql.Password = "pw2"
	}); err != nil {
		t.Fatalf("second updateSection: %v", err)
	}

	// 原子替换：旧硬链接内容不变（仍为 pw1）。
	after, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("配置写入非原子：旧硬链接内容被覆盖\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// 目标文件已是新内容且可完整解析，其他段落不受影响。
	cfg, err := readConfig()
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if cfg.Mysql == nil || cfg.Mysql.Password != "pw2" {
		t.Fatalf("新配置口令未落盘: %+v", cfg.Mysql)
	}

	// 不应留下临时文件。
	entries, err := os.ReadDir(filepath.Dir(ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("原子写入残留临时文件: %s", e.Name())
		}
	}
}
