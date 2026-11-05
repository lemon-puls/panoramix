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
	"sort"
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

// isQuery 判断语句是否返回结果集。
// 仅按首关键字判断不够：WITH ... UPDATE 会被当成查询，
// SELECT ... INTO OUTFILE 会把文件写入当成结果集（总监评审要求收紧）。
// 这里做一个轻量的顶层关键字扫描（跳过引号、反引号、注释与括号内内容）。
func isQuery(sqlText string) bool {
	words := scanTopKeywords(sqlText)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN":
		// SELECT ... INTO OUTFILE/DUMPFILE 属于写文件，必须走 Exec
		if hasPhrase(words, "INTO", "OUTFILE") || hasPhrase(words, "INTO", "DUMPFILE") {
			return false
		}
		return true
	case "WITH":
		// WITH ... UPDATE/INSERT/DELETE/REPLACE 是写操作
		for _, w := range words[1:] {
			switch w {
			case "UPDATE", "INSERT", "DELETE", "REPLACE":
				return false
			}
		}
		return true
	}
	return false
}

// scanTopKeywords 返回括号深度为 0、且不在字符串/注释中的大写关键字。
func scanTopKeywords(sqlText string) []string {
	var words []string
	var word strings.Builder
	depth := 0
	flush := func() {
		if word.Len() > 0 {
			if depth == 0 {
				words = append(words, strings.ToUpper(word.String()))
			}
			word.Reset()
		}
	}
	i, n := 0, len(sqlText)
	for i < n {
		c := sqlText[i]
		switch {
		case c == '\'' || c == '"':
			flush()
			quote := c
			i++
			for i < n {
				if sqlText[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if sqlText[i] == quote {
					if i+1 < n && sqlText[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '`':
			flush()
			i++
			for i < n && sqlText[i] != '`' {
				i++
			}
			if i < n {
				i++
			}
		case c == '-' && i+1 < n && sqlText[i+1] == '-':
			flush()
			for i < n && sqlText[i] != '\n' {
				i++
			}
		case c == '#':
			flush()
			for i < n && sqlText[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sqlText[i+1] == '*':
			flush()
			i += 2
			for i+1 < n && !(sqlText[i] == '*' && sqlText[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			}
		case c == '(':
			flush()
			depth++
			i++
		case c == ')':
			flush()
			if depth > 0 {
				depth--
			}
			i++
		case c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c >= 0x80:
			word.WriteByte(c)
			i++
		default:
			flush()
			i++
		}
	}
	flush()
	return words
}

func hasPhrase(words []string, phrase ...string) bool {
	if len(phrase) == 0 || len(words) < len(phrase) {
		return false
	}
	for i := 0; i+len(phrase) <= len(words); i++ {
		ok := true
		for j, p := range phrase {
			if words[i+j] != p {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
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
	out, err := scanRows(rows, limit)
	if err != nil {
		return nil, err
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out, nil
}

// queryArgs 执行参数化查询（值全部走 ? 占位符）
func queryArgs(ctx context.Context, db *sql.DB, sqlText string, args []any, limit int) (*Result, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return scanRows(rows, limit)
}

// scanRows 扫描结果集（Query 与 TableData 共用）。调用方负责关闭 rows。
func scanRows(rows *sql.Rows, limit int) (*Result, error) {
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
	return out, nil
}

// quoteIdent 反引号转义标识符（库名/表名），防止语法错误与注入
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// ---------- 数据标签单元格写入（校验 + 参数化 + 事务） ----------

// RowUpdate 一行更新：PK 用于主键定位，Set 为要写入的列。
// map 的 value 为 nil 表示 SQL NULL。所有值都通过占位符传递，
// 不在 Go 侧拼接字符串字面量 —— 反斜杠/单引号/Unicode/NUL 均不会被解释。
type RowUpdate struct {
	PK  map[string]*string `json:"pk"`
	Set map[string]*string `json:"set"`
}

// maxRowUpdates 单次批量更新上限，避免一次请求锁住长事务
const maxRowUpdates = 1000

// codedError 构造带稳定错误码的错误（格式：code: message）。
// message 里只允许出现表名/列名等标识符，严禁拼接单元格值。
func codedError(code, format string, args ...any) error {
	return queryErrf(code, format, args...)
}

// sortedStringKeys 返回 map 的有序键（生成 SQL / 错误消息时保持确定性）
func sortedStringKeys(m map[string]*string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedBoolKeys 返回集合的有序键
func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// buildRowUpdateSQL 生成参数化的 UPDATE 语句与参数。
// 库名/表名/列名全部反引号转义；值（包括主键值）全部走 `?` 占位符。
// 主键值为 nil 时使用 `IS NULL`：MySQL 中 `col = NULL` 永不匹配。
func buildRowUpdateSQL(database, table string, u RowUpdate) (string, []any, error) {
	database = strings.TrimSpace(database)
	table = strings.TrimSpace(table)
	if database == "" || table == "" {
		return "", nil, codedError(ErrCodeApplyEditsFailed, "database and table are required")
	}
	if len(u.Set) == 0 {
		return "", nil, codedError(ErrCodeApplyEditsFailed, "no columns to update")
	}
	if len(u.PK) == 0 {
		return "", nil, codedError(ErrCodeApplyEditsFailed, "primary key is required")
	}

	// map 遍历顺序随机；排序列名让生成 SQL 稳定、可断言、便于日志排查
	setCols := sortedStringKeys(u.Set)
	pkCols := sortedStringKeys(u.PK)

	var b strings.Builder
	args := make([]any, 0, len(setCols)+len(pkCols))
	b.WriteString("UPDATE ")
	b.WriteString(quoteIdent(database))
	b.WriteByte('.')
	b.WriteString(quoteIdent(table))
	b.WriteString(" SET ")
	for i, col := range setCols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteIdent(col))
		b.WriteString(" = ?")
		args = append(args, nullableArg(u.Set[col]))
	}
	b.WriteString(" WHERE ")
	for i, col := range pkCols {
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString(quoteIdent(col))
		if v := u.PK[col]; v == nil {
			b.WriteString(" IS NULL")
		} else {
			b.WriteString(" = ?")
			args = append(args, *v)
		}
	}
	return b.String(), args, nil
}

// nullableArg 把 *string 转成数据库参数：nil → SQL NULL
func nullableArg(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// validateRowUpdates 在任何 Exec 之前校验更新集合。
// 关键约束：PK 键集合必须与表的主键列完全一致 —— 只传复合主键的一部分
// 会生成范围过宽的 WHERE，一次更新多行（QA 已在真实 MySQL 8.4 复现）。
func validateRowUpdates(columns, pkColumns map[string]bool, updates []RowUpdate) error {
	if len(updates) > maxRowUpdates {
		return codedError(ErrCodeTooManyEdits, "got %d row updates, max %d", len(updates), maxRowUpdates)
	}
	if len(pkColumns) == 0 {
		return codedError(ErrCodeApplyEditsFailed, "table has no primary key")
	}
	tablePK := sortedBoolKeys(pkColumns)
	for i, u := range updates {
		n := i + 1
		if len(u.Set) == 0 {
			return codedError(ErrCodeApplyEditsFailed, "update #%d has no columns to set", n)
		}
		if len(u.PK) == 0 {
			return codedError(ErrCodeApplyEditsFailed, "update #%d has an empty primary key", n)
		}
		// 先判断列是否存在，避免把“拼错列名”误报成主键不匹配
		for _, col := range sortedStringKeys(u.PK) {
			if !columns[col] {
				return codedError(ErrCodeApplyEditsFailed, "update #%d: primary key column %q does not exist", n, col)
			}
		}
		for _, col := range sortedStringKeys(u.Set) {
			if !columns[col] {
				return codedError(ErrCodeApplyEditsFailed, "update #%d: column %q does not exist", n, col)
			}
		}
		for _, col := range sortedStringKeys(u.PK) {
			if !pkColumns[col] {
				return codedError(ErrCodeApplyEditsFailed, "update #%d: column %q is not a primary key column", n, col)
			}
		}
		if len(u.PK) != len(pkColumns) {
			return codedError(ErrCodeApplyEditsFailed,
				"update #%d: primary key columns mismatch (table: %s; got: %s)",
				n, strings.Join(tablePK, ","), strings.Join(sortedStringKeys(u.PK), ","))
		}
	}
	return nil
}

// queryFullColumns 读取列的完整定义（SHOW FULL COLUMNS，含 Comment）。
// DESCRIBE 不返回 Comment，列面板的注释搜索/提示依赖这里。
func queryFullColumns(ctx context.Context, db *sql.DB, database, table string) ([]ColumnInfo, error) {
	q := "SHOW FULL COLUMNS FROM " + quoteIdent(database) + "." + quoteIdent(table)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, codedError(ErrCodeTableMetadataUnavailable, "show full columns: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, codedError(ErrCodeTableMetadataUnavailable, "columns: %v", err)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var out [][]any
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, codedError(ErrCodeTableMetadataUnavailable, "scan: %v", err)
		}
		row := make([]any, len(cols))
		copy(row, vals)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, codedError(ErrCodeTableMetadataUnavailable, "rows: %v", err)
	}
	return parseColumnRows(cols, out), nil
}

// parseColumnRows 把 SHOW FULL COLUMNS 的行转换成 ColumnInfo（纯函数，便于单测）。
// Default 必须保留 SQL NULL 与空字符串的区别：nil → nil；” → 指向空串的指针。
func parseColumnRows(cols []string, rows [][]any) []ColumnInfo {
	idx := func(name string) int {
		for i, c := range cols {
			if strings.EqualFold(c, name) {
				return i
			}
		}
		return -1
	}
	fieldIdx := idx("Field")
	typeIdx := idx("Type")
	nullIdx := idx("Null")
	keyIdx := idx("Key")
	defIdx := idx("Default")
	extraIdx := idx("Extra")
	commentIdx := idx("Comment")

	out := make([]ColumnInfo, 0, len(rows))
	for _, row := range rows {
		get := func(i int) string {
			if i < 0 || i >= len(row) || row[i] == nil {
				return ""
			}
			return formatValue(row[i])
		}
		ci := ColumnInfo{
			Field:   get(fieldIdx),
			Type:    get(typeIdx),
			Null:    get(nullIdx),
			Key:     get(keyIdx),
			Extra:   get(extraIdx),
			Comment: get(commentIdx),
		}
		if defIdx >= 0 && defIdx < len(row) && row[defIdx] != nil {
			d := formatValue(row[defIdx])
			ci.Default = &d
		}
		if ci.Field == "" {
			continue
		}
		out = append(out, ci)
	}
	return out
}

// loadTableColumns 通过 SHOW FULL COLUMNS 读取全部列与主键列（Key == 'PRI'）。
// 必须在任何 UPDATE 执行前调用：没有主键信息就无法保证 WHERE 只命中一行。
func loadTableColumns(ctx context.Context, db *sql.DB, database, table string) (map[string]bool, map[string]bool, error) {
	cols, err := queryFullColumns(ctx, db, database, table)
	if err != nil {
		return nil, nil, err
	}
	all := map[string]bool{}
	pks := map[string]bool{}
	for _, c := range cols {
		all[c.Field] = true
		if strings.EqualFold(c.Key, "PRI") {
			pks[c.Field] = true
		}
	}
	return all, pks, nil
}

// applyRowUpdates 在同一事务内执行全部 UPDATE。
// 任一语句失败即回滚并返回 (0, err)：绝不留下"部分行已写入"的状态。
// 返回值为实际执行的 UPDATE 语句数（每行一条）。
func applyRowUpdates(ctx context.Context, db *sql.DB, database, table string, updates []RowUpdate) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, codedError(ErrCodeApplyEditsFailed, "begin transaction: %v", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone，忽略即可
	defer func() { _ = tx.Rollback() }()

	var applied int64
	for i, u := range updates {
		stmt, args, err := buildRowUpdateSQL(database, table, u)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return 0, codedError(ErrCodeApplyEditsFailed, "update #%d: %v", i+1, err)
		}
		applied++
	}
	if err := tx.Commit(); err != nil {
		return 0, codedError(ErrCodeApplyEditsFailed, "commit: %v", err)
	}
	return applied, nil
}

// updateRowsOnDB 读取元数据 → 校验 → 在事务中执行。
// 校验失败时不会执行任何 UPDATE（也不开启事务）。
func updateRowsOnDB(ctx context.Context, db *sql.DB, database, table string, updates []RowUpdate) (int64, error) {
	database = strings.TrimSpace(database)
	table = strings.TrimSpace(table)
	if database == "" || table == "" {
		return 0, codedError(ErrCodeApplyEditsFailed, "database and table are required")
	}
	columns, pkColumns, err := loadTableColumns(ctx, db, database, table)
	if err != nil {
		return 0, err
	}
	if err := validateRowUpdates(columns, pkColumns, updates); err != nil {
		return 0, err
	}
	return applyRowUpdates(ctx, db, database, table, updates)
}

// UpdateRows 按主键批量更新单元格。整批放在一个事务内：
// 任一语句失败则全部回滚，前端不会看到"应用了 X/Y"的中间状态。
// 执行前先用 SHOW FULL COLUMNS 校验列与主键集合。
func (m *Manager) UpdateRows(c store.DBConn, password, database, table string, updates []RowUpdate) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	if len(updates) > maxRowUpdates {
		return 0, codedError(ErrCodeTooManyEdits, "got %d row updates, max %d", len(updates), maxRowUpdates)
	}
	if strings.TrimSpace(database) == "" || strings.TrimSpace(table) == "" {
		return 0, codedError(ErrCodeApplyEditsFailed, "database and table are required")
	}
	db, err := m.Open(c, password)
	if err != nil {
		return 0, codedError(ErrCodeApplyEditsFailed, "open connection: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return updateRowsOnDB(ctx, db, database, table, updates)
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
	// SHOW FULL COLUMNS 是 DESCRIBE 的直接替代，额外返回 Comment；
	// 列面板的注释搜索/提示必须依赖它（DESCRIBE 没有 Comment 列）。
	return queryFullColumns(ctx, db, database, table)
}

// TableMeta 表基本信息（HeidiSQL「基本」标签）
type TableMeta struct {
	Name          string `json:"name"`
	Engine        string `json:"engine"`
	Collation     string `json:"collation"`
	Comment       string `json:"comment"`
	CreateTime    string `json:"createTime"`
	Rows          int64  `json:"rows"`
	DataLength    int64  `json:"dataLength"`
	IndexLength   int64  `json:"indexLength"`
	AutoIncrement int64  `json:"autoIncrement"`
}

// IndexInfo 索引（HeidiSQL「索引」标签）
type IndexInfo struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

// ForeignKeyInfo 外键（HeidiSQL「外键」标签）
type ForeignKeyInfo struct {
	Name      string `json:"name"`
	Column    string `json:"column"`
	RefTable  string `json:"refTable"`
	RefColumn string `json:"refColumn"`
	OnUpdate  string `json:"onUpdate"`
	OnDelete  string `json:"onDelete"`
}

// TableMetaInfo 读取表基本信息
func (m *Manager) TableMetaInfo(c store.DBConn, password, database, table string) (*TableMeta, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := `SELECT TABLE_NAME, IFNULL(ENGINE,''), IFNULL(TABLE_COLLATION,''),
		IFNULL(TABLE_COMMENT,''), IFNULL(DATE_FORMAT(CREATE_TIME,'%Y-%m-%d %H:%i:%s'),''),
		IFNULL(TABLE_ROWS,0), IFNULL(DATA_LENGTH,0), IFNULL(INDEX_LENGTH,0), IFNULL(AUTO_INCREMENT,0)
		FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=?`
	var meta TableMeta
	err = db.QueryRowContext(ctx, q, database, table).Scan(
		&meta.Name, &meta.Engine, &meta.Collation, &meta.Comment,
		&meta.CreateTime, &meta.Rows, &meta.DataLength, &meta.IndexLength, &meta.AutoIncrement)
	if err != nil {
		return nil, fmt.Errorf("table meta: %w", err)
	}
	return &meta, nil
}

// ListIndexes 列出表索引（按索引名聚合列）
func (m *Manager) ListIndexes(c store.DBConn, password, database, table string) ([]IndexInfo, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := `SELECT INDEX_NAME, INDEX_TYPE, NON_UNIQUE, COLUMN_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=? AND TABLE_NAME=?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`
	rows, err := db.QueryContext(ctx, q, database, table)
	if err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	defer rows.Close()
	byName := map[string]*IndexInfo{}
	var order []string
	for rows.Next() {
		var name, typ, col string
		var nonUnique int64
		if err := rows.Scan(&name, &typ, &nonUnique, &col); err != nil {
			return nil, err
		}
		idx, ok := byName[name]
		if !ok {
			idx = &IndexInfo{Name: name, Type: typ, Unique: nonUnique == 0}
			byName[name] = idx
			order = append(order, name)
		}
		idx.Columns = append(idx.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]IndexInfo, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out, nil
}

// ListForeignKeys 列出外键
func (m *Manager) ListForeignKeys(c store.DBConn, password, database, table string) ([]ForeignKeyInfo, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_NAME,
		k.REFERENCED_COLUMN_NAME, IFNULL(r.UPDATE_RULE,''), IFNULL(r.DELETE_RULE,'')
		FROM information_schema.KEY_COLUMN_USAGE k
		LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS r
		  ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME
		WHERE k.TABLE_SCHEMA=? AND k.TABLE_NAME=? AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.CONSTRAINT_NAME, k.ORDINAL_POSITION`
	rows, err := db.QueryContext(ctx, q, database, table)
	if err != nil {
		return nil, fmt.Errorf("list fks: %w", err)
	}
	defer rows.Close()
	var out []ForeignKeyInfo
	for rows.Next() {
		var fk ForeignKeyInfo
		if err := rows.Scan(&fk.Name, &fk.Column, &fk.RefTable, &fk.RefColumn, &fk.OnUpdate, &fk.OnDelete); err != nil {
			return nil, err
		}
		out = append(out, fk)
	}
	return out, rows.Err()
}

// ShowCreateObject 返回表或视图的建表/建视图语句。
//
// 注意：MySQL 的 SHOW CREATE VIEW 返回 4 列（View / Create View /
// character_set_client / collation_connection），与 SHOW CREATE TABLE 的 2 列不同；
// 旧实现固定 Scan 2 列，遇到视图必然失败（总监评审必须修改项 5）。
func (m *Manager) ShowCreateObject(c store.DBConn, password, database, table string) (string, bool, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return "", false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var typ string
	err = db.QueryRowContext(ctx,
		`SELECT TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=?`,
		database, table).Scan(&typ)
	if err != nil {
		return "", false, fmt.Errorf("show create object: %w", err)
	}
	if strings.EqualFold(typ, "VIEW") {
		rows, err := db.QueryContext(ctx, "SHOW CREATE VIEW "+quoteIdent(database)+"."+quoteIdent(table))
		if err != nil {
			return "", true, fmt.Errorf("show create view: %w", err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return "", true, fmt.Errorf("show create view columns: %w", err)
		}
		if !rows.Next() {
			return "", true, fmt.Errorf("show create view: no row")
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", true, fmt.Errorf("show create view scan: %w", err)
		}
		ddl := pickCreateViewDDL(cols, vals)
		if ddl == "" {
			return "", true, fmt.Errorf("show create view: empty DDL")
		}
		return ddl, true, nil
	}

	var name, ddl string
	q := "SHOW CREATE TABLE " + quoteIdent(database) + "." + quoteIdent(table)
	if err := db.QueryRowContext(ctx, q).Scan(&name, &ddl); err != nil {
		return "", false, fmt.Errorf("show create table: %w", err)
	}
	return ddl, false, nil
}

// pickCreateViewDDL 从 SHOW CREATE VIEW 的 4 列结果中取出 Create View 列。
// 单独抽出来便于单元测试（视图返回列与表不同，是总监评审点名的历史缺陷）。
func pickCreateViewDDL(cols []string, vals []any) string {
	for i, col := range cols {
		if strings.EqualFold(col, "Create View") && i < len(vals) && vals[i] != nil {
			return formatValue(vals[i])
		}
	}
	return ""
}

// ShowCreateTable 返回建表语句（表结构详情；视图也能正确解析）
func (m *Manager) ShowCreateTable(c store.DBConn, password, database, table string) (string, error) {
	ddl, _, err := m.ShowCreateObject(c, password, database, table)
	return ddl, err
}

// ServerVersion 返回 MySQL 服务端版本（AI 上下文用；不包含任何连接凭据）
func (m *Manager) ServerVersion(c store.DBConn, password string) (string, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var v string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&v); err != nil {
		return "", fmt.Errorf("server version: %w", err)
	}
	return v, nil
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
