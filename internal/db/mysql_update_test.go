package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
