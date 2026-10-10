package dbmode

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/config"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/mysqlbe"
	"github.com/LinPr/sqltui/internal/db/oobe"
	"github.com/LinPr/sqltui/internal/db/oraclebe"
	"github.com/LinPr/sqltui/internal/db/postgresbe"
	"github.com/LinPr/sqltui/internal/db/redisbe"
	"github.com/LinPr/sqltui/internal/db/sqlserverbe"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// Field keys shared by field construction, dialing and config saving.
const (
	fkUser     = "user"
	fkPassword = "password"
	fkHost     = "host"
	fkPort     = "port"
	fkDatabase = "database"
	fkSslMode  = "sslmode"
	fkDbNum    = "dbnum"
	fkOrg      = "org"
)

// connField is one editable line of the connection form. The LineInput is
// used append-only: the cursor never moves off the end, so Backspace/Clear/
// InsertKey behave as "delete last rune / wipe / append".
type connField struct {
	key    string
	label  string
	masked bool
	input  ui.LineInput
}

// connDoneMsg reports the outcome of an asynchronous connection attempt.
// Exactly one of be/kv is non-nil on success.
type connDoneMsg struct {
	kind string
	vals map[string]string
	be   db.Backend
	kv   db.KVBackend
	err  error
}

// connForm is the connection form overlay, parameterized by engine kind.
// Fields prefill from the saved config; ctrl+s saves, enter (or ctrl+r)
// connects asynchronously from any field.
type connForm struct {
	ctx        ui.AppContext
	kind       string
	fields     []connField
	focus      int
	errText    string
	connecting bool
}

func newConnForm(ctx ui.AppContext, kind string) *connForm {
	return &connForm{ctx: ctx, kind: kind, fields: loadFields(kind)}
}

// loadFields builds the field list for one kind, prefilled from the saved
// per-engine config (best effort: a missing/corrupt config yields empty
// prefills, the readers themselves provide sensible defaults).
func loadFields(kind string) []connField {
	spec, ok := engineSpecs[kind]
	if !ok {
		return nil
	}
	vals := spec.prefill()
	fields := make([]connField, len(spec.fields))
	for i, s := range spec.fields {
		fields[i] = connField{key: s.key, label: s.label, masked: s.masked, input: ui.NewLineInput(vals[i])}
	}
	return fields
}

// fieldSpec is one row of the engineSpecs table: the form field key, display
// label and whether the input is masked.
type fieldSpec struct {
	key    string
	label  string
	masked bool
}

// baseFields is the common field head, in display order.
var baseFields = []fieldSpec{
	{key: fkUser, label: "user"},
	{key: fkPassword, label: "password", masked: true},
	{key: fkHost, label: "host"},
	{key: fkPort, label: "port"},
}

// engineSpec declares, for one engine kind, the connection form fields in
// display order, the prefill source, the dialer and the config writer.
// engineSpecs is the single source of truth behind loadFields, dial and
// saveConfig.
type engineSpec struct {
	fields  []fieldSpec
	prefill func() []string
	dial    func(v map[string]string) (db.Backend, db.KVBackend, error)
	save    func(v map[string]string) error
}

func fieldsFor(tail ...fieldSpec) []fieldSpec {
	all := make([]fieldSpec, 0, len(baseFields)+len(tail))
	all = append(all, baseFields...)
	return append(all, tail...)
}

