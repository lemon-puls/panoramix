package ssh

import (
	"strings"
	"testing"
)

func TestBuildKeyAuthWithPassphrase(t *testing.T) {
	dir := t.TempDir()
	_, priv := newTestKey(t)
	path := writeTestPrivateKey(t, dir, "id_ed25519", priv, "secret123")

	// 正确 passphrase
	auth, err := buildKeyAuth(path, "secret123")
	if err != nil || len(auth) != 1 {
		t.Fatalf("build with passphrase: err=%v len=%d", err, len(auth))
	}

	// 错误 passphrase 必须报错
	if _, err := buildKeyAuth(path, "wrong"); err == nil {
		t.Fatal("wrong passphrase should fail")
	}

	// 未提供 passphrase 必须报"requires passphrase"类错误
	_, err = buildKeyAuth(path, "")
	if err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected passphrase-required error, got %v", err)
	}
}

func TestBuildKeyAuthWithoutPassphrase(t *testing.T) {
	dir := t.TempDir()
	_, priv := newTestKey(t)
	path := writeTestPrivateKey(t, dir, "id_ed25519", priv, "")

	auth, err := buildKeyAuth(path, "")
	if err != nil || len(auth) != 1 {
		t.Fatalf("build without passphrase: err=%v len=%d", err, len(auth))
	}
}

func TestBuildAuthValidation(t *testing.T) {
	// password 类型但未存密码
	_, err := buildAuth(ConnectConfig{AuthType: "password"})
	if err == nil || !strings.Contains(err.Error(), "no password") {
		t.Fatalf("expected no-password error, got %v", err)
	}

	// key 类型但未配置路径
	_, err = buildAuth(ConnectConfig{AuthType: "key"})
	if err == nil || !strings.Contains(err.Error(), "no key path") {
		t.Fatalf("expected no-key-path error, got %v", err)
	}

	// 未知类型
	_, err = buildAuth(ConnectConfig{AuthType: "certificate"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}

	// 密码类型应同时产出 password + keyboard-interactive 两种方法
	auth, err := buildAuth(ConnectConfig{AuthType: "keyboard_interactive", Password: "pw"})
	if err != nil || len(auth) != 2 {
		t.Fatalf("expected 2 auth methods, got err=%v len=%d", err, len(auth))
	}
}

// 编译期守卫：确保 Password 不会意外进入错误信息（脱敏红线）
func TestAuthErrorNoSecretLeak(t *testing.T) {
	secret := "SUPER-SECRET-PW-42"
	// 触发一个必然失败的构建路径（未知认证类型），检查错误内容
	_, err := buildAuth(ConnectConfig{AuthType: "unsupported", Password: secret})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("auth error leaked password: %v", err)
	}
}
