// Package sftp 提供远程文件操作与传输队列。
// 依赖方向：app → sftp；sftp 不 import internal/ssh，
// 通过 ClientSource 接口取已连接会话的 SSH 客户端（见 ARCHITECTURE.md）。
package sftp

import (
	"fmt"
	"path"
	"sort"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// ClientSource 从会话 ID 取底层 SSH 客户端（app 层注入 ssh.Manager 适配）
type ClientSource interface {
	Client(sessionID string) (*gossh.Client, error)
}

// Emitter 后端 → 前端事件推送接口（与 ssh/app 中的定义结构一致）
type Emitter interface {
	Emit(event string, data ...interface{})
}

// NewSftpClient 打开一个 SFTP 子系统客户端（每个任务/操作独立，用完关闭）
func NewSftpClient(src ClientSource, sessionID string) (*sftp.Client, error) {
	client, err := src.Client(sessionID)
	if err != nil {
		return nil, err
	}
	return sftp.NewClient(client)
}

// RemoteEntry 远程文件条目（前端列表行）
type RemoteEntry struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	IsDir bool      `json:"isDir"`
	Size  int64     `json:"size"`
	Mode  string    `json:"mode"`
	Mtime time.Time `json:"mtime"`
}

// ListDir 列目录（目录在前、按名称排序；dir 为空则取用户主目录）
func ListDir(cli *sftp.Client, dir string) ([]RemoteEntry, error) {
	if dir == "" {
		wd, err := cli.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
		dir = wd
	}
	entries, err := cli.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", dir, err)
	}
	out := make([]RemoteEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, RemoteEntry{
			Name:  e.Name(),
			Path:  path.Join(dir, e.Name()),
			IsDir: e.IsDir(),
			Size:  e.Size(),
			Mode:  e.Mode().String(),
			Mtime: e.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Mkdir 创建目录（v0.1：父目录需已存在）
func Mkdir(cli *sftp.Client, dir string) error {
	return cli.Mkdir(dir)
}

// Rename 重命名/移动
func Rename(cli *sftp.Client, oldPath, newPath string) error {
	return cli.PosixRename(oldPath, newPath)
}

// RemoveAll 递归删除文件或目录
func RemoveAll(cli *sftp.Client, target string) error {
	fi, err := cli.Stat(target)
	if err != nil {
		return fmt.Errorf("stat %s: %w", target, err)
	}
	if !fi.IsDir() {
		return cli.Remove(target)
	}
	entries, err := cli.ReadDir(target)
	if err != nil {
		return fmt.Errorf("readdir %s: %w", target, err)
	}
	for _, e := range entries {
		sub := path.Join(target, e.Name())
		if e.IsDir() {
			if err := RemoveAll(cli, sub); err != nil {
				return err
			}
		} else if err := cli.Remove(sub); err != nil {
			return err
		}
	}
	return cli.RemoveDirectory(target)
}
