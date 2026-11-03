// Package db 提供数据库连接管理（MySQL / Redis）。
// 连接池按连接 ID 缓存复用；密码由 app 层从 keychain 解出传入，
// 仅存于内存（本包不落盘、不写日志）。
// 本包不感知 Wails（依赖方向见 ARCHITECTURE.md）。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/lemon-puls/panoramix/internal/store"
)

// Result 查询结果（只读网格展示用）
type Result struct {
	Columns    []string    `json:"columns"`
	Rows       [][]*string `json:"rows"` // nil 元素表示 SQL NULL
	Affected   int64       `json:"affected"`
	DurationMs int64       `json:"durationMs"`
	Truncated  bool        `json:"truncated"` // 超过行数上限被截断
}

// Manager 数据库连接池管理（MySQL 用 database/sql，Redis 用 go-redis）
type Manager struct {
	mu    sync.Mutex
	pools map[string]*sql.DB       // connID → MySQL 连接池
	rdb   map[string]*redis.Client // connID → Redis 客户端
}

func NewManager() *Manager {
	return &Manager{
		pools: map[string]*sql.DB{},
		rdb:   map[string]*redis.Client{},
	}
}

// buildDSN 构造 MySQL DSN（含超时与只读会话默认值）
func buildDSN(c store.DBConn, password string) string {
	cfg := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&timeout=10s&readTimeout=60s&writeTimeout=60s&charset=utf8mb4",
		c.Username, password,
		net.JoinHostPort(c.Addr, strconv.Itoa(c.Port)),
		c.DBName,
	)
	return cfg
}

// Open 建立（或复用）到指定数据库的连接池
func (m *Manager) Open(c store.DBConn, password string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.pools[c.ID]; ok {
		return db, nil
	}
	if c.Kind != store.DBMySQL {
		return nil, fmt.Errorf("unsupported kind: %s", c.Kind)
	}
	db, err := sql.Open("mysql", buildDSN(c, password))
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	// 连接池：最多 5 个连接，空闲 5 分钟回收
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	m.pools[c.ID] = db
	return db, nil
}

// Close 关闭并移除连接池
func (m *Manager) Close(id string) {
	m.mu.Lock()
	db := m.pools[id]
	delete(m.pools, id)
	m.mu.Unlock()
	if db != nil {
		_ = db.Close()
	}
}

// CloseAll 关闭全部连接池（应用退出）
func (m *Manager) CloseAll() {
	m.mu.Lock()
	pools := m.pools
	rdbs := m.rdb
	m.pools = map[string]*sql.DB{}
	m.rdb = map[string]*redis.Client{}
	m.mu.Unlock()
	for _, db := range pools {
		_ = db.Close()
	}
	for _, cli := range rdbs {
		_ = cli.Close()
	}
}

// CloseConn 关闭指定连接（MySQL 或 Redis）
func (m *Manager) CloseConn(id string) {
	m.Close(id)
	m.closeRedis(id)
}

