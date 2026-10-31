package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// newTestKey 每次现场生成一把 ed25519 密钥对（无需任何静态 PEM 数据）
func newTestKey(t *testing.T) (gossh.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pk, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("to ssh pubkey: %v", err)
	}
	return pk, priv
}

// writeTestPrivateKey 把私钥写成 OpenSSH PEM 文件（可选 passphrase）
func writeTestPrivateKey(t *testing.T, dir, name string, priv ed25519.PrivateKey, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase != "" {
		block, err = gossh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	} else {
		block, err = gossh.MarshalPrivateKey(priv, "test")
	}
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestHostKeyTOFU(t *testing.T) {
	checker := NewHostKeyChecker(filepath.Join(t.TempDir(), "known_hosts"))
	key, _ := newTestKey(t)

	// 1. 首连：unknown
	err := checker.Verify("example.com:22", key)
	var hke *HostKeyError
	if !errors.As(err, &hke) || hke.Kind != "unknown" {
		t.Fatalf("expected unknown, got %v", err)
	}
	if !strings.HasPrefix(hke.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint format wrong: %s", hke.Fingerprint)
	}

	// 2. 用户确认后 Trust：再校验通过
	if err := checker.Trust("example.com:22", key); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if err := checker.Verify("example.com:22", key); err != nil {
		t.Fatalf("verify after trust: %v", err)
	}

	// 3. 同一 key 再次校验仍然通过（幂等）
	if err := checker.Verify("example.com:22", key); err != nil {
		t.Fatalf("same key should still verify: %v", err)
	}
}

func TestHostKeyChangedAndReplace(t *testing.T) {
	checker := NewHostKeyChecker(filepath.Join(t.TempDir(), "known_hosts"))
	keyA, _ := newTestKey(t)
	keyB, _ := newTestKey(t)

	if err := checker.Trust("srv:2222", keyA); err != nil {
		t.Fatalf("trust: %v", err)
	}
	// 写入另一台主机，验证 Replace 只动目标主机
	if err := checker.Trust("other:22", keyB); err != nil {
		t.Fatalf("trust other: %v", err)
	}

	// srv:2222 换了 key → changed
	err := checker.Verify("srv:2222", keyB)
	var hke *HostKeyError
	if !errors.As(err, &hke) || hke.Kind != "changed" {
		t.Fatalf("expected changed, got %v", err)
	}

	// 用户明确确认 → Replace 后通过，且 other 条目仍在
	if err := checker.Replace("srv:2222", keyB); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := checker.Verify("srv:2222", keyB); err != nil {
		t.Fatalf("verify after replace: %v", err)
	}
	if err := checker.Verify("other:22", keyB); err != nil {
		t.Fatalf("other host entry must survive replace: %v", err)
	}
	// 旧 keyA 现在应该是 changed（防回滚）
	if err := checker.Verify("srv:2222", keyA); err == nil {
		t.Fatal("old key should no longer verify")
	}
}

func TestHostKeyNormalizeNonStandardPort(t *testing.T) {
	checker := NewHostKeyChecker(filepath.Join(t.TempDir(), "known_hosts"))
	key, _ := newTestKey(t)

	// 非标端口的 known_hosts 行应为 [host]:2222 形态；Trust 后按相同形态可校验
	if err := checker.Trust(HostPort("example.com", 2222), key); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if err := checker.Verify(HostPort("example.com", 2222), key); err != nil {
		t.Fatalf("verify non-standard port: %v", err)
	}
	// 端口不同则视为另一台主机
	if err := checker.Verify(HostPort("example.com", 2200), key); err == nil {
		t.Fatal("different port must not match")
	}
}
