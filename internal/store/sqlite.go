package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无 CGO，Windows 交叉编译友好
)

// ConfigDir 返回应用配置目录（数据库、known_hosts、会话日志都在这里）
func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "." // 极端场景回退当前目录，保证可运行
	}
	return filepath.Join(dir, "Panoramix")
}

// DefaultPath 返回默认数据库文件路径（跟随系统配置目录）
func DefaultPath() string {
	return filepath.Join(ConfigDir(), "panoramix.db")
}

type sqliteStore struct {
	db *sql.DB
}

// Open 打开（必要时创建）SQLite 存储并执行 schema 迁移
func Open(path string) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc 驱动多连接并发写易触发 SQLITE_BUSY：单连接 + WAL + busy_timeout 规避
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	s := &sqliteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	log.Printf("[store] sqlite opened: %s", path)
	return s, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

// migrate 建立/升级 schema（当前 v1；meta 表记录版本号，为后续升级预留）
func (s *sqliteStore) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create meta: %w", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS groups (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			parent_id  TEXT NOT NULL DEFAULT '',
			sort       INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS hosts (
			id                TEXT PRIMARY KEY,
			group_id          TEXT NOT NULL DEFAULT '',
			name              TEXT NOT NULL,
			addr              TEXT NOT NULL,
			port              INTEGER NOT NULL DEFAULT 22,
			username          TEXT NOT NULL DEFAULT '',
			auth_type         TEXT NOT NULL DEFAULT 'password',
			key_path          TEXT NOT NULL DEFAULT '',
			init_commands     TEXT NOT NULL DEFAULT '[]',
			login_script      TEXT NOT NULL DEFAULT '[]',
			proxy_chain       TEXT NOT NULL DEFAULT '[]',
			keepalive_sec     INTEGER NOT NULL DEFAULT 30,
			auto_reconnect    INTEGER NOT NULL DEFAULT 1,
			last_connected_at INTEGER NOT NULL DEFAULT 0,
			sort              INTEGER NOT NULL DEFAULT 0,
			created_at        INTEGER NOT NULL,
			updated_at        INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS snippets (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			content    TEXT NOT NULL,
			sort       INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("exec ddl: %w", err)
		}
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO meta(k, v) VALUES ('schema_version', '1')`); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	// v2 迁移：老库补 proxy_chain 列（SQLite 无 ADD COLUMN IF NOT EXISTS，
	// 用列存在性探测保证幂等）
	var version string
	_ = s.db.QueryRow(`SELECT v FROM meta WHERE k='schema_version'`).Scan(&version)
	if version < "2" {
		if !s.columnExists("hosts", "proxy_chain") {
			if _, err := s.db.Exec(
				`ALTER TABLE hosts ADD COLUMN proxy_chain TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migrate v2: %w", err)
			}
			log.Printf("[store] schema migrated to v2 (proxy_chain)")
		}
		if _, err := s.db.Exec(`UPDATE meta SET v='2' WHERE k='schema_version'`); err != nil {
			return fmt.Errorf("set schema version 2: %w", err)
		}
		version = "2"
	}
	// v3：login_script（expect 式登录脚本）
	if version < "3" {
		if !s.columnExists("hosts", "login_script") {
			if _, err := s.db.Exec(
				`ALTER TABLE hosts ADD COLUMN login_script TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migrate v3: %w", err)
			}
			log.Printf("[store] schema migrated to v3 (login_script)")
		}
		if _, err := s.db.Exec(`UPDATE meta SET v='3' WHERE k='schema_version'`); err != nil {
			return fmt.Errorf("set schema version 3: %w", err)
		}
	}
	return nil
}

// columnExists 探测表是否已有某列（PRAGMA table_info）
func (s *sqliteStore) columnExists(table, column string) bool {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// ---------- 分组 ----------

func (s *sqliteStore) ListGroups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT id, name, parent_id, sort, created_at, updated_at
		FROM groups ORDER BY sort, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		var created, updated int64
		if err := rows.Scan(&g.ID, &g.Name, &g.ParentID, &g.Sort, &created, &updated); err != nil {
			return nil, err
		}
		g.CreatedAt = time.Unix(created, 0)
		g.UpdatedAt = time.Unix(updated, 0)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveGroup(g *Group) error {
	now := time.Now().Unix()
	if g.ID == "" {
		g.ID = NewID()
		g.CreatedAt = time.Unix(now, 0)
		g.UpdatedAt = g.CreatedAt
		_, err := s.db.Exec(`INSERT INTO groups (id, name, parent_id, sort, created_at, updated_at)
			VALUES (?,?,?,?,?,?)`,
			g.ID, g.Name, g.ParentID, g.Sort, now, now)
		return err
	}
	g.UpdatedAt = time.Unix(now, 0)
	_, err := s.db.Exec(`UPDATE groups SET name=?, parent_id=?, sort=?, updated_at=? WHERE id=?`,
		g.Name, g.ParentID, g.Sort, now, g.ID)
	return err
}

func (s *sqliteStore) DeleteGroup(id string) error {
	res, err := s.db.Exec(`DELETE FROM groups WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ---------- 主机 ----------

const hostColumns = `id, group_id, name, addr, port, username, auth_type, key_path,
	init_commands, login_script, proxy_chain, keepalive_sec, auto_reconnect, last_connected_at, sort,
	created_at, updated_at`

func scanHost(scanner interface{ Scan(...any) error }) (Host, error) {
	var h Host
	var authType, initCmds, loginScript, proxyChain string
	var port, keepalive, autoRe, lastAt int64
	var created, updated int64
	if err := scanner.Scan(&h.ID, &h.GroupID, &h.Name, &h.Addr, &port, &h.Username,
		&authType, &h.KeyPath, &initCmds, &loginScript, &proxyChain, &keepalive, &autoRe, &lastAt, &h.Sort,
		&created, &updated); err != nil {
		return h, err
	}
	h.Port = int(port)
	h.KeepaliveSec = int(keepalive)
	h.AutoReconnect = autoRe == 1
	h.AuthType = AuthType(authType)
	h.LastConnectedAt = time.Unix(lastAt, 0)
	h.CreatedAt = time.Unix(created, 0)
	h.UpdatedAt = time.Unix(updated, 0)
	if err := json.Unmarshal([]byte(initCmds), &h.InitCommands); err != nil {
		// 脏数据兜底：解析失败按空列表处理，不阻塞启动
		h.InitCommands = []string{}
	}
	if err := json.Unmarshal([]byte(loginScript), &h.LoginScript); err != nil {
		h.LoginScript = []LoginStep{}
	}
	if err := json.Unmarshal([]byte(proxyChain), &h.ProxyChain); err != nil {
		h.ProxyChain = []string{}
	}
	return h, nil
}

func (s *sqliteStore) ListHosts() ([]Host, error) {
	rows, err := s.db.Query(`SELECT ` + hostColumns + ` FROM hosts ORDER BY sort, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveHost(h *Host) error {
	// 字段兜底：非法值归一为默认，保证库内数据始终可用
	if h.Port <= 0 {
		h.Port = 22
	}
	if h.KeepaliveSec <= 0 {
		h.KeepaliveSec = 30
	}
	if h.InitCommands == nil {
		h.InitCommands = []string{}
	}
	if h.ProxyChain == nil {
		h.ProxyChain = []string{}
	}
	if h.LoginScript == nil {
		h.LoginScript = []LoginStep{}
	}
	script, err := json.Marshal(h.LoginScript)
	if err != nil {
		return fmt.Errorf("marshal login script: %w", err)
	}
	cmds, err := json.Marshal(h.InitCommands)
	if err != nil {
		return fmt.Errorf("marshal init commands: %w", err)
	}
	chain, err := json.Marshal(h.ProxyChain)
	if err != nil {
		return fmt.Errorf("marshal proxy chain: %w", err)
	}
	now := time.Now().Unix()
	if h.ID == "" {
		h.ID = NewID()
		h.CreatedAt = time.Unix(now, 0)
		h.UpdatedAt = h.CreatedAt
		return insertHost(s.db, h, string(cmds), string(script), string(chain), now)
	}
	h.UpdatedAt = time.Unix(now, 0)
	res, err := s.db.Exec(`UPDATE hosts SET group_id=?, name=?, addr=?, port=?, username=?,
		auth_type=?, key_path=?, init_commands=?, login_script=?, proxy_chain=?, keepalive_sec=?, auto_reconnect=?,
		last_connected_at=?, sort=?, updated_at=? WHERE id=?`,
		h.GroupID, h.Name, h.Addr, h.Port, h.Username, string(h.AuthType),
		h.KeyPath, string(cmds), string(script), string(chain), h.KeepaliveSec, boolInt(h.AutoReconnect),
		h.LastConnectedAt.Unix(), h.Sort, now, h.ID)
	if err != nil {
		return err
	}
	// 非空 ID 但记录不存在（service 层为新主机预生成 ID 的场景）：按新建处理
	if n, _ := res.RowsAffected(); n == 0 {
		h.CreatedAt = time.Unix(now, 0)
		return insertHost(s.db, h, string(cmds), string(script), string(chain), now)
	}
	return nil
}

func insertHost(db *sql.DB, h *Host, cmds, script, chain string, now int64) error {
	_, err := db.Exec(`INSERT INTO hosts (`+hostColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		h.ID, h.GroupID, h.Name, h.Addr, h.Port, h.Username, string(h.AuthType),
		h.KeyPath, cmds, script, chain, h.KeepaliveSec, boolInt(h.AutoReconnect),
		h.LastConnectedAt.Unix(), h.Sort, now, now)
	return err
}

func (s *sqliteStore) TouchHost(id string, at time.Time) error {
	res, err := s.db.Exec(`UPDATE hosts SET last_connected_at=? WHERE id=?`, at.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqliteStore) DeleteHost(id string) error {
	res, err := s.db.Exec(`DELETE FROM hosts WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ---------- 命令片段 ----------

func (s *sqliteStore) ListSnippets() ([]Snippet, error) {
	rows, err := s.db.Query(`SELECT id, name, content, sort, created_at, updated_at
		FROM snippets ORDER BY sort, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snippet
	for rows.Next() {
		var sn Snippet
		var created, updated int64
		if err := rows.Scan(&sn.ID, &sn.Name, &sn.Content, &sn.Sort, &created, &updated); err != nil {
			return nil, err
		}
		sn.CreatedAt = time.Unix(created, 0)
		sn.UpdatedAt = time.Unix(updated, 0)
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveSnippet(sn *Snippet) error {
	now := time.Now().Unix()
	if sn.ID == "" {
		sn.ID = NewID()
		sn.CreatedAt = time.Unix(now, 0)
		sn.UpdatedAt = sn.CreatedAt
		_, err := s.db.Exec(`INSERT INTO snippets (id, name, content, sort, created_at, updated_at)
			VALUES (?,?,?,?,?,?)`,
			sn.ID, sn.Name, sn.Content, sn.Sort, now, now)
		return err
	}
	sn.UpdatedAt = time.Unix(now, 0)
	_, err := s.db.Exec(`UPDATE snippets SET name=?, content=?, sort=?, updated_at=? WHERE id=?`,
		sn.Name, sn.Content, sn.Sort, now, sn.ID)
	return err
}

func (s *sqliteStore) DeleteSnippet(id string) error {
	res, err := s.db.Exec(`DELETE FROM snippets WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ---------- 全局设置 ----------

func (s *sqliteStore) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *sqliteStore) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
