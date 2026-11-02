//go:build windows

package fsx

import "os"

// Roots 返回可用的盘符根（如 C:\, D:\），用于本地面板的盘符切换
func Roots() []string {
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if _, err := os.Stat(root); err == nil {
			out = append(out, root)
		}
	}
	return out
}
