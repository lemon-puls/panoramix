package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDirSorting(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// 目录在前（按名称），文件在后
	if !entries[0].IsDir || entries[0].Name != "adir" {
		t.Fatalf("first entry should be adir, got %+v", entries[0])
	}
	if !entries[1].IsDir || entries[1].Name != "zdir" {
		t.Fatalf("second entry should be zdir, got %+v", entries[1])
	}
	if entries[2].IsDir || entries[2].Name != "b.txt" {
		t.Fatalf("third entry should be b.txt, got %+v", entries[2])
	}
}

func TestHomeAndParent(t *testing.T) {
	h := Home()
	if h == "" {
		t.Fatal("home is empty")
	}
	// 根目录的父目录是自身
	if root := string(filepath.Separator); Parent(root) != root {
		t.Fatalf("parent of root should be root, got %s", Parent(root))
	}
	// 子目录的父目录正确
	sub := filepath.Join(h, "sub")
	if Parent(sub) != filepath.Clean(h) {
		t.Fatalf("parent mismatch: %s", Parent(sub))
	}
	// 空路径回退主目录
	if Parent("") != h {
		t.Fatalf("empty parent should be home")
	}
}
