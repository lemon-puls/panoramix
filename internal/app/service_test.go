package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"panorama/internal/store"
)

// ---------- 测试替身 ----------

type nopEmitter struct{}

func (nopEmitter) SetContext(context.Context)  {}
func (nopEmitter) Emit(string, ...interface{}) {}

// memSecrets 内存凭据库：模拟 keychain，同时可断言“凭据只出现在这里”
type memSecrets struct{ m map[string]string }

func newMemSecrets() *memSecrets { return &memSecrets{m: map[string]string{}} }

func (m *memSecrets) SaveSecret(ref, secret string) error {
	m.m[ref] = secret
	return nil
}

func (m *memSecrets) GetSecret(ref string) (string, error) {
	if v, ok := m.m[ref]; ok {
		return v, nil
	}
	return "", store.ErrSecretNotFound
}

func (m *memSecrets) DeleteSecret(ref string) error {
	delete(m.m, ref)
	return nil
}

func newTestService(t *testing.T) (*Service, *memSecrets) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sec := newMemSecrets()
	return NewService(st, sec, nopEmitter{}), sec
}

// ---------- 脱敏（硬性验收项） ----------

func TestHostInputRedaction(t *testing.T) {
	in := HostInput{Password: "super-secret-pw", Passphrase: "super-secret-pp"}
	in.Name = "web-1"
	in.Addr = "10.0.0.1"
	in.Username = "root"

	for _, got := range []string{fmt.Sprintf("%v", in), fmt.Sprintf("%#v", in)} {
		if strings.Contains(got, "super-secret") {
			t.Fatalf("HostInput leaked credentials: %s", got)
		}
	}
}

// ---------- 主机与凭据 ----------

func TestSaveHostStoresSecretOnlyInKeychain(t *testing.T) {
	svc, sec := newTestService(t)

	in := HostInput{}
	in.Name = "web-1"
	in.Addr = "10.0.0.2"
	in.Port = 22
	in.AuthType = store.AuthPassword
	in.Password = "pw123"

	h, err := svc.SaveHost(in)
	if err != nil {
		t.Fatalf("save host: %v", err)
	}
	if h.ID == "" || !h.HasSecret {
		t.Fatalf("expected id/HasSecret filled, got %+v", h)
	}
	// 凭据必须能从 keychain 读回
	if got, err := sec.GetSecret(store.HostPasswordRef(h.ID)); err != nil || got != "pw123" {
		t.Fatalf("secret not stored in keychain: got=%q err=%v", got, err)
	}
	// SQLite 中不存在密码字段——通过 Host 结构本身无密码字段保证，此处复核 keychain 唯一性
	if len(sec.m) != 1 {
		t.Fatalf("unexpected extra secrets: %v", sec.m)
	}
}

func TestSaveHostKeepsExistingSecretWhenBlank(t *testing.T) {
	svc, sec := newTestService(t)

	in := HostInput{}
	in.Name = "web-2"
	in.Addr = "10.0.0.3"
	in.Password = "old-pw"
	h, err := svc.SaveHost(in)
	if err != nil {
		t.Fatalf("save host: %v", err)
	}

	// 留空密码再次保存：不应覆盖既有凭据
	in2 := HostInput{}
	in2.ID = h.ID
	in2.Name = "web-2"
	in2.Addr = "10.0.0.3"
	in2.Password = ""
	if _, err := svc.SaveHost(in2); err != nil {
		t.Fatalf("resave host: %v", err)
	}
	if got, _ := sec.GetSecret(store.HostPasswordRef(h.ID)); got != "old-pw" {
		t.Fatalf("blank password should keep existing secret, got %q", got)
	}

	// 显式删除凭据
	if err := svc.DeleteHostSecret(h.ID, "password"); err != nil {
		t.Fatalf("delete host secret: %v", err)
	}
	if _, err := sec.GetSecret(store.HostPasswordRef(h.ID)); err == nil {
		t.Fatal("secret should be deleted")
	}
}

func TestSaveHostValidation(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.SaveHost(HostInput{}); err == nil {
		t.Fatal("empty host should be rejected")
	}
	in := HostInput{}
	in.Name = "  " // 纯空白同样拒绝
	in.Addr = "1.2.3.4"
	if _, err := svc.SaveHost(in); err == nil {
		t.Fatal("blank name should be rejected")
	}
}

func TestDeleteGroupBlockedWhenHostsExist(t *testing.T) {
	svc, _ := newTestService(t)

	g, err := svc.SaveGroup(store.Group{Name: "prod"})
	if err != nil {
		t.Fatalf("save group: %v", err)
	}
	in := HostInput{}
	in.Name = "h1"
	in.Addr = "1.2.3.4"
	in.GroupID = g.ID
	h, err := svc.SaveHost(in)
	if err != nil {
		t.Fatalf("save host: %v", err)
	}

	if err := svc.DeleteGroup(g.ID); err == nil {
		t.Fatal("deleting non-empty group should fail")
	}
	if err := svc.DeleteHost(h.ID); err != nil {
		t.Fatalf("delete host: %v", err)
	}
	if err := svc.DeleteGroup(g.ID); err != nil {
		t.Fatalf("delete empty group: %v", err)
	}
}

func TestSnippetValidation(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.SaveSnippet(store.Snippet{Name: "", Content: "ls"}); err == nil {
		t.Fatal("snippet without name should be rejected")
	}
	if _, err := svc.SaveSnippet(store.Snippet{Name: "list", Content: " "}); err == nil {
		t.Fatal("snippet with blank content should be rejected")
	}
}