// Test 测试连接（不缓存，用完即关）
func Test(c store.DBConn, password string) error {
	db, err := sql.Open("mysql", buildDSN(c, password))
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

// isQuery 判断语句是否返回结果集（按首个关键字粗判，够用且简单）
func isQuery(sqlText string) bool {
	s := strings.TrimSpace(sqlText)
	// 去掉前导注释
	for strings.HasPrefix(s, "--") || strings.HasPrefix(s, "/*") {
		if strings.HasPrefix(s, "--") {
			if idx := strings.IndexByte(s, '\n'); idx >= 0 {
				s = strings.TrimSpace(s[idx+1:])
				continue
			}
			return false
		}
		if idx := strings.Index(s, "*/"); idx >= 0 {
			s = strings.TrimSpace(s[idx+2:])
			continue
		}
		return false
	}
	switch strings.ToUpper(firstWord(s)) {
	case "SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN", "WITH":
		return true
	}
	return false
}

func firstWord(s string) string {
	if idx := strings.IndexAny(s, " \t\r\n("); idx > 0 {
		return s[:idx]
	}
	return s
}

// Query 执行 SQL。查询类返回结果集（最多 limit 行，超出标记 Truncated）；
// DML 返回影响行数。不自动执行 COUNT（大表会卡死，见 ROADMAP v0.3）。
func (m *Manager) Query(c store.DBConn, password, sqlText string, limit int) (*Result, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	if !isQuery(sqlText) {
		res, err := db.ExecContext(ctx, sqlText)
		if err != nil {
			return nil, fmt.Errorf("exec: %w", err)
		}
		affected, _ := res.RowsAffected()
		return &Result{Affected: affected, DurationMs: time.Since(start).Milliseconds()}, nil
	}

	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	out := &Result{Columns: cols}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if len(out.Rows) >= limit {
			out.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		row := make([]*string, len(cols))
		for i, v := range vals {
			if v == nil {
				continue // NULL 保持 nil
			}
			s := formatValue(v)
			row[i] = &s
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out, nil
}

// quoteIdent 反引号转义标识符（库名/表名），防止语法错误与注入
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// ListDatabases 列出全部数据库（SHOW DATABASES，按名称排序）
func (m *Manager) ListDatabases(c store.DBConn, password string) ([]string, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("show databases: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// TableInfo 表/视图概要（含行数估算，来自 information_schema）
type TableInfo struct {
	Name    string `json:"name"`
	IsView  bool   `json:"isView"`
	Rows    int64  `json:"rows"` // 估算行数（InnoDB 为近似值）
	Comment string `json:"comment"`
}

// ListTables 列出指定库的表与视图
func (m *Manager) ListTables(c store.DBConn, password, database string) ([]TableInfo, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := `SELECT TABLE_NAME, TABLE_TYPE, IFNULL(TABLE_ROWS,0), IFNULL(TABLE_COMMENT,'')
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME`
	rows, err := db.QueryContext(ctx, q, database)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	var out []TableInfo
	for rows.Next() {
		var t TableInfo
		var typ string
		if err := rows.Scan(&t.Name, &typ, &t.Rows, &t.Comment); err != nil {
			return nil, err
		}
		t.IsView = strings.EqualFold(typ, "VIEW")
		out = append(out, t)
	}
	return out, rows.Err()
}

// ColumnInfo 列定义（DESCRIBE 结果）
type ColumnInfo struct {
	Field   string  `json:"field"`
	Type    string  `json:"type"`
	Null    string  `json:"null"`
	Key     string  `json:"key"`
	Default *string `json:"default"`
	Extra   string  `json:"extra"`
	Comment string  `json:"comment"`
}

// DescribeTable 返回列定义（DESCRIBE + information_schema 的注释）
func (m *Manager) DescribeTable(c store.DBConn, password, database, table string) ([]ColumnInfo, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := "DESCRIBE " + quoteIdent(database) + "." + quoteIdent(table)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("describe: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []ColumnInfo
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		get := func(name string) string {
			for i, col := range cols {
				if strings.EqualFold(col, name) {
					if vals[i] == nil {
						return ""
					}
					return formatValue(vals[i])
				}
			}
			return ""
		}
		ci := ColumnInfo{
			Field: get("Field"), Type: get("Type"), Null: get("Null"),
			Key: get("Key"), Extra: get("Extra"), Comment: get("Comment"),
		}
		if d := get("Default"); strings.EqualFold(ci.Null, "YES") && d == "" {
			ci.Default = nil
		} else {
			ci.Default = &d
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

// ShowCreateTable 返回建表语句（表结构详情）
func (m *Manager) ShowCreateTable(c store.DBConn, password, database, table string) (string, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := "SHOW CREATE TABLE " + quoteIdent(database) + "." + quoteIdent(table)
	var name, ddl string
	if err := db.QueryRowContext(ctx, q).Scan(&name, &ddl); err != nil {
		return "", fmt.Errorf("show create table: %w", err)
	}
	return ddl, nil
}

// formatValue 把驱动返回的值转成展示字符串（时间/数字/二进制统一处理）
func formatValue(v any) string {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}
