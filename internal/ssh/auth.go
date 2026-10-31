package ssh

import (
	"errors"
	"fmt"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

// buildAuth 根据认证类型构造 SSH 认证方法列表。
// Password/Passphrase 是 keychain 解密结果，仅存在于内存，严禁写日志。
func buildAuth(cfg ConnectConfig) ([]gossh.AuthMethod, error) {
	switch cfg.AuthType {
	case "password", "keyboard_interactive":
		if cfg.Password == "" {
			return nil, errors.New("no password stored for this host")
		}
		methods := []gossh.AuthMethod{gossh.Password(cfg.Password)}
		// v0.1 约定：keyboard-interactive 的所有问题都用已存密码应答
		// （覆盖"密码认证走 keyboard-interactive 协议"的服务器；逐项 2FA 应答后续版本支持）
		methods = append(methods,
			gossh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = cfg.Password
				}
				return answers, nil
			}))
		return methods, nil
	case "key":
		return buildKeyAuth(cfg.KeyPath, cfg.Passphrase)
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", cfg.AuthType)
	}
}

func buildKeyAuth(keyPath, passphrase string) ([]gossh.AuthMethod, error) {
	if keyPath == "" {
		return nil, errors.New("no key path configured for this host")
	}
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	var signer gossh.Signer
	if passphrase != "" {
		signer, err = gossh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
	} else {
		signer, err = gossh.ParsePrivateKey(pemBytes)
		if err != nil {
			var perr *gossh.PassphraseMissingError
			if errors.As(err, &perr) {
				return nil, errors.New("private key requires a passphrase")
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return []gossh.AuthMethod{gossh.PublicKeys(signer)}, nil
}
