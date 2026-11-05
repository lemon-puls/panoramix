package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lemon-puls/panoramix/internal/store"
)

// ---------- 参数化 UPDATE 的 SQL/参数断言 ----------

func strPtr(v string) *string { return &v }

func TestBuildRowUpdateSQLParameterizesValues(t *testing.T) {
	cases := []struct {
		name     string
		database string
		table    string
		update   RowUpdate
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "backslash is passed as parameter, not escaped literal",
			database: "app",
			table:    "users",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("1")},
				Set: map[string]*string{"path": strPtr(`C:\temp\new`)},
			},
			wantSQL:  "UPDATE `app`.`users` SET `path` = ? WHERE `id` = ?",
			wantArgs: []any{`C:\temp\new`, "1"},
		},
		{
			name:     "single quote stays verbatim in parameter",
			database: "app",
			table:    "users",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("7")},
				Set: map[string]*string{"name": strPtr("O'Reilly")},
			},
			wantSQL:  "UPDATE `app`.`users` SET `name` = ? WHERE `id` = ?",
			wantArgs: []any{"O'Reilly", "7"},
		},
		{
			name:     "unicode and emoji",
			database: "app",
			table:    "users",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("8")},
				Set: map[string]*string{"name": strPtr("中文 🚀")},
			},
			wantSQL:  "UPDATE `app`.`users` SET `name` = ? WHERE `id` = ?",
			wantArgs: []any{"中文 🚀", "8"},
		},
		{
			name:     "NUL byte is not truncated",
			database: "app",
			table:    "users",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("9")},
				Set: map[string]*string{"raw": strPtr("a\x00b")},
			},
			wantSQL:  "UPDATE `app`.`users` SET `raw` = ? WHERE `id` = ?",
			wantArgs: []any{"a\x00b", "9"},
		},
		{
			name:     "NULL set value uses nil parameter",
			database: "app",
			table:    "users",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("10")},
				Set: map[string]*string{"email": nil},
			},
			wantSQL:  "UPDATE `app`.`users` SET `email` = ? WHERE `id` = ?",
			wantArgs: []any{nil, "10"},
		},
		{
			name:     "composite primary key",
			database: "app",
			table:    "memberships",
			update: RowUpdate{
				PK:  map[string]*string{"tenant_id": strPtr("t1"), "user_id": strPtr("u2")},
				Set: map[string]*string{"role": strPtr("admin")},
			},
			wantSQL:  "UPDATE `app`.`memberships` SET `role` = ? WHERE `tenant_id` = ? AND `user_id` = ?",
			wantArgs: []any{"admin", "t1", "u2"},
		},
		{
			name:     "NULL primary key uses IS NULL",
			database: "app",
			table:    "memberships",
			update: RowUpdate{
				PK:  map[string]*string{"tenant_id": nil, "user_id": strPtr("u2")},
				Set: map[string]*string{"role": strPtr("admin")},
			},
			wantSQL:  "UPDATE `app`.`memberships` SET `role` = ? WHERE `tenant_id` IS NULL AND `user_id` = ?",
			wantArgs: []any{"admin", "u2"},
		},
		{
			name:     "bigint primary key kept as exact string",
			database: "app",
			table:    "events",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("9223372036854775807")},
				Set: map[string]*string{"note": strPtr("max")},
			},
			wantSQL:  "UPDATE `app`.`events` SET `note` = ? WHERE `id` = ?",
			wantArgs: []any{"max", "9223372036854775807"},
		},
		{
			name:     "identifiers are backtick escaped",
			database: "we`ird",
			table:    "ta`ble",
			update: RowUpdate{
				PK:  map[string]*string{"i`d": strPtr("1")},
				Set: map[string]*string{"co`l": strPtr("v")},
			},
			wantSQL:  "UPDATE `we``ird`.`ta``ble` SET `co``l` = ? WHERE `i``d` = ?",
			wantArgs: []any{"v", "1"},
		},
		{
			name:     "multiple set columns sorted deterministically",
			database: "app",
			table:    "users",
			update: RowUpdate{
				PK:  map[string]*string{"id": strPtr("1")},
				Set: map[string]*string{"z": strPtr("1"), "a": strPtr("2")},
			},
			wantSQL:  "UPDATE `app`.`users` SET `a` = ?, `z` = ? WHERE `id` = ?",
			wantArgs: []any{"2", "1", "1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlText, args, err := buildRowUpdateSQL(tc.database, tc.table, tc.update)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sqlText != tc.wantSQL {
				t.Fatalf("sql mismatch\n got: %s\nwant: %s", sqlText, tc.wantSQL)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args length = %d, want %d (%#v)", len(args), len(tc.wantArgs), args)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Fatalf("arg[%d] = %#v, want %#v", i, args[i], tc.wantArgs[i])
				}
			}
			// 值绝不能出现在 SQL 文本里（NUL/反斜杠/引号都只走参数）
			for _, v := range tc.wantArgs {
				if s, ok := v.(string); ok && s != "" && strings.Contains(sqlText, s) {
					t.Fatalf("value %q leaked into SQL text: %s", s, sqlText)
				}
			}
		})
	}
}

