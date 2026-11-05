package db

import (
	"strings"
	"testing"
)

func testColumns() []ColumnInfo {
	return []ColumnInfo{
		{Field: "id", Type: "bigint unsigned", Key: "PRI"},
		{Field: "name", Type: "varchar(100)"},
		{Field: "amount", Type: "decimal(12,2)"},
		{Field: "created_at", Type: "datetime"},
		{Field: "status", Type: "enum('new','paid','failed')"},
		{Field: "payload", Type: "json"},
		{Field: "avatar", Type: "blob"},
		{Field: "order", Type: "int"},
		{Field: "group", Type: "int"},
	}
}

func TestColumnFamily(t *testing.T) {
	cases := map[string]columnFamily{
		"bigint unsigned":  famNumeric,
		"int(11)":          famNumeric,
		"decimal(12,2)":    famNumeric,
		"float":            famNumeric,
		"bit(1)":           famNumeric,
		"datetime":         famDate,
		"timestamp":        famDate,
		"year":             famDate,
		"varchar(100)":     famText,
		"longtext":         famText,
		"enum('a','b')":    famEnum,
		"set('a','b')":     famEnum,
		"json":             famJSON,
		"varbinary(16)":    famBinary,
		"blob":             famBinary,
		"geometry":         famOther,
		"unknown_type(10)": famOther,
	}
	for typ, want := range cases {
		if got := columnFamilyOf(typ); got != want {
			t.Errorf("columnFamily(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestOperatorMatrix(t *testing.T) {
	allOps := []FilterOp{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpContains, OpIsNull, OpIsNotNull}
	expected := map[columnFamily]map[FilterOp]bool{
		famNumeric: {OpEq: true, OpNe: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpIsNull: true, OpIsNotNull: true},
		famDate:    {OpEq: true, OpNe: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpIsNull: true, OpIsNotNull: true},
		famText:    {OpEq: true, OpNe: true, OpContains: true, OpIsNull: true, OpIsNotNull: true},
		famEnum:    {OpEq: true, OpNe: true, OpIsNull: true, OpIsNotNull: true},
		famJSON:    {OpContains: true, OpIsNull: true, OpIsNotNull: true},
		famBinary:  {OpIsNull: true, OpIsNotNull: true},
		famOther:   {OpIsNull: true, OpIsNotNull: true},
	}
	for fam, ops := range expected {
		for _, op := range allOps {
			if got := opAllowed(fam, op); got != ops[op] {
				t.Errorf("family %v op %q = %v, want %v", fam, op, got, ops[op])
			}
		}
	}
}

func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"plain":   "plain",
		"50%_off": "50!%!_off",
		"a!b":     "a!!b",
		"%_!":     "!%!_!!",
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateAndBuildStructured(t *testing.T) {
	q := TableQuery{
		Database: "app",
		Table:    "grid_qa",
		Conditions: []FilterCondition{
			{Column: "status", Op: OpEq, Value: "paid"},
			{Column: "created_at", Op: OpGte, Value: "2026-10-01"},
			{Column: "name", Op: OpContains, Value: "foo"},
		},
		SortColumn: "created_at",
		SortDesc:   true,
		Page:       1,
		PageSize:   100,
	}
	built, err := validateAndBuild(testColumns(), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSQL := "SELECT * FROM `app`.`grid_qa` WHERE `status` = ? AND `created_at` >= ? AND `name` LIKE ? ESCAPE '!' ORDER BY `created_at` DESC LIMIT ? OFFSET ?"
	if built.SQL != wantSQL {
		t.Fatalf("SQL mismatch\n got: %s\nwant: %s", built.SQL, wantSQL)
	}
	wantArgs := []any{"paid", "2026-10-01", "%foo%", 100, int64(100)}
	if len(built.Args) != len(wantArgs) {
		t.Fatalf("args = %#v", built.Args)
	}
	for i := range wantArgs {
		if built.Args[i] != wantArgs[i] {
			t.Fatalf("arg[%d] = %#v, want %#v", i, built.Args[i], wantArgs[i])
		}
	}
	if !strings.Contains(built.Preview, "`status` = 'paid'") ||
		!strings.Contains(built.Preview, "`name` LIKE '%foo%' ESCAPE '!'") ||
		!strings.Contains(built.Preview, "LIMIT 100 OFFSET 100") {
		t.Fatalf("preview mismatch: %s", built.Preview)
	}
	wantWhere := "`status` = 'paid' AND `created_at` >= '2026-10-01' AND `name` LIKE '%foo%' ESCAPE '!'"
	if built.Where != wantWhere {
		t.Fatalf("where mismatch\n got: %s\nwant: %s", built.Where, wantWhere)
	}
	if strings.Contains(built.SQL, "paid") {
		t.Fatalf("value leaked into execution SQL: %s", built.SQL)
	}
}

func TestValidateAndBuildNullOps(t *testing.T) {
	q := TableQuery{
		Database: "app", Table: "t", PageSize: 100,
		Conditions: []FilterCondition{
			{Column: "name", Op: OpIsNull},
			{Column: "amount", Op: OpIsNotNull},
		},
	}
	built, err := validateAndBuild(testColumns(), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(built.Args) != 2 { // 只有 LIMIT/OFFSET
		t.Fatalf("args = %#v, want only LIMIT/OFFSET", built.Args)
	}
	if !strings.Contains(built.SQL, "`name` IS NULL AND `amount` IS NOT NULL") {
		t.Fatalf("SQL = %s", built.SQL)
	}
}

func TestValidateAndBuildPreviewEscapesLiterals(t *testing.T) {
	q := TableQuery{
		Database: "app", Table: "t", PageSize: 100,
		Conditions: []FilterCondition{
			{Column: "name", Op: OpEq, Value: "O'Reilly\\temp\n中文 😀"},
		},
	}
	built, err := validateAndBuild(testColumns(), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if built.Args[0] != "O'Reilly\\temp\n中文 😀" {
		t.Fatalf("arg changed: %#v", built.Args[0])
	}
	if !strings.Contains(built.Preview, `'O''Reilly\\temp\n中文 😀'`) {
		t.Fatalf("preview not escaped: %s", built.Preview)
	}
}

func TestValidateAndBuildRejects(t *testing.T) {
	base := TableQuery{Database: "app", Table: "t", PageSize: 100}
	with := func(fn func(*TableQuery)) TableQuery {
		q := base
		fn(&q)
		return q
	}
	cases := []struct {
		name string
		q    TableQuery
		code string
	}{
		{"mode conflict", with(func(q *TableQuery) {
			q.Conditions = []FilterCondition{{Column: "name", Op: OpEq, Value: "x"}}
			q.RawWhere = "1=1"
		}), ErrCodeFilterModeConflict},
		{"too many conditions", with(func(q *TableQuery) {
			for i := 0; i < 6; i++ {
				q.Conditions = append(q.Conditions, FilterCondition{Column: "name", Op: OpEq, Value: "x"})
			}
		}), ErrCodeTooManyConditions},
		{"unknown column", with(func(q *TableQuery) {
			q.Conditions = []FilterCondition{{Column: "nope", Op: OpEq, Value: "x"}}
		}), ErrCodeUnknownColumn},
		{"operator not allowed", with(func(q *TableQuery) {
			q.Conditions = []FilterCondition{{Column: "amount", Op: OpContains, Value: "x"}}
		}), ErrCodeOperatorNotAllowed},
		{"value too long", with(func(q *TableQuery) {
			q.Conditions = []FilterCondition{{Column: "name", Op: OpEq, Value: strings.Repeat("x", maxConditionValue+1)}}
		}), ErrCodeValueTooLong},
		{"raw too long", with(func(q *TableQuery) {
			q.RawWhere = strings.Repeat("x", maxRawWhere+1) + " = 1"
		}), ErrCodeRawWhereTooLong},
		{"sort unknown", with(func(q *TableQuery) {
			q.SortColumn = "nope"
		}), ErrCodeUnknownColumn},
		{"negative page", with(func(q *TableQuery) {
			q.Page = -1
		}), ErrCodeInvalidPage},
		{"invalid page size", with(func(q *TableQuery) {
			q.PageSize = 42
		}), ErrCodeInvalidPage},
		{"offset too large", with(func(q *TableQuery) {
			q.Page = 1_000_001
			q.PageSize = 1000
		}), ErrCodeInvalidPage},
		{"missing table", with(func(q *TableQuery) {
			q.Table = " "
		}), ErrCodeTableMetadataUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateAndBuild(testColumns(), tc.q)
			assertErrorCode(t, err, tc.code)
		})
	}
}

func TestAnalyzeRawWhereAllowed(t *testing.T) {
	cases := map[string]string{
		"status = 'paid' OR amount > 100":        "status = 'paid' OR amount > 100",
		"note = 'a;b--c'":                        "note = 'a;b--c'",
		"`note` = 'x/*y*/'":                      "`note` = 'x/*y*/'",
		"id IN (SELECT id FROM grid_qa LIMIT 5)": "id IN (SELECT id FROM grid_qa LIMIT 5)",
		"order = 1":                              "order = 1",
		"group = 1":                              "group = 1",
		"`order` = 1 AND `group` = 2":            "`order` = 1 AND `group` = 2",
		"NOT (status = 'new')":                   "NOT (status = 'new')",
		"name = 'a''b'":                          "name = 'a''b'",
	}
	for in, want := range cases {
		got, hadWhere, err := analyzeRawWhere(in)
		if err != nil {
			t.Errorf("analyzeRawWhere(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want || hadWhere {
			t.Errorf("analyzeRawWhere(%q) = %q, hadWhere=%v, want %q", in, got, hadWhere, want)
		}
	}
}

func TestAnalyzeRawWhereStripsOneLeadingWhere(t *testing.T) {
	for _, in := range []string{"WHERE status = 1", "where   status = 1", "  WhErE\tstatus = 1"} {
		got, hadWhere, err := analyzeRawWhere(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !hadWhere || got != "status = 1" {
			t.Fatalf("analyzeRawWhere(%q) = %q, hadWhere=%v", in, got, hadWhere)
		}
	}
	// 只去掉一个：第二个 WHERE 保留（执行时会由 MySQL 报语法错误）
	got, hadWhere, err := analyzeRawWhere("WHERE WHERE x = 1")
	if err != nil || !hadWhere || got != "WHERE x = 1" {
		t.Fatalf("double WHERE: got=%q hadWhere=%v err=%v", got, hadWhere, err)
	}
	// 反引号标识符 / 字符串开头的 where 不算
	if got, hadWhere, _ := analyzeRawWhere("`where` = 1"); hadWhere || got != "`where` = 1" {
		t.Fatalf("backticked where incorrectly stripped: %q %v", got, hadWhere)
	}
}

func TestAnalyzeRawWhereRejected(t *testing.T) {
	cases := []string{
		"1=1; DROP TABLE grid_qa",
		"1=1 -- comment",
		"1=1 # comment",
		"1=1 /* comment */",
		"/*!50000 1=1 */",
		"ORDER BY id",
		"GROUP BY status",
		"HAVING count(*) > 1",
		"LIMIT 10",
		"OFFSET 10",
		"UNION SELECT 1",
		"name = 'unterminated",
		"`unterminated",
		"name = 'a\x00b'",
		"name = 'a\x01b'",
	}
	for _, in := range cases {
		if _, _, err := analyzeRawWhere(in); err == nil {
			t.Errorf("analyzeRawWhere(%q) should be rejected", in)
		} else {
			assertErrorCode(t, err, ErrCodeUnsafeRawWhere)
		}
	}
}

func TestValidateAndBuildRawWhere(t *testing.T) {
	q := TableQuery{
		Database: "app", Table: "t", RawWhere: "WHERE status = 'paid' OR amount > 100",
		PageSize: 100,
	}
	built, err := validateAndBuild(testColumns(), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(built.SQL, "WHERE status = 'paid' OR amount > 100 LIMIT ? OFFSET ?") {
		t.Fatalf("SQL = %s", built.SQL)
	}
	if built.Where != "status = 'paid' OR amount > 100" {
		t.Fatalf("Where = %q", built.Where)
	}
	if !built.HadLeadingWhere {
		t.Fatal("HadLeadingWhere should be true for a leading WHERE")
	}
	if len(built.Args) != 2 {
		t.Fatalf("raw args should only be LIMIT/OFFSET: %#v", built.Args)
	}

	// 没有前导 WHERE 时不标记
	built, err = validateAndBuild(testColumns(), TableQuery{
		Database: "app", Table: "t", RawWhere: "status = 'paid'", PageSize: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if built.HadLeadingWhere {
		t.Fatal("HadLeadingWhere should be false without leading WHERE")
	}
}
