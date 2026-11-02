package db

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/lemon-puls/panoramix/internal/store"
)

// setupRedis 启动进程内 Redis（miniredis）并返回连接配置
func setupRedis(t *testing.T) (*miniredis.Miniredis, store.DBConn) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	host, portStr, err := splitHostPort(mr.Addr())
	if err != nil {
		t.Fatalf("parse addr: %v", err)
	}
	return mr, store.DBConn{
		ID:   "test-redis",
		Name: "test",
		Kind: store.DBRedis,
		Addr: host,
		Port: portStr,
	}
}

func splitHostPort(addr string) (string, int, error) {
	var host string
	var port int
	// miniredis Addr 形如 127.0.0.1:PORT
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host = addr[:i]
			_, err := fmtSscan(addr[i+1:], &port)
			return host, port, err
		}
	}
	return "", 0, errBadAddr
}

var errBadAddr = &addrError{}

type addrError struct{}

func (*addrError) Error() string { return "bad address" }

func fmtSscan(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadAddr
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return 1, nil
}

func TestRedisScanAndValue(t *testing.T) {
	mr, cfg := setupRedis(t)
	mgr := NewManager()
	defer mgr.CloseAll()

	// 种数据：不同类型
	mr.Set("user:1", "alice")
	mr.Set("user:2", "bob")
	mr.HSet("profile:1", "name", "alice")
	mr.HSet("profile:1", "city", "hz")
	mr.Lpush("queue", "a")
	mr.Lpush("queue", "b")
	mr.Lpush("queue", "c")
	mr.SAdd("tags", "go", "rust")
	mr.ZAdd("rank", 10, "x")
	mr.ZAdd("rank", 20, "y")
	mr.SetTTL("user:1", 60*time.Second)

	page, err := mgr.RedisScan(cfg, "", 0, "user:*", 100)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(page.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d (%+v)", len(page.Keys), page.Keys)
	}
	for _, k := range page.Keys {
		if k.Type != "string" {
			t.Fatalf("unexpected type for %s: %s", k.Key, k.Type)
		}
	}
	// TTL 已设置的键应有正 TTL
	found := false
	for _, k := range page.Keys {
		if k.Key == "user:1" && k.TTL > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("user:1 should have positive TTL: %+v", page.Keys)
	}

	// string 值
	v, err := mgr.RedisValue(cfg, "", "user:1")
	if err != nil {
		t.Fatalf("value string: %v", err)
	}
	if v.Type != "string" || v.Str == nil || *v.Str != "alice" {
		t.Fatalf("string value mismatch: %+v", v)
	}

	// hash 值
	v, err = mgr.RedisValue(cfg, "", "profile:1")
	if err != nil {
		t.Fatalf("value hash: %v", err)
	}
	if v.Type != "hash" || len(v.Hash) != 2 || v.Hash["city"] != "hz" {
		t.Fatalf("hash value mismatch: %+v", v)
	}

	// list 值（LPUSH 逆序）
	v, err = mgr.RedisValue(cfg, "", "queue")
	if err != nil {
		t.Fatalf("value list: %v", err)
	}
	if v.Type != "list" || len(v.List) != 3 || v.List[0] != "c" {
		t.Fatalf("list value mismatch: %+v", v.List)
	}

	// set 值
	v, err = mgr.RedisValue(cfg, "", "tags")
	if err != nil {
		t.Fatalf("value set: %v", err)
	}
	if v.Type != "set" || len(v.Set) != 2 {
		t.Fatalf("set value mismatch: %+v", v)
	}

	// zset 值
	v, err = mgr.RedisValue(cfg, "", "rank")
	if err != nil {
		t.Fatalf("value zset: %v", err)
	}
	if v.Type != "zset" || len(v.ZSet) != 2 || v.ZSet[0].Score != 10 {
		t.Fatalf("zset value mismatch: %+v", v.ZSet)
	}

	// 不存在的键
	if _, err := mgr.RedisValue(cfg, "", "missing"); err == nil {
		t.Fatal("missing key should error")
	}
}

func TestRedisTTLAndDelete(t *testing.T) {
	mr, cfg := setupRedis(t)
	mgr := NewManager()
	defer mgr.CloseAll()

	mr.Set("k", "v")
	if err := mgr.RedisSetTTL(cfg, "", "k", 120); err != nil {
		t.Fatalf("set ttl: %v", err)
	}
	if got := mr.TTL("k"); got != 120*time.Second {
		t.Fatalf("ttl = %v", got)
	}
	// 0 秒 = 移除过期
	if err := mgr.RedisSetTTL(cfg, "", "k", 0); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if got := mr.TTL("k"); got != 0 {
		t.Fatalf("ttl should be cleared, got %v", got)
	}
	// 删除
	if err := mgr.RedisDelete(cfg, "", "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if mr.Exists("k") {
		t.Fatal("key should be deleted")
	}
}

func TestIsQuery(t *testing.T) {
	cases := map[string]bool{
		"SELECT 1":                     true,
		"  select * from t":            true,
		"SHOW TABLES":                  true,
		"desc users":                   true,
		"EXPLAIN SELECT 1":             true,
		"WITH x AS (SELECT 1) SELECT * FROM x": true,
		"INSERT INTO t VALUES (1)":     false,
		"UPDATE t SET a=1":             false,
		"DELETE FROM t":                false,
		"CREATE TABLE t (id INT)":      false,
		"-- comment\nSELECT 1":         true,
		"/* c */ SELECT 1":             true,
	}
	for sqlText, want := range cases {
		if got := isQuery(sqlText); got != want {
			t.Errorf("isQuery(%q) = %v, want %v", sqlText, got, want)
		}
	}
}

// 确保 redis 包被正确引用（编译期守卫）
var _ = redis.KeepTTL