func TestBuildRowUpdateSQLRejectsIncompleteUpdate(t *testing.T) {
	if _, _, err := buildRowUpdateSQL("", "t", RowUpdate{PK: map[string]*string{"id": strPtr("1")}, Set: map[string]*string{"a": strPtr("x")}}); err == nil {
		t.Fatal("empty database must be rejected")
	}
	if _, _, err := buildRowUpdateSQL("db", "", RowUpdate{PK: map[string]*string{"id": strPtr("1")}, Set: map[string]*string{"a": strPtr("x")}}); err == nil {
		t.Fatal("empty table must be rejected")
	}
	if _, _, err := buildRowUpdateSQL("db", "t", RowUpdate{PK: map[string]*string{"id": strPtr("1")}}); err == nil {
		t.Fatal("empty set must be rejected")
	}
	if _, _, err := buildRowUpdateSQL("db", "t", RowUpdate{Set: map[string]*string{"a": strPtr("x")}}); err == nil {
		t.Fatal("empty primary key must be rejected")
	}
}

// ---------- 事务语义（fake driver） ----------

type fakeExec struct {
	query string
	args  []driver.NamedValue
}

type fakeConn struct {
	mu        sync.Mutex
	execs     []fakeExec
	execN     int
	failOn    map[int]error // 第 N 条（1-based）Exec 返回错误
	commits   int
	rollbacks int
	// SHOW FULL COLUMNS 的模拟结果
	metaCols []string
	metaRows [][]driver.Value
	queries  []string
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return &fakeTx{conn: c}, nil }

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.execN++
	c.execs = append(c.execs, fakeExec{query: query, args: append([]driver.NamedValue(nil), args...)})
	if err, ok := c.failOn[c.execN]; ok {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queries = append(c.queries, query)
	if c.metaRows == nil {
		return nil, errors.New("no metadata configured")
	}
	return &fakeRows{cols: c.metaCols, rows: c.metaRows}, nil
}

type fakeRows struct {
	cols []string
	rows [][]driver.Value
	i    int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}

type metaField struct{ name, key string }

// newMetaConn 构造带 SHOW FULL COLUMNS 结果的 fake 连接
func newMetaConn(fields ...metaField) *fakeConn {
	cols := []string{"Field", "Type", "Collation", "Null", "Key", "Default", "Extra", "Privileges", "Comment"}
	rows := make([][]driver.Value, 0, len(fields))
	for _, f := range fields {
		rows = append(rows, []driver.Value{
			[]byte(f.name), []byte("varchar(64)"), []byte("utf8mb4"), []byte("NO"),
			[]byte(f.key), nil, []byte(""), []byte("select"), []byte(""),
		})
	}
	return &fakeConn{metaCols: cols, metaRows: rows}
}

func assertErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %q, got nil", code)
	}
	if !strings.HasPrefix(err.Error(), code+": ") {
		t.Fatalf("error %q does not start with %q", err.Error(), code+": ")
	}
}

type fakeTx struct{ conn *fakeConn }

func (tx *fakeTx) Commit() error {
	tx.conn.mu.Lock()
	defer tx.conn.mu.Unlock()
	tx.conn.commits++
	return nil
}

func (tx *fakeTx) Rollback() error {
	tx.conn.mu.Lock()
	defer tx.conn.mu.Unlock()
	tx.conn.rollbacks++
	return nil
}

type fakeDriver struct{ conn *fakeConn }

