//go:build !windows

package fsx

// Roots 返回可用的文件系统根（Unix 只有一个 /）
func Roots() []string {
	return []string{"/"}
}