var engineSpecs = map[string]engineSpec{
	KindMysql: {
		fields: fieldsFor(fieldSpec{key: fkDatabase, label: "database"}),
		prefill: func() []string {
			c, err := config.ReadMySqlConfig()
			if err != nil {
				c = &config.MysqlConfig{}
			}
			return []string{c.UserName, c.Password, c.Host, c.Port, c.DbName}
		},
		dial: func(v map[string]string) (db.Backend, db.KVBackend, error) {
			be, err := mysqlbe.Connect(mysqlbe.Config{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			})
			if err != nil {
				return nil, nil, err
			}
			return be, nil, nil
		},
		save: func(v map[string]string) error {
			return config.WriteMysqlConfig(&config.MysqlConfig{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			})
		},
	},
	KindPostgres: {
		fields: fieldsFor(
			fieldSpec{key: fkDatabase, label: "database"},
			fieldSpec{key: fkSslMode, label: "sslmode"},
		),
		prefill: func() []string {
			c, err := config.ReadPostgresConfig()
			if err != nil {
				c = &config.PostgresConfig{}
			}
			return []string{c.UserName, c.Password, c.Host, c.Port, c.DbName, c.SslMode}
		},
		dial: func(v map[string]string) (db.Backend, db.KVBackend, error) {
			be, err := postgresbe.Connect(postgresbe.Config{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
				SslMode: v[fkSslMode],
			})
			if err != nil {
				return nil, nil, err
			}
			return be, nil, nil
		},
		save: func(v map[string]string) error {
			return config.WritePostgresConfig(&config.PostgresConfig{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
				SslMode: v[fkSslMode],
			})
		},
	},
	KindRedis: {
		fields: fieldsFor(fieldSpec{key: fkDbNum, label: "db number"}),
		prefill: func() []string {
			c, err := config.ReadRedisConfig()
			if err != nil {
				c = &config.RedisConfig{}
			}
			return []string{c.UserName, c.Password, c.Host, c.Port, c.RdbNum}
		},
		dial: func(v map[string]string) (db.Backend, db.KVBackend, error) {
			kv, err := redisbe.Connect(redisbe.Config{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], RdbNum: v[fkDbNum],
			})
			if err != nil {
				return nil, nil, err
			}
			return nil, kv, nil
		},
		save: func(v map[string]string) error {
			return config.WriteRedisConfig(&config.RedisConfig{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], RdbNum: v[fkDbNum],
			})
		},
	},
	KindOpenObserve: {
		fields: fieldsFor(fieldSpec{key: fkOrg, label: "org"}),
		prefill: func() []string {
			c := config.ReadOpenObserveConfig()
			host, port, org := c.Host, c.Port, c.Org
			if host == "" {
				host = "localhost"
			}
			if port == "" {
				port = "5080"
			}
			if org == "" {
				org = "default"
			}
			return []string{c.UserName, c.Password, host, port, org}
		},
		dial: func(v map[string]string) (db.Backend, db.KVBackend, error) {
			be, err := oobe.Connect(oobe.Config{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], Org: v[fkOrg],
			})
			if err != nil {
				return nil, nil, err
			}
			return be, nil, nil
		},
		save: func(v map[string]string) error {
			return config.WriteOpenObserveConfig(config.OpenObserveConfig{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], Org: v[fkOrg],
			})
		},
	},
	KindSqlServer: {
		fields: fieldsFor(fieldSpec{key: fkDatabase, label: "database"}),
		prefill: func() []string {
			c, err := config.ReadSqlServerConfig()
			if err != nil {
				c = &config.SqlServerConfig{}
			}
			return []string{c.UserName, c.Password, c.Host, c.Port, c.DbName}
		},
		dial: func(v map[string]string) (db.Backend, db.KVBackend, error) {
			be, err := sqlserverbe.Connect(sqlserverbe.Config{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			})
			if err != nil {
				return nil, nil, err
			}
			return be, nil, nil
		},
		save: func(v map[string]string) error {
			return config.WriteSqlServerConfig(&config.SqlServerConfig{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			})
		},
	},
	KindOracle: {
		fields: fieldsFor(fieldSpec{key: fkDatabase, label: "service name"}),
		prefill: func() []string {
			c, err := config.ReadOracleConfig()
			if err != nil {
				c = &config.OracleConfig{}
			}
			return []string{c.UserName, c.Password, c.Host, c.Port, c.DbName}
		},
		dial: func(v map[string]string) (db.Backend, db.KVBackend, error) {
			be, err := oraclebe.Connect(oraclebe.Config{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			})
			if err != nil {
				return nil, nil, err
			}
			return be, nil, nil
		},
		save: func(v map[string]string) error {
			return config.WriteOracleConfig(&config.OracleConfig{
				UserName: v[fkUser], Password: v[fkPassword],
				Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			})
		},
	},
}

// values snapshots the current field contents keyed by field key. Masked
// fields (passwords) are passed through verbatim: credentials may
// legitimately contain leading/trailing whitespace.
func (o *connForm) values() map[string]string {
	m := make(map[string]string, len(o.fields))
	for _, fl := range o.fields {
		s := fl.input.String()
		if !fl.masked {
			s = strings.TrimSpace(s)
		}
		m[fl.key] = s
	}
	return m
}

// dialer opens connections for startConnect; a package variable so tests can
// swap in a fake connector and drive the full async connect flow without a
// live server.
var dialer = dial

// dial opens the connection for one kind. Exactly one of the returned
// backends is non-nil on success; whoever attaches it (the app, via
// SetBackendMsg) owns it and closes it on replacement or exit.
func dial(kind string, v map[string]string) (db.Backend, db.KVBackend, error) {
	if spec, ok := engineSpecs[kind]; ok {
		return spec.dial(v)
	}
	return nil, nil, fmt.Errorf("unknown database kind: %s", kind)
}

// saveConfig persists the form values with the existing per-engine writers.
func saveConfig(kind string, v map[string]string) error {
	if spec, ok := engineSpecs[kind]; ok {
		return spec.save(v)
	}
	return fmt.Errorf("unknown database kind: %s", kind)
}

// Fullscreen marks the form as a full page (ui.FullscreenOverlay): a
// centered box over a fully blanked body. It is the root page of the
// database-mode page stack (login -> browser -> table).
func (o *connForm) Fullscreen() bool { return true }

// startConnect flips into the connecting state and returns the asynchronous
// dial command. A second request while one is in flight is ignored.
func (o *connForm) startConnect() tea.Cmd {
	if o.connecting {
		return nil
	}
	o.connecting = true
	o.errText = ""
	kind, vals := o.kind, o.values()
	return func() tea.Msg {
		be, kv, err := dialer(kind, vals)
		return connDoneMsg{kind: kind, vals: vals, be: be, kv: kv, err: err}
	}
}