func (d *fakeDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

var fakeDriverSeq int64

// openFakeDB 注册一个独立的 fake driver 并返回 *sql.DB
func openFakeDB(t *testing.T, conn *fakeConn) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("panoramix-fake-update-%d", atomic.AddInt64(&fakeDriverSeq, 1))
	sql.Register(name, &fakeDriver{conn: conn})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestApplyRowUpdatesCommitsAll(t *testing.T) {
	conn := &fakeConn{}
	db := openFakeDB(t, conn)
	updates := []RowUpdate{
		{PK: map[string]*string{"id": strPtr("1")}, Set: map[string]*string{"a": strPtr(`C:\temp`)}},
		{PK: map[string]*string{"id": strPtr("2")}, Set: map[string]*string{"b": nil}},
	}
	applied, err := applyRowUpdates(context.Background(), db, "app", "users", updates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied != 2 {
		t.Fatalf("applied = %d, want 2", applied)
	}
	if conn.commits != 1 || conn.rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d, want 1/0", conn.commits, conn.rollbacks)
	}
	if len(conn.execs) != 2 {
		t.Fatalf("execs = %d, want 2", len(conn.execs))
	}
	// 反斜杠值必须原样作为参数，而不是被拼进 SQL
	if got := conn.execs[0].args[0].Value; got != `C:\temp` {
		t.Fatalf("arg = %#v, want %q", got, `C:\temp`)
	}
	if strings.Contains(conn.execs[0].query, `C:\temp`) {
		t.Fatalf("value leaked into SQL: %s", conn.execs[0].query)
	}
}

func TestApplyRowUpdatesRollsBackOnSecondFailure(t *testing.T) {
	conn := &fakeConn{failOn: map[int]error{2: errors.New("duplicate entry 'x' for key 'PRIMARY'")}}
	db := openFakeDB(t, conn)
	updates := []RowUpdate{
		{PK: map[string]*string{"id": strPtr("1")}, Set: map[string]*string{"a": strPtr("ok")}},
		{PK: map[string]*string{"id": strPtr("2")}, Set: map[string]*string{"a": strPtr("boom")}},
	}
	applied, err := applyRowUpdates(context.Background(), db, "app", "users", updates)
	if err == nil {
		t.Fatal("expected error")
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0 (all rolled back)", applied)
	}
	if conn.commits != 0 {
		t.Fatalf("commits = %d, want 0", conn.commits)
	}
	if conn.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", conn.rollbacks)
	}
	if !strings.Contains(err.Error(), "update #2") {
		t.Fatalf("error should identify failing statement: %v", err)
	}
	if len(conn.execs) != 2 {
		t.Fatalf("execs = %d, want 2 (first applied then rolled back)", len(conn.execs))
	}
}

func TestApplyRowUpdatesRejectsInvalidBeforeWriting(t *testing.T) {
	conn := &fakeConn{}
	db := openFakeDB(t, conn)
	// 第二条缺少主键：必须在执行任何 UPDATE 前失败
	updates := []RowUpdate{
		{PK: map[string]*string{"id": strPtr("1")}, Set: map[string]*string{"a": strPtr("ok")}},
		{Set: map[string]*string{"a": strPtr("bad")}},
	}
	applied, err := applyRowUpdates(context.Background(), db, "app", "users", updates)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	// 第一条已经执行，但事务回滚；关键是不提交
	if conn.commits != 0 || conn.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, want 0/1", conn.commits, conn.rollbacks)
	}
}

// ---------- 主键/列校验（QA 回归：复合主键只传一列会更新多行） ----------

func TestValidateRowUpdatesRejectsPartialCompositePK(t *testing.T) {
	cols := map[string]bool{"id": true, "tenant_id": true, "name": true}
	pks := map[string]bool{"id": true, "tenant_id": true}
	updates := []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1")}, // 少了 tenant_id
		Set: map[string]*string{"name": strPtr("x")},
	}}
	assertErrorCode(t, validateRowUpdates(cols, pks, updates), ErrCodeApplyEditsFailed)
}

func TestValidateRowUpdatesRejectsNonPKColumnInPK(t *testing.T) {
	cols := map[string]bool{"id": true, "tenant_id": true, "name": true}
	pks := map[string]bool{"id": true, "tenant_id": true}
	updates := []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1"), "name": strPtr("x")},
		Set: map[string]*string{"tenant_id": strPtr("t1")},
	}}
	assertErrorCode(t, validateRowUpdates(cols, pks, updates), ErrCodeApplyEditsFailed)
}

func TestValidateRowUpdatesRejectsUnknownColumns(t *testing.T) {
	cols := map[string]bool{"id": true, "name": true}
	pks := map[string]bool{"id": true}
	// set 列不存在
	assertErrorCode(t, validateRowUpdates(cols, pks, []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1")},
		Set: map[string]*string{"nope": strPtr("x")},
	}}), ErrCodeApplyEditsFailed)
	// 主键列不存在
	assertErrorCode(t, validateRowUpdates(cols, pks, []RowUpdate{{
		PK:  map[string]*string{"nope": strPtr("1")},
		Set: map[string]*string{"name": strPtr("x")},
	}}), ErrCodeApplyEditsFailed)
}

