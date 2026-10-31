package ssh

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestOutputBatcherMergesBursts(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]byte

	b := newOutputBatcher(20*time.Millisecond, func(data []byte) {
		mu.Lock()
		flushed = append(flushed, append([]byte(nil), data...))
		mu.Unlock()
	})

	// 5ms 间隔连写 4 笔，应被合并为 ≤2 次刷出（20ms 窗口）
	go func() {
		for i := 0; i < 4; i++ {
			b.Write([]byte{byte('a' + i)})
			time.Sleep(5 * time.Millisecond)
		}
	}()
	time.Sleep(80 * time.Millisecond)
	b.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) == 0 || len(flushed) > 2 {
		t.Fatalf("expected 1-2 flushes for burst, got %d", len(flushed))
	}
	var merged bytes.Buffer
	for _, f := range flushed {
		merged.Write(f)
	}
	if merged.String() != "abcd" {
		t.Fatalf("data lost/reordered: %q", merged.String())
	}
}

func TestOutputBatcherCloseFlushesRemaining(t *testing.T) {
	var got []byte
	done := make(chan struct{})
	b := newOutputBatcher(time.Hour, func(data []byte) { // 窗口拉长：只有 Close 能触发
		got = append([]byte(nil), data...)
		close(done)
	})
	b.Write([]byte("tail"))
	b.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not flush remaining data")
	}
	if string(got) != "tail" {
		t.Fatalf("flushed %q, want tail", got)
	}
}
