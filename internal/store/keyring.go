package store

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringSecretStore 基于 OS keychain 的凭据存取。
// Windows: Credential Manager / macOS: Keychain / Linux: Secret Service(DBus)。
type keyringSecretStore struct {
	service string // keychain 中的条目命名空间
}

// NewKeyringSecretStore 创建凭据存取器。
// M4 将补充：keychain 不可用时回退主密码 AES-GCM 加密容器（见 ARCHITECTURE.md）。
func NewKeyringSecretStore(service string) SecretStore {
	return &keyringSecretStore{service: service}
}

func (k *keyringSecretStore) SaveSecret(ref, secret string) error {
	if err := keyring.Set(k.service, ref, secret); err != nil {
		return fmt.Errorf("keyring set: %w", err) // 错误信息只含引用键，不含凭据
	}
	return nil
}

func (k *keyringSecretStore) GetSecret(ref string) (string, error) {
	v, err := keyring.Get(k.service, ref)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("keyring get: %w", err)
	}
	return v, nil
}

func (k *keyringSecretStore) DeleteSecret(ref string) error {
	err := keyring.Delete(k.service, ref)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keyring delete: %w", err)
	}
	return nil
}
