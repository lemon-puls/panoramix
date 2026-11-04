package ai

import (
	"strings"
	"testing"
)

func TestAnalyzeTableDriven(t *testing.T) {
	cases := []struct {
		name          string
		sql           string
		allowWrite    bool
		wantWrite     bool
		wantMulti     bool
		wantDanger    bool
		wantUncertain bool
		wantInsert    bool
		wantReason    string
	}{
		{name: "select", sql: "SELECT * FROM users", allowWrite: false, wantInsert: true},
		{name: "select with semicolon in string", sql: "SELECT ';' AS x FROM users", wantInsert: true},
		{name: "select with semicolon in backtick", sql: "SELECT `a;b` FROM t", wantInsert: true},
		{name: "show", sql: "SHOW TABLES", wantInsert: true},
		{name: "explain", sql: "EXPLAIN SELECT * FROM users", wantInsert: true},
		{name: "leading comment select", sql: "/* hi */ -- x\nSELECT 1", wantInsert: true},
		{name: "multi statement", sql: "SELECT 1; SELECT 2", wantMulti: true, wantReason: WarnMultiStatement},
		{name: "multi with trailing semicolon only", sql: "SELECT 1;", wantInsert: true},
		{name: "with select", sql: "WITH x AS (SELECT 1) SELECT * FROM x", wantInsert: true},
		{name: "with update", sql: "WITH x AS (SELECT 1) UPDATE users SET a=1", wantWrite: true, wantReason: WarnWriteOperation},
		{name: "with update allowed", sql: "WITH x AS (SELECT 1) UPDATE users SET a=1", allowWrite: true, wantWrite: true, wantInsert: true},
		{name: "insert blocked", sql: "INSERT INTO users (a) VALUES (1)", wantWrite: true, wantReason: WarnWriteOperation},
		{name: "insert allowed", sql: "INSERT INTO users (a) VALUES (1)", allowWrite: true, wantWrite: true, wantInsert: true},
		{name: "update", sql: "UPDATE users SET a=1", wantWrite: true, wantReason: WarnWriteOperation},
		{name: "ddl", sql: "CREATE TABLE t (id int)", wantWrite: true, wantReason: WarnWriteOperation},
		{name: "call", sql: "CALL proc()", wantWrite: true, wantReason: WarnWriteOperation},
		{name: "select into outfile", sql: "SELECT * FROM users INTO OUTFILE '/tmp/x'", wantDanger: true, wantReason: WarnDangerousStatement},
		{name: "select into dumpfile", sql: "SELECT 1 INTO DUMPFILE '/tmp/x'", wantDanger: true, wantReason: WarnDangerousStatement},
		{name: "select for update", sql: "SELECT * FROM users FOR UPDATE", wantDanger: true, wantReason: WarnDangerousStatement},
		{name: "lock in share mode", sql: "SELECT * FROM users LOCK IN SHARE MODE", wantDanger: true, wantReason: WarnDangerousStatement},
		{name: "dangerous even when write allowed", sql: "SELECT * FROM users INTO OUTFILE '/tmp/x'", allowWrite: true, wantDanger: true, wantReason: WarnDangerousStatement},
		{name: "unknown keyword", sql: "FOO bar baz", wantUncertain: true, wantReason: WarnUncertainStatement},
		{name: "unknown keyword allowed", sql: "FOO bar baz", allowWrite: true, wantUncertain: true, wantInsert: true},
		{name: "empty", sql: "   ", wantUncertain: true, wantReason: WarnUncertainStatement},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Analyze(tc.sql)
			if a.IsWrite != tc.wantWrite {
				t.Fatalf("IsWrite = %v, want %v (%+v)", a.IsWrite, tc.wantWrite, a)
			}
			if a.MultiStatement != tc.wantMulti {
				t.Fatalf("MultiStatement = %v, want %v", a.MultiStatement, tc.wantMulti)
			}
			if a.Dangerous != tc.wantDanger {
				t.Fatalf("Dangerous = %v, want %v (%+v)", a.Dangerous, tc.wantDanger, a)
			}
			if a.Uncertain != tc.wantUncertain {
				t.Fatalf("Uncertain = %v, want %v", a.Uncertain, tc.wantUncertain)
			}
			allowed, reason := a.InsertPolicy(tc.allowWrite)
			if allowed != tc.wantInsert {
				t.Fatalf("InsertPolicy = %v (%s), want %v", allowed, reason, tc.wantInsert)
			}
			if tc.wantReason != "" && reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestAnalyzeExtractsTables(t *testing.T) {
	a := Analyze("SELECT u.id FROM users u JOIN orders o ON o.user_id = u.id WHERE o.x IN (SELECT id FROM payments)")
	joined := strings.Join(a.Tables, ",")
	for _, want := range []string{"users", "orders", "payments"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing table %q in %v", want, a.Tables)
		}
	}
}

func TestAnalyzeIgnoresCommentsAndStringsForKeywords(t *testing.T) {
	a := Analyze("SELECT 'UPDATE users SET x=1' AS note /* DELETE FROM users */ FROM logs")
	if a.IsWrite || a.Dangerous || a.MultiStatement {
		t.Fatalf("keywords inside strings/comments must be ignored: %+v", a)
	}
}

func TestParseResponse(t *testing.T) {
	cases := []struct {
		name     string
		resp     string
		wantSQL  string
		wantExpl string
	}{
		{
			name:     "sql fence",
			resp:     "Here you go:\n```sql\nSELECT * FROM users;\n```\nThis lists users.",
			wantSQL:  "SELECT * FROM users;",
			wantExpl: "Here you go:\nThis lists users.",
		},
		{
			name:    "mysql fence",
			resp:    "```mysql\nSELECT 1\n```",
			wantSQL: "SELECT 1",
		},
		{
			name:    "no language fence",
			resp:    "```\nUPDATE t SET a = 1;\n```",
			wantSQL: "UPDATE t SET a = 1;",
		},
		{
			name:    "prefers sql over json",
			resp:    "```json\n{\"a\":1}\n```\n```sql\nSELECT 2;\n```",
			wantSQL: "SELECT 2;",
		},
		{
			name:     "no fence first statement",
			resp:     "The answer is SELECT id FROM users WHERE id = 1; done.",
			wantSQL:  "SELECT id FROM users WHERE id = 1;",
			wantExpl: "The answer is SELECT id FROM users WHERE id = 1; done.",
		},
		{
			name:    "no sql at all",
			resp:    "I need more information about the tables.",
			wantSQL: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlText, expl := ParseResponse(tc.resp)
			if sqlText != tc.wantSQL {
				t.Fatalf("sql = %q, want %q", sqlText, tc.wantSQL)
			}
			if tc.wantExpl != "" && expl != tc.wantExpl {
				t.Fatalf("explanation = %q, want %q", expl, tc.wantExpl)
			}
		})
	}
}
