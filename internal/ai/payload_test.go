package ai

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSanitizeDDLStripsSensitiveMetadata(t *testing.T) {
	raw := "CREATE ALGORITHM=UNDEFINED DEFINER=`admin`@`10.0.0.9` SQL SECURITY DEFINER VIEW `v` AS select 1\n" +
		"CREATE TABLE `users` (\n" +
		"  `id` bigint NOT NULL AUTO_INCREMENT,\n" +
		"  `name` varchar(64) NOT NULL COMMENT 'user  name',\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB AUTO_INCREMENT=421337 DEFAULT CHARSET=utf8mb4 /*!40101 SET NAMES utf8 */"
	got := SanitizeDDL(raw)
	for _, banned := range []string{"DEFINER", "admin", "10.0.0.9", "AUTO_INCREMENT=421337", "/*!40101"} {
		if strings.Contains(got, banned) {
			t.Fatalf("sanitized DDL still contains %q:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "COMMENT 'user  name'") {
		t.Fatalf("column comment must be preserved, got:\n%s", got)
	}
	if strings.ContainsRune(got, '\x00') || strings.ContainsRune(got, '\x07') {
		t.Fatal("control characters must be stripped")
	}
}

func TestSanitizeDDLRemovesCurrentUserDefiner(t *testing.T) {
	got := SanitizeDDL("CREATE VIEW `v` DEFINER=CURRENT_USER AS SELECT 1")
	if strings.Contains(strings.ToUpper(got), "DEFINER") {
		t.Fatalf("DEFINER=CURRENT_USER not removed: %s", got)
	}
}

func TestReferencedTables(t *testing.T) {
	ddl := "CREATE TABLE `orders` (\n" +
		"  CONSTRAINT `fk_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`),\n" +
		"  CONSTRAINT `fk_pay` FOREIGN KEY (`pay_id`) REFERENCES `billing`.`payments` (`id`)\n" +
		")"
	got := referencedTables(ddl)
	want := map[string]bool{"users": true, "payments": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("unexpected referenced table %q in %v", name, got)
		}
	}
}

func TestBuildPayloadGenerate(t *testing.T) {
	built, err := BuildPayload(BuildInput{
		Mode:         ModeGenerate,
		Database:     "demo",
		MySQLVersion: "8.0.36",
		Question:     "最近 7 天未登录且余额大于 0 的用户",
		Locale:       "zh-CN",
		Tables: []TableSchema{{
			Name: "users",
			DDL:  "CREATE TABLE `users` (`id` bigint, `balance` decimal(10,2)) ENGINE=InnoDB AUTO_INCREMENT=99",
		}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	payload := built.SystemPrompt + built.UserPrompt
	for _, want := range []string{"最近 7 天未登录", "### TABLE `users`", "MySQL version: 8.0.36", "Database: demo"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q:\n%s", want, payload)
		}
	}
	if strings.Contains(payload, "AUTO_INCREMENT=99") {
		t.Fatal("DDL was not sanitized before building payload")
	}
	// 类型层面：BuildInput 没有行数据字段；这里用哨兵值证明 payload 只来自元数据。
	if strings.Contains(payload, "row-value-sentinel") {
		t.Fatal("payload must never contain row data")
	}
	if built.SchemaChars == 0 || built.PromptChars == 0 || len(built.Tables) != 1 {
		t.Fatalf("unexpected summary: %+v", built)
	}
}

func TestBuildPayloadContextTooLargeListsTables(t *testing.T) {
	big := strings.Repeat("x", 60)
	_, err := BuildPayload(BuildInput{
		Mode:     ModeGenerate,
		Question: "q",
		Budget:   50,
		Tables: []TableSchema{
			{Name: "a", DDL: big},
			{Name: "b", DDL: big},
		},
	})
	var tooLarge *ErrContextTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected ErrContextTooLarge, got %v", err)
	}
	if tooLarge.Total <= tooLarge.Budget || len(tooLarge.Tables) != 2 {
		t.Fatalf("bad error payload: %+v", tooLarge)
	}
}

func TestBuildPayloadValidation(t *testing.T) {
	if _, err := BuildPayload(BuildInput{Mode: ModeGenerate}); err == nil {
		t.Fatal("generate without question should fail")
	}
	if _, err := BuildPayload(BuildInput{Mode: ModeExplain}); err == nil {
		t.Fatal("explain without sql should fail")
	}
	if _, err := BuildPayload(BuildInput{Mode: ModeFix, SQL: "select 1"}); err != nil {
		t.Fatalf("fix without error should be allowed: %v", err)
	}
	if _, err := BuildPayload(BuildInput{
		Mode:     ModeGenerate,
		Question: strings.Repeat("x", MaxQuestionRunes+1),
	}); err == nil {
		t.Fatal("oversized question should fail")
	}
}

func TestBuildPayloadExplainAndFix(t *testing.T) {
	built, err := BuildPayload(BuildInput{
		Mode:   ModeFix,
		SQL:    "SELECT * FROM users WHERE user_id = 1",
		Error:  "Unknown column 'user_id'",
		Tables: []TableSchema{{Name: "users", DDL: "CREATE TABLE `users` (`id` bigint)"}},
		Locale: "en-US",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(built.UserPrompt, "Last error:") || !strings.Contains(built.UserPrompt, "Unknown column") {
		t.Fatalf("fix payload missing error:\n%s", built.UserPrompt)
	}
	if !strings.Contains(built.SystemPrompt, "English") {
		t.Fatalf("en-US locale should request English output:\n%s", built.SystemPrompt)
	}
	if _, err := BuildPayload(BuildInput{
		Mode: ModeExplain, SQL: "SELECT 1", Locale: "zh-CN",
	}); err != nil {
		t.Fatalf("explain build: %v", err)
	}
}

func TestBuildPayloadFiltersControlCharsAndWarnsForeignKeys(t *testing.T) {
	built, err := BuildPayload(BuildInput{
		Mode:     ModeGenerate,
		Question: "find\x00users\x07",
		Tables: []TableSchema{{
			Name: "orders",
			DDL:  "CREATE TABLE `orders` (`user_id` bigint, CONSTRAINT `fk` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`))",
		}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.ContainsAny(built.UserPrompt, "\x00\x07") {
		t.Fatal("control chars must be stripped from the question")
	}
	found := false
	for _, w := range built.Warnings {
		if w.Code == WarnForeignKeyUnselected && w.Detail == "users" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected foreign_key_unselected warning, got %+v", built.Warnings)
	}
}

func TestPayloadHashStable(t *testing.T) {
	a := PayloadHash("abc")
	b := PayloadHash("abc")
	c := PayloadHash("abd")
	if a != b || a == c || len(a) != 16 {
		t.Fatalf("unstable hash: %s %s %s", a, b, c)
	}
}

func TestBuildPayloadFiltersIdentifiers(t *testing.T) {
	built, err := BuildPayload(BuildInput{
		Mode:         ModeGenerate,
		Question:     "q",
		Database:     "demo\x00x",
		MySQLVersion: "8.0.36\ninjected",
		Tables: []TableSchema{{
			Name: "users\x07",
			DDL:  "CREATE TABLE `users` (`id` bigint)",
		}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	combined := built.SystemPrompt + built.UserPrompt
	if strings.ContainsAny(combined, "\x00\x07") {
		t.Fatal("control characters in identifiers/database/version must be stripped")
	}
	if !strings.Contains(combined, "### TABLE `users`") {
		t.Fatalf("sanitized table name missing:\n%s", combined)
	}
	if !strings.Contains(combined, "Database: demox") {
		t.Fatalf("sanitized database missing:\n%s", combined)
	}
	if strings.Contains(combined, "\ninjected") {
		t.Fatal("version newline must not create a new injected prompt line")
	}
}

func TestBuildPayloadSensitiveHints(t *testing.T) {
	built, err := BuildPayload(BuildInput{
		Mode:     ModeFix,
		SQL:      "-----BEGIN RSA PRIVATE KEY-----\nabc",
		Error:    "Authorization: Bearer sk-abc123",
		Question: "password=hunter2 contact a@b.com key AKIAIOSFODNN7EXAMPLE",
		Tables: []TableSchema{{
			Name: "users",
			DDL:  "CREATE TABLE `users` (`email` varchar(128) COMMENT 'contact ops@example.com')",
		}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	wantPairs := map[string]bool{
		"question/" + SensitivePasswordAssignment: true,
		"question/" + SensitiveEmail:              true,
		"question/" + SensitiveAWSAccessKey:       true,
		"sql/" + SensitivePrivateKey:              true,
		"error/" + SensitiveBearerToken:           true,
		"schema/" + SensitiveEmail:                true,
	}
	found := map[string]bool{}
	for _, h := range built.SensitiveHints {
		key := h.Field + "/" + h.Kind
		if wantPairs[key] {
			found[key] = true
		}
	}
	for key := range wantPairs {
		if !found[key] {
			t.Fatalf("missing hint %s: %+v", key, built.SensitiveHints)
		}
	}
	// 提示只含字段/类型，绝不回显命中内容
	dump := fmt.Sprintf("%v", built.SensitiveHints)
	for _, secret := range []string{
		"hunter2", "a@b.com", "ops@example.com", "sk-abc123",
		"AKIAIOSFODNN7EXAMPLE", "BEGIN RSA",
	} {
		if strings.Contains(dump, secret) {
			t.Fatalf("sensitive hint leaked %q: %s", secret, dump)
		}
	}
	// 用户输入不被自动删改（payload 仍包含原文）
	if !strings.Contains(built.UserPrompt, "hunter2") {
		t.Fatal("user input must not be modified automatically")
	}
}
