package xshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureXSH 生成一份 .xsh 文件（自动选择编码）
func fixtureXSH(t *testing.T, dir, name string, utf16 bool, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if utf16 {
		runes := []rune(body)
		u := make([]uint16, len(runes))
		for i, r := range runes {
			u[i] = uint16(r)
		}
		b := make([]byte, 0, len(u)*2+2)
		b = append(b, 0xFF, 0xFE) // UTF-16LE BOM
		for _, r := range u {
			b = append(b, byte(r), byte(r>>8))
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return path
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

const xshPassword = `[CONNECTION]
Protocol=SSH
Host=10.0.0.11
Port=2222

[CONNECTION:AUTHENTICATION]
Method=PASSWORD
UserName=root
Password=x0ff1245deadbeef
`

const xshKeyUTF16 = `[CONNECTION]
Protocol=SSH
Host=example.com
Port=22

[CONNECTION:AUTHENTICATION]
Method=PUBLICKEY
UserName=deploy
UserKey=MyEd25519
`

const xshTelnet = `[CONNECTION]
Protocol=TELNET
Host=192.168.1.1
Port=23
`

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	fixtureXSH(t, dir, "生产-web.xsh", false, xshPassword)
	fixtureXSH(t, dir, "deploy-example.xsh", true, xshKeyUTF16)
	fixtureXSH(t, dir, "老路由器.xsh", false, xshTelnet)
	// 非 .xsh 文件必须被忽略
	fixtureXSH(t, dir, "ignore.txt", false, xshPassword)

	list, err := ScanDir(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 previews, got %d", len(list))
	}

	byName := map[string]Preview{}
	for _, p := range list {
		byName[p.Name] = p
	}

	web := byName["生产-web"]
	if !web.Valid || web.Host != "10.0.0.11" || web.Port != 2222 ||
		web.Username != "root" || web.AuthType != "password" {
		t.Fatalf("password session parsed wrong: %+v", web)
	}

	dep := byName["deploy-example"]
	if !dep.Valid || dep.Host != "example.com" || dep.AuthType != "key" || dep.KeyName != "MyEd25519" {
		t.Fatalf("key session parsed wrong: %+v", dep)
	}

	tel := byName["老路由器"]
	if tel.Valid || tel.SkipReason == "" {
		t.Fatalf("telnet session should be invalid with reason: %+v", tel)
	}
}

func TestParseINI(t *testing.T) {
	doc := ParseINI("; comment\n# comment2\n[A]\nK1 = V1\nK2=V=2\n[a]\nk1 = overridden\n")
	a := section(doc, "A")
	if a["K1"] != "V1" {
		t.Fatalf("K1: %q", a["K1"])
	}
	if a["K2"] != "V=2" {
		t.Fatalf("value with '=': %q", a["K2"])
	}
	// 大小写不敏感合并（[a] 与 [A] 同节，覆盖 K1）
	if a["k1"] != "overridden" {
		t.Fatalf("case-insensitive section: %q", a["k1"])
	}
}

func TestDecodeUTF16NoBOM(t *testing.T) {
	body := "[CONNECTION]\r\nHost=1.2.3.4\r\n"
	runes := []rune(body)
	b := make([]byte, 0, len(runes)*2)
	for _, r := range runes {
		b = append(b, byte(r), byte(r>>8)) // 无 BOM 的 UTF-16LE
	}
	got := decodeText(b)
	if !strings.Contains(got, "Host=1.2.3.4") {
		t.Fatalf("utf16 no-bom decode failed: %q", got)
	}
}
