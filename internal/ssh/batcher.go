package ssh

import (
	"sync"
	"time"
)

// outputBatcher 合并高频 PTY 输出：第一笔数据到达时启动 interval 定时器，
// 到点把累积数据一次性刷出（协议约定 ≤16ms 一批，见 ARCHITECTURE.md）。
// 同时实现 io.Writer，可直接挂到 ssh.Session 的 Stdout/Stderr。
type outputBatcher struct {
	interval time.Duration
	mu       sync.Mutex
	buf      []byte
	timer    *time.Timer
	flush    func(data []byte)
}

func newOutputBatcher(interval time.Duration, flush func(data []byte)) *outputBatcher {
	return &outputBatcher{interval: interval, flush: flush}
}

func (b *outputBatcher) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	if b.timer == nil {
		b.timer = time.AfterFunc(b.interval, b.fire)
	}
	b.mu.Unlock()
	return len(p), nil
}

func (b *outputBatcher) fire() {
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	data := b.buf
	b.buf = nil
	f := b.flush
	b.mu.Unlock()
	if len(data) > 0 && f != nil {
		f(data)
	}
}

// Close 刷出残余数据（会话结束时调用）
func (b *outputBatcher) Close() {
	b.fire()
}