func TestValidateRowUpdatesRejectsTableWithoutPK(t *testing.T) {
	cols := map[string]bool{"id": true, "name": true}
	assertErrorCode(t, validateRowUpdates(cols, map[string]bool{}, []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1")},
		Set: map[string]*string{"name": strPtr("x")},
	}}), ErrCodeApplyEditsFailed)
}

func TestValidateRowUpdatesRejectsTooManyRows(t *testing.T) {
	cols := map[string]bool{"id": true, "name": true}
	pks := map[string]bool{"id": true}
	updates := make([]RowUpdate, maxRowUpdates+1)
	pk := map[string]*string{"id": strPtr("1")}
	set := map[string]*string{"name": strPtr("x")}
	for i := range updates {
		updates[i] = RowUpdate{PK: pk, Set: set}
	}
	assertErrorCode(t, validateRowUpdates(cols, pks, updates), ErrCodeTooManyEdits)
}

func TestValidateRowUpdatesAcceptsCompositePK(t *testing.T) {
	cols := map[string]bool{"id": true, "tenant_id": true, "name": true}
	pks := map[string]bool{"id": true, "tenant_id": true}
	err := validateRowUpdates(cols, pks, []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1"), "tenant_id": strPtr("t1")},
		Set: map[string]*string{"name": strPtr("x")},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateRowsOnDBRejectsPartialCompositePKBeforeExec(t *testing.T) {
	conn := newMetaConn(
		metaField{name: "id", key: "PRI"},
		metaField{name: "tenant_id", key: "PRI"},
		metaField{name: "name", key: ""},
	)
	db := openFakeDB(t, conn)
	updates := []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1")}, // 只传复合主键的一部分
		Set: map[string]*string{"name": strPtr("x")},
	}}
	applied, err := updateRowsOnDB(context.Background(), db, "app", "memberships", updates)
	assertErrorCode(t, err, ErrCodeApplyEditsFailed)
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if len(conn.execs) != 0 {
		t.Fatalf("no UPDATE may run before validation, got %d", len(conn.execs))
	}
	if len(conn.queries) != 1 || !strings.Contains(conn.queries[0], "SHOW FULL COLUMNS") {
		t.Fatalf("expected one SHOW FULL COLUMNS metadata query, got %#v", conn.queries)
	}
}

func TestUpdateRowsOnDBRejectsUnknownColumnBeforeExec(t *testing.T) {
	conn := newMetaConn(
		metaField{name: "id", key: "PRI"},
		metaField{name: "name", key: ""},
	)
	db := openFakeDB(t, conn)
	applied, err := updateRowsOnDB(context.Background(), db, "app", "users", []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1")},
		Set: map[string]*string{"missing_col": strPtr("x")},
	}})
	assertErrorCode(t, err, ErrCodeApplyEditsFailed)
	if applied != 0 || len(conn.execs) != 0 {
		t.Fatalf("applied=%d execs=%d, want 0/0", applied, len(conn.execs))
	}
}

func TestUpdateRowsOnDBWritesCompositePKWithBothColumns(t *testing.T) {
	conn := newMetaConn(
		metaField{name: "id", key: "PRI"},
		metaField{name: "tenant_id", key: "PRI"},
		metaField{name: "name", key: ""},
	)
	db := openFakeDB(t, conn)
	applied, err := updateRowsOnDB(context.Background(), db, "app", "memberships", []RowUpdate{{
		PK:  map[string]*string{"tenant_id": strPtr("t1"), "id": strPtr("1")},
		Set: map[string]*string{"name": strPtr("x")},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	if len(conn.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(conn.execs))
	}
	sqlText := conn.execs[0].query
	if !strings.Contains(sqlText, "WHERE `id` = ? AND `tenant_id` = ?") {
		t.Fatalf("composite PK not fully constrained: %s", sqlText)
	}
	args := conn.execs[0].args
	if len(args) != 3 || args[0].Value != "x" || args[1].Value != "1" || args[2].Value != "t1" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

// TestManagerUpdateRowsRejectsBeforeConnecting 验证公共入口在建立连接前就拒绝：
// 超限/空库名不依赖数据库即可返回稳定错误码。
func TestManagerUpdateRowsRejectsBeforeConnecting(t *testing.T) {
	m := NewManager()
	tooMany := make([]RowUpdate, maxRowUpdates+1)
	_, err := m.UpdateRows(store.DBConn{}, "", "app", "users", tooMany)
	assertErrorCode(t, err, ErrCodeTooManyEdits)

	_, err = m.UpdateRows(store.DBConn{}, "", "  ", "users", []RowUpdate{{
		PK:  map[string]*string{"id": strPtr("1")},
		Set: map[string]*string{"name": strPtr("x")},
	}})
	assertErrorCode(t, err, ErrCodeApplyEditsFailed)
}
