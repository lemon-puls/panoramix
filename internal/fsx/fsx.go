// Package fsx 提供本地文件系统浏览能力（双面板的本地一栏）。
// 仅做只读列目录与路径解析，不涉及任何凭据或远程操作。
package fsx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry 本地目录项（前端列表行）
type Entry struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	IsDir bool      `json:"isDir"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
}

// Home 返回用户主目录（本地面板初始位置）
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return string(filepath.Separator)
	}
	return h
}

// ListDir 列本地目录（目录在前，按名称排序）；dir 为空时取主目录
func ListDir(dir string) ([]Entry, error) {
	if dir == "" {
		dir = Home()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			continue // 并发删除等竞态：跳过该项
		}
		out = append(out, Entry{
			Name:  e.Name(),
			Path:  filepath.Join(dir, e.Name()),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			Mtime: info.ModTime(),
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

// Parent 返回父目录（根目录返回自身）
func Parent(dir string) string {
	if dir == "" {
		return Home()
	}
	p := filepath.Dir(dir)
	if p == dir {
		return dir
	}
	return p
}
