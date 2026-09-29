package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// homeDir resolves the user's home directory portably: os.UserHomeDir covers
// Windows (USERPROFILE) and Unix ($HOME); the explicit HOME fallback keeps
// unusual environments (containers, MSYS) working. Windows-native processes
// have no HOME, so relying on it alone made dataPath start with "/" and the
// init() MkdirAll crash the program before main() could run.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

var dataPath = filepath.Join(homeDir(), ".config", "sqltui")
var LogFile = dataPath + "/sqltui.log"

// ConfigFile is the canonical on-disk config location (YAML). Older versions
// stored JSON in config.json next to it; that file is migrated automatically
// on load (see migrateLegacyConfig) and left in place.
var ConfigFile = dataPath + "/config.yaml"

type MysqlConfig struct {
	UserName string `json:"userName" yaml:"userName"`
	Password string `json:"password" yaml:"password"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	DbName   string `json:"dbName" yaml:"dbName"`
}

type RedisConfig struct {
	UserName string `json:"userName" yaml:"userName"`
	Password string `json:"password" yaml:"password"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	RdbNum   string `json:"rdbNum" yaml:"rdbNum"`
}
type SqliteConfig struct {
	FilePath string `json:"filePath" yaml:"filePath"`
}

type PostgresConfig struct {
	UserName string `json:"userName" yaml:"userName"`
	Password string `json:"password" yaml:"password"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	DbName   string `json:"dbName" yaml:"dbName"`
	SslMode  string `json:"sslMode" yaml:"sslMode"`
}

type SqlServerConfig struct {
	UserName string `json:"userName" yaml:"userName"`
	Password string `json:"password" yaml:"password"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	DbName   string `json:"dbName" yaml:"dbName"`
}

