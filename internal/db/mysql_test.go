package db

import "testing"

func TestIsQueryClassification(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM users", true},
		{"  select 1", true},
		{"SHOW TABLES", true},
		{"DESCRIBE users", true},
		{"EXPLAIN SELECT 1", true},
		{"WITH x AS (SELECT 1) SELECT * FROM x", true},
		{"WITH x AS (SELECT 1) UPDATE users SET a=1", false},
		{"WITH x AS (SELECT 1) DELETE FROM users", false},
		{"SELECT * FROM users INTO OUTFILE '/tmp/x'", false},
		{"SELECT 1 INTO DUMPFILE '/tmp/x'", false},
		{"SELECT 'INTO OUTFILE' AS s FROM t", true},
		{"-- comment\nSELECT 1", true},
		{"/* comment */ SELECT 1", true},
		{"/* comment */ UPDATE t SET a=1", false},
		{"INSERT INTO t VALUES (1)", false},
		{"CALL proc()", false},
		{"SET @x=1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isQuery(tc.sql); got != tc.want {
			t.Errorf("isQuery(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent("users"); got != "`users`" {
		t.Fatalf("got %q", got)
	}
	if got := quoteIdent("a`b"); got != "`a``b`" {
		t.Fatalf("backtick not escaped: %q", got)
	}
}

func TestPickCreateViewDDL(t *testing.T) {
	// SHOW CREATE VIEW 返回 4 列：View / Create View / character_set_client / collation_connection
	cols := []string{"View", "Create View", "character_set_client", "collation_connection"}
	vals := []any{
		[]byte("v_users"),
		[]byte("CREATE VIEW `v_users` AS SELECT 1"),
		[]byte("utf8mb4"),
		[]byte("utf8mb4_general_ci"),
	}
	got := pickCreateViewDDL(cols, vals)
	if got != "CREATE VIEW `v_users` AS SELECT 1" {
		t.Fatalf("got %q", got)
	}
	// 列顺序变化也能按列名找到
	cols2 := []string{"character_set_client", "collation_connection", "Create View", "View"}
	vals2 := []any{[]byte("utf8mb4"), []byte("utf8mb4_general_ci"), []byte("CREATE VIEW x AS SELECT 1"), []byte("x")}
	if got := pickCreateViewDDL(cols2, vals2); got != "CREATE VIEW x AS SELECT 1" {
		t.Fatalf("got %q", got)
	}
	if got := pickCreateViewDDL([]string{"View"}, []any{[]byte("x")}); got != "" {
		t.Fatalf("missing Create View should return empty, got %q", got)
	}
}

func TestParseColumnRows(t *testing.T) {
	// SHOW FULL COLUMNS 的列顺序
	cols := []string{"Field", "Type", "Collation", "Null", "Key", "Default", "Extra", "Privileges", "Comment"}
	rows := [][]any{
		{[]byte("id"), []byte("bigint unsigned"), nil, []byte("NO"), []byte("PRI"), nil, []byte("auto_increment"), []byte("select"), []byte("")},
		{[]byte("email"), []byte("varchar(200)"), []byte("utf8mb4_general_ci"), []byte("NO"), []byte("UNI"), nil, []byte(""), []byte("select"), []byte("邮箱")},
		{[]byte("empty_def"), []byte("varchar(10)"), []byte("utf8mb4_general_ci"), []byte("NO"), []byte(""), []byte(""), []byte(""), []byte("select"), []byte("")},
		{[]byte("null_def"), []byte("varchar(10)"), []byte("utf8mb4_general_ci"), []byte("YES"), []byte(""), nil, []byte(""), []byte("select"), []byte("")},
	}
	got := parseColumnRows(cols, rows)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[0].Field != "id" || got[0].Key != "PRI" || got[0].Extra != "auto_increment" {
		t.Fatalf("unexpected first column: %+v", got[0])
	}
	// Comment 必须真实映射（DESCRIBE 拿不到）
	if got[1].Comment != "邮箱" {
		t.Fatalf("Comment = %q, want 邮箱", got[1].Comment)
	}
	// Default 必须区分 SQL NULL 与空字符串
	if got[2].Default == nil || *got[2].Default != "" {
		t.Fatalf("empty default should be non-nil empty string, got %#v", got[2].Default)
	}
	if got[3].Default != nil {
		t.Fatalf("NULL default should be nil, got %#v", got[3].Default)
	}
	// 列顺序保持
	if got[0].Field != "id" || got[1].Field != "email" || got[2].Field != "empty_def" || got[3].Field != "null_def" {
		t.Fatalf("column order not preserved: %+v", got)
	}

	// 列顺序变化也能按列名映射
	cols2 := []string{"Comment", "Default", "Key", "Null", "Type", "Field"}
	rows2 := [][]any{{[]byte("姓名"), []byte("x"), []byte(""), []byte("YES"), []byte("varchar(10)"), []byte("name")}}
	got2 := parseColumnRows(cols2, rows2)
	if len(got2) != 1 || got2[0].Field != "name" || got2[0].Comment != "姓名" || got2[0].Default == nil || *got2[0].Default != "x" {
		t.Fatalf("column mapping by name failed: %+v", got2)
	}
}
