package finalshell

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	// 典型 SSH 密码连接
	writeConfig(t, dir, "abc123_connect_config.json", `{
		"name": "生产-web-1",
		"host": "10.0.0.21",
		"port": 2222,
		"user_name": "root",
		"password": "U2FsdGVkX19encrypted==",
		"conection_type": 100,
		"proxy_id": "",
		"secret_key_id": ""
	}`)
	// 端口以字符串给出（部分版本）
	writeConfig(t, dir, "def456_connect_config.json", `{
		"name": "测试机",
		"host": "192.168.1.8",
		"port": "22",
		"user_name": "deploy",
		"conection_type": 100,
		"proxy_id": "proxy-uuid",
		"secret_key_id": ""
	}`)
	// 使用密钥认证
	writeConfig(t, dir, "ghi789_connect_config.json", `{
		"name": "密钥机",
		"host": "example.com",
		"port": 22,
		"user_name": "ops",
		"conection_type": 100,
		"secret_key_id": "key-uuid"
	}`)
	// 非 SSH 类型（如 RDP/代理）
	writeConfig(t, dir, "jkl012_connect_config.json", `{
		"name": "RDP 机",
		"host": "10.0.0.9",
		"port": 3389,
		"user_name": "admin",
		"conection_type": 200
	}`)
	// 损坏文件
	writeConfig(t, dir, "broken_connect_config.json", `{not valid json`)
	// 非配置文件名，应被忽略
	writeConfig(t, dir, "notes.txt", `hello`)

	list, err := ScanDir(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("expected 5 previews, got %d", len(list))
	}

	byName := map[string]Preview{}
	for _, p := range list {
		byName[p.Name] = p
	}

	web := byName["生产-web-1"]
	if !web.Valid || web.Host != "10.0.0.21" || web.Port != 2222 || web.Username != "root" || web.AuthType != "password" {
		t.Fatalf("password conn parsed wrong: %+v", web)
	}
	if web.HasProxy {
		t.Fatalf("no proxy expected: %+v", web)
	}

	test := byName["测试机"]
	if !test.Valid || test.Port != 22 || test.Username != "deploy" || !test.HasProxy {
		t.Fatalf("string-port conn parsed wrong: %+v", test)
	}

	key := byName["密钥机"]
	if !key.Valid || key.AuthType != "key" || key.KeyName == "" {
		t.Fatalf("key conn parsed wrong: %+v", key)
	}

	if rdp := byName["RDP 机"]; rdp.Valid || rdp.SkipReason == "" {
		t.Fatalf("non-ssh should be invalid: %+v", rdp)
	}

	// 损坏文件：Name 为空（解析失败时用文件名兜底前）
	var brokenSeen bool
	for _, p := range list {
		if p.File == "broken_connect_config.json" {
			brokenSeen = true
			if p.Valid || p.SkipReason == "" {
				t.Fatalf("broken json should be invalid: %+v", p)
			}
		}
	}
	if !brokenSeen {
		t.Fatal("broken file not scanned")
	}
}