// successCmds is the ordered message sequence after a successful connect:
// close the form, attach the connection to the app, open the browser
// (schema for SQL engines, key browser via the wrapped schema factory for
// redis) and toast the connection title.
func (o *connForm) successCmds(m connDoneMsg) []tea.Cmd {
	title := ""
	if m.be != nil {
		title = m.be.Title()
	}
	if m.kv != nil {
		title = m.kv.Title()
	}
	be, kv := m.be, m.kv
	return []tea.Cmd{
		ui.CloseOverlay,
		func() tea.Msg { return ui.SetBackendMsg{Backend: be, KV: kv} },
		func() tea.Msg { return ui.RunCommandMsg{Name: "schema"} },
		func() tea.Msg { return ui.ToastMsg{Text: "connected: " + title} },
	}
}

func (o *connForm) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	switch m := msg.(type) {
	case connDoneMsg:
		o.connecting = false
		if m.err != nil {
			o.errText = m.err.Error()
			return o, nil
		}
		_ = saveConfig(m.kind, m.vals) // legacy behavior: persist on success
		return o, tea.Sequence(o.successCmds(m)...)

	case tea.PasteMsg:
		o.insertText(m.Content)
		return o, nil

	case tea.KeyPressMsg:
		return o.handleKey(m)
	}
	return o, nil
}

// insertText appends printable text to the focused field. Newlines (and other
// control characters) are stripped so multi-line pastes land as one value.
func (o *connForm) insertText(s string) {
	if len(o.fields) == 0 {
		return
	}
	o.fields[o.focus].input.InsertKey(s)
}

func (o *connForm) handleKey(key tea.KeyPressMsg) (ui.Overlay, tea.Cmd) {
	switch key.String() {
	case "esc":
		// While a dial is in flight the form must stay up: dismissing it
		// would drop the connDoneMsg (the freshly dialed backend replaces —
		// and closes — the one the app is still using), and reopening the
		// form could start a second concurrent dial.
		if o.connecting {
			return o, nil
		}
		// The form is the root page of the database-mode page stack: esc
		// always quits, connected or not.
		return o, tea.Quit
	case "ctrl+c":
		return o, tea.Quit
	case "tab", "down":
		o.focus = (o.focus + 1) % len(o.fields)
		return o, nil
	case "shift+tab", "up":
		o.focus = (o.focus - 1 + len(o.fields)) % len(o.fields)
		return o, nil
	case "enter", "ctrl+r":
		return o, o.startConnect()
	case "ctrl+s":
		if err := saveConfig(o.kind, o.values()); err != nil {
			o.errText = "save config: " + err.Error()
			return o, nil
		}
		return o, func() tea.Msg { return ui.ToastMsg{Text: "config saved"} }
	case "backspace":
		o.fields[o.focus].input.Backspace()
		return o, nil
	case "ctrl+u":
		o.fields[o.focus].input.Clear()
		return o, nil
	}
	if t := key.Text; t != "" {
		o.insertText(t)
	}
	return o, nil
}

func (o *connForm) View(width, height int, th *theme.Theme) string {
	w := width - 4
	if w > 60 {
		w = 60
	}
	if w < 30 {
		w = 30
	}
	inner := w - 2

	labelW := 0
	for _, fl := range o.fields {
		if n := len(fl.label); n > labelW {
			labelW = n
		}
	}

	var lines []string
	for i, fl := range o.fields {
		val := fl.input.String()
		if fl.masked {
			val = strings.Repeat("*", fl.input.Len())
		}
		label := fmt.Sprintf(" %-*s ", labelW, fl.label)
		room := inner - ansi.StringWidth(label) - 1
		if room < 1 {
			room = 1
		}
		// Keep the tail visible while typing long values.
		if r := []rune(val); len(r) > room {
			val = "…" + string(r[len(r)-room+1:])
		}
		if i == o.focus {
			cur := th.ListSelected.Render(" ")
			lines = append(lines, th.Text.Render(label)+th.Input.Render(val)+cur)
		} else {
			lines = append(lines, th.Subtle.Render(label)+th.Text.Render(val))
		}
	}

	lines = append(lines, "")
	switch {
	case o.connecting:
		lines = append(lines, th.Warning.Render(" connecting..."))
	case o.errText != "":
		for _, l := range strings.Split(ansi.Wrap(o.errText, inner-2, ""), "\n") {
			lines = append(lines, th.Error.Render(" "+l))
		}
	default:
		lines = append(lines, th.Placeholder.Render(" fill in the connection parameters"))
	}

	lines = append(lines, th.Placeholder.Render(" enter connect · tab/↑↓ move · ctrl+s save · esc quit"))

	return ui.Box("connect "+o.kind, strings.Join(lines, "\n"), w, th)
}

// compile-time interface checks
var (
	_ ui.Overlay           = (*connForm)(nil)
	_ ui.FullscreenOverlay = (*connForm)(nil)
)
