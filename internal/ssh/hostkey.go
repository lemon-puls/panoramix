package ssh

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyError 连接阶段的 known_hosts 异常（TOFU 策略）。
// Kind=unknown 表示首次连接未知主机（前端弹窗展示指纹让用户确认）；
// Kind=changed 表示指纹变更（红色警告，需用户明确确认才能替换）。
type HostKeyError struct {
	Kind        string
	HostPort    string
	Fingerprint string // SHA256:... 格式
	Err         error
}

func (e *HostKeyError) Error() string {
	if e.Kind == "changed" {
		return fmt.Sprintf("host key CHANGED for %s (fingerprint %s)", e.HostPort, e.Fingerprint)
	}
	return fmt.Sprintf("unknown host key for %s (fingerprint %s)", e.HostPort, e.Fingerprint)
}

func (e *HostKeyError) Unwrap() error { return e.Err }

// HostKeyChecker known_hosts 文件的校验与维护（OpenSSH 兼容格式）。
// TOFU 流程：首连 unknown -> 前端确认 -> Trust 追加；
// 指纹 changed -> 红色警告 -> 用户明确确认后 Replace 整行替换。
type HostKeyChecker struct {
	path string
	mu   sync.Mutex
}

// NewHostKeyChecker 创建校验器，path 为 known_hosts 文件路径
func NewHostKeyChecker(path string) *HostKeyChecker {
	return &HostKeyChecker{path: path}
}

// addrStub net.Addr 存根：knownhosts 校验器要求非 nil 的 remote 地址
type addrStub struct{ hostPort string }

func (a addrStub) Network() string { return "tcp" }
func (a addrStub) String() string  { return a.hostPort }

// Verify 校验主机公钥。返回 nil 表示匹配；
// 返回 *HostKeyError 表示未知主机（TOFU 候选）或指纹变更。
func (c *HostKeyChecker) Verify(hostPort string, key gossh.PublicKey) error {
	norm := knownhosts.Normalize(hostPort)
	c.mu.Lock()
	defer c.mu.Unlock()

	verifier, err := knownhosts.New(c.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// 文件尚不存在：全部主机都是未知的
			return &HostKeyError{Kind: "unknown", HostPort: norm,
				Fingerprint: gossh.FingerprintSHA256(key), Err: err}
		}
		return fmt.Errorf("read known_hosts: %w", err)
	}
	if verr := verifier(hostPort, addrStub{hostPort: hostPort}, key); verr != nil {
		var ke *knownhosts.KeyError
		if errors.As(verr, &ke) {
			hke := &HostKeyError{HostPort: norm,
				Fingerprint: gossh.FingerprintSHA256(key), Err: verr}
			if len(ke.Want) == 0 {
				hke.Kind = "unknown"
			} else {
				hke.Kind = "changed"
			}
			return hke
		}
		return verr
	}
	return nil
}

// Trust 追加主机公钥条目（TOFU 首连确认后调用）
func (c *HostKeyChecker) Trust(hostPort string, key gossh.PublicKey) error {
	return c.write(hostPort, key, false)
}

// Replace 先删除该主机全部旧条目再追加新条目。
// 仅在指纹变更且用户明确确认后调用——这是防御中间人的最后一道闸门。
func (c *HostKeyChecker) Replace(hostPort string, key gossh.PublicKey) error {
	return c.write(hostPort, key, true)
}

func (c *HostKeyChecker) write(hostPort string, key gossh.PublicKey, replace bool) error {
	norm := knownhosts.Normalize(hostPort)
	line := knownhosts.Line([]string{norm}, key)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	if replace {
		if err := c.removeLocked(norm); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	return nil
}

// removeLocked 删除该主机的全部旧条目（调用方需持有 mu）
func (c *HostKeyChecker) removeLocked(norm string) error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read known_hosts: %w", err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			continue // 跳过畸形行
		}
		if !hostListContains(fields[0], norm) {
			kept = append(kept, trimmed)
		}
	}
	if len(kept) == 0 {
		// 清空后直接删文件，避免留空文件
		if err := os.Remove(c.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove known_hosts: %w", err)
		}
		return nil
	}
	content := strings.Join(kept, "\n") + "\n"
	if err := os.WriteFile(c.path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("rewrite known_hosts: %w", err)
	}
	return nil
}

func hostListContains(hostList, norm string) bool {
	for _, h := range strings.Split(hostList, ",") {
		if h == norm {
			return true
		}
	}
	return false
}

// HostPort 拼接 addr:port（known_hosts 规范化前的形态）
func HostPort(addr string, port int) string {
	return net.JoinHostPort(addr, strconv.Itoa(port))
}