type OracleConfig struct {
	UserName string `json:"userName" yaml:"userName"`
	Password string `json:"password" yaml:"password"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	DbName   string `json:"dbName" yaml:"dbName"`
}

type OpenObserveConfig struct {
	UserName       string   `yaml:"user_name,omitempty"`
	Password       string   `yaml:"password,omitempty"`
	Host           string   `yaml:"host,omitempty"`
	Port           string   `yaml:"port,omitempty"`
	Org            string   `yaml:"org,omitempty"`
	SummaryColumns []string `yaml:"summary_columns,omitempty"`
}

// UIConfig holds viewer preferences persisted between runs.
type UIConfig struct {
	Theme          string `json:"theme" yaml:"theme"`
	ShowBorders    bool   `json:"showBorders" yaml:"showBorders"`
	ShowRowNumbers bool   `json:"showRowNumbers" yaml:"showRowNumbers"`
}

// DefaultUIConfig returns the UI preferences used when none are saved.
func DefaultUIConfig() *UIConfig {
	return &UIConfig{
		Theme:          "sorbet",
		ShowBorders:    true,
		ShowRowNumbers: true,
	}
}

type SqlConfig struct {
	Mysql       *MysqlConfig       `json:"mysql" yaml:"mysql"`
	Redis       *RedisConfig       `json:"redis" yaml:"redis"`
	Sqlite      *SqliteConfig      `json:"sqlite" yaml:"sqlite"`
	Postgres    *PostgresConfig    `json:"postgres" yaml:"postgres"`
	SqlServer   *SqlServerConfig   `json:"sqlserver" yaml:"sqlserver"`
	Oracle      *OracleConfig      `json:"oracle" yaml:"oracle"`
	OpenObserve *OpenObserveConfig `json:"openobserve" yaml:"openobserve"`
	UI          *UIConfig          `json:"ui" yaml:"ui"`
}

func init() {
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		log.Fatal(err)
	}

	if err := SetDefaultLog(); err != nil {
		log.Fatal(err)
	}

	if err := SetDefaultConfig(); err != nil {
		log.Fatal(err)
	}
}

func defaultSqlConfig() SqlConfig {
	var c SqlConfig
	c.Mysql = &MysqlConfig{}
	defaultMysql(c.Mysql)
	c.Redis = &RedisConfig{}
	defaultRedis(c.Redis)
	c.Sqlite = &SqliteConfig{}
	defaultSqlite(c.Sqlite)
	c.Postgres = &PostgresConfig{}
	defaultPostgres(c.Postgres)
	c.SqlServer = &SqlServerConfig{}
	defaultSqlServer(c.SqlServer)
	c.Oracle = &OracleConfig{}
	defaultOracle(c.Oracle)
	c.UI = DefaultUIConfig()
	return c
}

// defaultMysql fills a zero MysqlConfig with the standard starter values.
func defaultMysql(c *MysqlConfig) {
	c.UserName = "root"
	c.Host = "127.0.0.1"
	c.Port = "3306"
	c.DbName = "test_db"
}

func defaultRedis(c *RedisConfig) {
	c.Host = "127.0.0.1"
	c.Port = "6379"
	c.RdbNum = "0"
}

// defaultSqlite points at a fresh default sqlite file inside the data path.
// Evaluated per call so tests that repoint dataPath see their own dir.
func defaultSqlite(c *SqliteConfig) {
	c.FilePath = filepath.Join(dataPath, "sqlite.default")
}

func defaultPostgres(c *PostgresConfig) {
	c.UserName = "postgres"
	c.Host = "127.0.0.1"
	c.Port = "5432"
	c.DbName = "postgres"
	c.SslMode = "disable"
}

func defaultSqlServer(c *SqlServerConfig) {
	c.UserName = "sa"
	c.Host = "127.0.0.1"
	c.Port = "1433"
	c.DbName = "master"
}

func defaultOracle(c *OracleConfig) {
	c.UserName = "system"
	c.Host = "127.0.0.1"
	c.Port = "1521"
	c.DbName = "xe"
}

// defaultOobe returns a starter OpenObserve config.
func defaultOobe() OpenObserveConfig {
	return OpenObserveConfig{Host: "localhost", Port: "5080", Org: "default"}
}

// ReadSection loads one engine section, filling engine defaults when the
// section is absent from the file. It errors like readConfig when the file
// itself is unreadable or corrupt.
func ReadSection[T any](get func(*SqlConfig) *T, set func(*SqlConfig, *T), fillDefaults func(*T)) (*T, error) {
	tmpConf, err := readConfig()
	if err != nil {
		return nil, err
	}
	if sec := get(&tmpConf); sec != nil {
		return sec, nil
	}
	sec := new(T)
	fillDefaults(sec)
	set(&tmpConf, sec)
	return sec, nil
}

// WriteSection replaces one engine section, preserving the others.
func WriteSection[T any](get func(*SqlConfig) *T, set func(*SqlConfig, *T), v *T) error {
	tmpConf := readSqlConfig()
	set(&tmpConf, v)
	return writeSqlConfig(tmpConf)
}

// Per-section accessors for the generic helpers above.
var (
	getMysql       = func(c *SqlConfig) *MysqlConfig { return c.Mysql }
	setMysql       = func(c *SqlConfig, s *MysqlConfig) { c.Mysql = s }
	getRedis       = func(c *SqlConfig) *RedisConfig { return c.Redis }
	setRedis       = func(c *SqlConfig, s *RedisConfig) { c.Redis = s }
	getSqlite      = func(c *SqlConfig) *SqliteConfig { return c.Sqlite }
	setSqlite      = func(c *SqlConfig, s *SqliteConfig) { c.Sqlite = s }
	getPostgres    = func(c *SqlConfig) *PostgresConfig { return c.Postgres }
	setPostgres    = func(c *SqlConfig, s *PostgresConfig) { c.Postgres = s }
	getSqlServer   = func(c *SqlConfig) *SqlServerConfig { return c.SqlServer }
	setSqlServer   = func(c *SqlConfig, s *SqlServerConfig) { c.SqlServer = s }
	getOracle      = func(c *SqlConfig) *OracleConfig { return c.Oracle }
	setOracle      = func(c *SqlConfig, s *OracleConfig) { c.Oracle = s }
	getOpenObserve = func(c *SqlConfig) *OpenObserveConfig { return c.OpenObserve }
	setOpenObserve = func(c *SqlConfig, s *OpenObserveConfig) { c.OpenObserve = s }
	getUI          = func(c *SqlConfig) *UIConfig { return c.UI }
	setUI          = func(c *SqlConfig, s *UIConfig) { c.UI = s }
)

func ReadMySqlConfig() (*MysqlConfig, error) {
	return ReadSection(getMysql, setMysql, defaultMysql)
}

func WriteMysqlConfig(mysqlConfig *MysqlConfig) error {
	return WriteSection(getMysql, setMysql, mysqlConfig)
}

func ReadRedisConfig() (*RedisConfig, error) {
	return ReadSection(getRedis, setRedis, defaultRedis)
}

func WriteRedisConfig(redisConfig *RedisConfig) error {
	return WriteSection(getRedis, setRedis, redisConfig)
}

func ReadSqliteConfig() (*SqliteConfig, error) {
	return ReadSection(getSqlite, setSqlite, defaultSqlite)
}

func WriteSqliteConfig(sqliteConfig *SqliteConfig) error {
	return WriteSection(getSqlite, setSqlite, sqliteConfig)
}

func ReadPostgresConfig() (*PostgresConfig, error) {
	return ReadSection(getPostgres, setPostgres, defaultPostgres)
}

func WritePostgresConfig(postgresConfig *PostgresConfig) error {
	return WriteSection(getPostgres, setPostgres, postgresConfig)
}

func ReadSqlServerConfig() (*SqlServerConfig, error) {
	return ReadSection(getSqlServer, setSqlServer, defaultSqlServer)
}

func WriteSqlServerConfig(sqlserverConfig *SqlServerConfig) error {
	return WriteSection(getSqlServer, setSqlServer, sqlserverConfig)
}

func ReadOracleConfig() (*OracleConfig, error) {
	return ReadSection(getOracle, setOracle, defaultOracle)
}

func WriteOracleConfig(oracleConfig *OracleConfig) error {
	return WriteSection(getOracle, setOracle, oracleConfig)
}

func ReadOpenObserveConfig() OpenObserveConfig {
	if c, err := ReadSection(getOpenObserve, setOpenObserve, func(s *OpenObserveConfig) { *s = defaultOobe() }); err == nil {
		return *c
	}
	return defaultOobe()
}

func WriteOpenObserveConfig(cfg OpenObserveConfig) error {
	return WriteSection(getOpenObserve, setOpenObserve, &cfg)
}

func ReadUIConfig() (*UIConfig, error) {
	return ReadSection(getUI, setUI, func(u *UIConfig) { *u = *DefaultUIConfig() })
}

func WriteUIConfig(uiConfig *UIConfig) error {
	return WriteSection(getUI, setUI, uiConfig)
}

// legacyConfigFile is the pre-YAML JSON config location, derived from the
// current ConfigFile so tests that repoint ConfigFile stay self-contained.
func legacyConfigFile() string {
	return filepath.Join(filepath.Dir(ConfigFile), "config.json")
}

// migrateLegacyConfig converts an old JSON config to the YAML file when the
// YAML file does not exist yet. The JSON file is left in place untouched.
// A missing JSON file is not an error; an unreadable one is only logged so
// that a fresh default config can still be created.
func migrateLegacyConfig() error {
	legacy := legacyConfigFile()
	if legacy == ConfigFile {
		return nil
	}
	if _, err := os.Stat(ConfigFile); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var tmpConf SqlConfig
	if err := json.Unmarshal(raw, &tmpConf); err != nil {
		log.Printf("config: cannot migrate %s (%v); starting from defaults", legacy, err)
		return nil
	}
	if err := writeSqlConfig(tmpConf); err != nil {
		return fmt.Errorf("config: migrating %s: %w", legacy, err)
	}
	log.Printf("config: migrated %s to %s (the old JSON file was left in place)", legacy, ConfigFile)
	return nil
}

// SetDefaultConfig makes sure a config file exists: it migrates a legacy JSON
// config if present, creates a default YAML file otherwise, and tightens the
// permissions of a pre-existing file.
func SetDefaultConfig() error {
	if err := migrateLegacyConfig(); err != nil {
		return err
	}

	if _, err := os.Stat(ConfigFile); err != nil {
		if os.IsNotExist(err) {
			log.Println("config file not found, creating default", ConfigFile)
			return writeSqlConfig(defaultSqlConfig())
		}
		return err
	}

	// tighten the permissions of a pre-existing config file, since it
	// stores database passwords in plaintext
	return os.Chmod(ConfigFile, 0600)
}

// readConfig migrates any legacy JSON config, then reads and parses the YAML
// config file.
func readConfig() (SqlConfig, error) {
	var tmpConf SqlConfig
	if err := migrateLegacyConfig(); err != nil {
		return tmpConf, err
	}
	raw, err := os.ReadFile(ConfigFile)
	if err != nil {
		return tmpConf, err
	}
	if err := yaml.Unmarshal(raw, &tmpConf); err != nil {
		return tmpConf, err
	}
	return tmpConf, nil
}

// readSqlConfig reads and parses the config file. When the file is missing
// or corrupt it returns a zero SqlConfig so that saving still works.
func readSqlConfig() SqlConfig {
	tmpConf, err := readConfig()
	if err != nil {
		return SqlConfig{}
	}
	return tmpConf
}

func writeSqlConfig(tmpConf SqlConfig) error {
	y, err := yaml.Marshal(&tmpConf)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigFile, y, 0600)
}

func SetDefaultLog() error {
	// truncate an oversized log at startup so it does not grow without bound
	const maxLogSize = 1 << 20 // 1 MB
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(LogFile); err == nil && info.Size() > maxLogSize {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}

	file, err := os.OpenFile(LogFile, flag, 0644)
	if err != nil {
		return err
	}

	log.SetOutput(file)
	return nil
}

// updateSection reads the full config, applies fn to mutate it, then writes it back.
func updateSection(fn func(*SqlConfig)) error {
	var cfg SqlConfig
	data, err := os.ReadFile(ConfigFile)
	if err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}
	fn(&cfg)
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigFile, out, 0600)
}

// ReadOobeSummaryColumns returns the user-configured OpenObserve summary columns.
// Returns nil when not set (caller should use the default timestamp+body selection).
func ReadOobeSummaryColumns() []string {
	cfg := ReadOpenObserveConfig()
	if len(cfg.SummaryColumns) == 0 {
		return nil
	}
	return cfg.SummaryColumns
}

// WriteOobeSummaryColumns saves the selected column names to the OpenObserve config section.
func WriteOobeSummaryColumns(cols []string) error {
	return updateSection(func(c *SqlConfig) {
		if c.OpenObserve == nil {
			c.OpenObserve = &OpenObserveConfig{}
		}
		c.OpenObserve.SummaryColumns = cols
	})
}
