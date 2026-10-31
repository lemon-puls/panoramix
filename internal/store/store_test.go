package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// openTestStore 为每个用例创建独立的临时库
func openTestStore(t *testing.T) Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestGroupCRUD(t *testing.T) {
	s := openTestStore(t)

	g := Group{Name: "prod", ParentID: "", Sort: 1}
	if err := s.SaveGroup(&g); err != nil {
		t.Fatalf("save group: %v", err)
	}
	if g.ID == "" || g.CreatedAt.IsZero() {
		t.Fatalf("expected id/timestamps backfilled, got %+v", g)
	}

	// 更新
	g.Name = "production"
	if err := s.SaveGroup(&g); err != nil {
		t.Fatalf("update group: %v", err)
	}
	list, err := s.ListGroups()
	if err != nil || len(list) != 1 {
		t.Fatalf("list groups: %v, len=%d", err, len(list))
	}
	if list[0].Name != "production" {
		t.Fatalf("group name not updated: %+v", list[0])
	}

	// 删除与删除不存在
	if err := s.DeleteGroup(g.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if err := s.DeleteGroup("not-exists"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("delete not-exists should return ErrNoRows, got %v", err)
	}
}

func TestHostCRUD(t *testing.T) {
	s := openTestStore(t)

	h := Host{
		Name:          "web-1",
		Addr:          "10.0.0.11",
		Port:          22,
		Username:      "root",
		AuthType:      AuthKey,
		KeyPath:       `C:\Users\me\.ssh\id_ed25519`,
		InitCommands:  []string{"cd /data", "tail -f app.log"},
		KeepaliveSec:  15,
		AutoReconnect: true,
		Sort:          2,
	}
	if err := s.SaveHost(&h); err != nil {
		t.Fatalf("save host: %v", err)
	}

	list, err := s.ListHosts()
	if err != nil || len(list) != 1 {
		t.Fatalf("list hosts: %v, len=%d", err, len(list))
	}
	got := list[0]
	if got.InitCommands == nil || len(got.InitCommands) != 2 || got.InitCommands[1] != "tail -f app.log" {
		t.Fatalf("init commands round-trip failed: %#v", got.InitCommands)
	}
	if got.KeepaliveSec != 15 || !got.AutoReconnect || got.AuthType != AuthKey {
		t.Fatalf("host fields round-trip failed: %+v", got)
	}

	// TouchHost 刷新最近连接时间
	at := time.Unix(1700000000, 0)
	if err := s.TouchHost(h.ID, at); err != nil {
		t.Fatalf("touch host: %v", err)
	}
	list, _ = s.ListHosts()
	if !list[0].LastConnectedAt.Equal(at) {
		t.Fatalf("last_connected_at not updated: %v", list[0].LastConnectedAt)
	}

	// 默认值兜底：非法端口/keepalive 归一
	bad := Host{Name: "bad", Addr: "x", Port: -1, KeepaliveSec: 0}
	if err := s.SaveHost(&bad); err != nil {
		t.Fatalf("save host: %v", err)
	}
	if bad.Port != 22 || bad.KeepaliveSec != 30 {
		t.Fatalf("defaults not applied: %+v", bad)
	}
}

func TestSnippetAndSetting(t *testing.T) {
	s := openTestStore(t)

	sn := Snippet{Name: "restart", Content: "systemctl restart app", Sort: 0}
	if err := s.SaveSnippet(&sn); err != nil {
		t.Fatalf("save snippet: %v", err)
	}
	sn.Content = "systemctl restart app && echo done"
	if err := s.SaveSnippet(&sn); err != nil {
		t.Fatalf("update snippet: %v", err)
	}
	list, err := s.ListSnippets()
	if err != nil || len(list) != 1 || list[0].Content != "systemctl restart app && echo done" {
		t.Fatalf("snippet round-trip failed: %v %+v", err, list)
	}
	if err := s.DeleteSnippet(sn.ID); err != nil {
		t.Fatalf("delete snippet: %v", err)
	}

	// settings 覆盖写
	if _, ok, _ := s.GetSetting("theme"); ok {
		t.Fatal("theme should not exist yet")
	}
	if err := s.SetSetting("theme", "dark"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatalf("overwrite setting: %v", err)
	}
	v, ok, err := s.GetSetting("theme")
	if err != nil || !ok || v != "light" {
		t.Fatalf("setting round-trip failed: v=%q ok=%v err=%v", v, ok, err)
	}
}
