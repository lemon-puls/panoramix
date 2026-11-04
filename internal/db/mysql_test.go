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
