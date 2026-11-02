package sftp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSftpQueuePauseResume 暂停期间进度冻结，恢复后继续并完成
func TestSftpQueuePauseResume(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	local := t.TempDir()
	q := NewQueue(dialSource{cli}, &captureEmitter{})

	// 80MB 模式化内容：留足暂停窗口
	big := make([]byte, 80*1024*1024)
	for i := range big {
		big[i] = byte(i % 241)
	}
	src := filepath.Join(local, "pausable.bin")
	if err := os.WriteFile(src, big, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	remoteDir := filepath.Join(root, "up")
	if _, err := q.Enqueue("s1", DirUpload, remoteDir, src); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// 等传出至少 1MB
	waitFor(t, 10*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateRunning && tk.Transferred >= 1*1024*1024 {
				return true
			}
		}
		return false
	}, "running with progress")

	var taskID string
	for _, tk := range q.Tasks() {
		taskID = tk.ID
	}
	if err := q.Pause(taskID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.ID == taskID && tk.State == StatePaused {
				return true
			}
		}
		return false
	}, "paused state")

	// 暂停后进度应基本冻结（允许一个在途块的余量）
	pausedAt := int64(0)
	for _, tk := range q.Tasks() {
		if tk.ID == taskID {
			pausedAt = tk.Transferred
		}
	}
	time.Sleep(800 * time.Millisecond)
	for _, tk := range q.Tasks() {
		if tk.ID == taskID {
			if tk.Transferred > pausedAt+2*1024*1024 {
				t.Fatalf("progress advanced while paused: %d -> %d", pausedAt, tk.Transferred)
			}
		}
	}

	if err := q.Resume(taskID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.ID == taskID && tk.State == StateDone {
				return true
			}
		}
		return false
	}, "done after resume")

	got, err := os.ReadFile(filepath.Join(remoteDir, "pausable.bin"))
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if sha(got) != sha(big) {
		t.Fatal("content mismatch after pause/resume")
	}
}

// TestSftpQueueSpeedLimit 限速生效：2MB 文件在 512KB/s 下至少需 ~3.5s
func TestSftpQueueSpeedLimit(t *testing.T) {
	port, closeSrv := startSftpSSHD(t)
	defer closeSrv()
	cli := dialTestClient(t, port)

	root := t.TempDir()
	local := t.TempDir()
	q := NewQueue(dialSource{cli}, &captureEmitter{})
	q.SetSpeedLimit(512) // KB/s

	size := 2 * 1024 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 233)
	}
	src := filepath.Join(local, "slow.bin")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	start := time.Now()
	if _, err := q.Enqueue("s1", DirUpload, filepath.Join(root, "up"), src); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool {
		for _, tk := range q.Tasks() {
			if tk.State == StateDone {
				return true
			}
		}
		return false
	}, "limited upload done")
	elapsed := time.Since(start)

	// 2MB @ 512KB/s ≈ 4s；给调度与协议开销留足下限
	if elapsed < 3500*time.Millisecond {
		t.Fatalf("speed limit not effective: finished in %v (expected >= 3.5s)", elapsed)
	}
	t.Logf("2MB @512KB/s took %v", elapsed)
}
