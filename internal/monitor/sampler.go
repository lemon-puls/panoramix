package monitor

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// Runner 在指定会话上执行一次性命令（app 层注入 ssh.Manager 适配）
type Runner interface {
	Run(sessionID, cmd string) (string, error)
}

// Emitter 后端 → 前端事件推送接口（与 ssh/sftp 中的定义结构一致）
type Emitter interface {
	Emit(event string, data ...interface{})
}

const defaultInterval = 2 * time.Second

// gpuProbeCommand GPU 探测：失败（无 NVIDIA 卡/无驱动）即永久禁用该会话的 GPU 卡片
const gpuProbeCommand = `nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu --format=csv,noheader,nounits`

// Sampler 单会话的监控采样器：独立通道周期采样（与终端 PTY 互不干扰），
// 失败静默降级——连续 3 次失败自动停止并通知前端。
type Sampler struct {
	src      Runner
	em       Emitter
	hostID   string
	session  string
	interval time.Duration

	mu       sync.Mutex
	prev     *snapshot
	gpuOK    *bool // nil=未探测
	stopped  bool
	failures int
}

func NewSampler(src Runner, em Emitter, hostID, session string, interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Sampler{src: src, em: em, hostID: hostID, session: session, interval: interval}
}

// Start 启动采样循环（首采立即执行，速率类指标首采为 0）。
// 阻塞至 Stop 或连续失败退出，调用方放 goroutine。
func (s *Sampler) Start() {
	s.sampleOnce() // 立即首采（速率类指标为 0）
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		}
		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}
		if !s.sampleOnce() {
			return
		}
	}
}

// sampleOnce 执行一次采样；返回 false 表示采样器应终止
func (s *Sampler) sampleOnce() bool {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	raw, err := s.src.Run(s.session, sampleCommand())
	if err != nil {
		s.fail()
		return s.shouldContinue()
	}
	raw = strings.TrimRight(raw, "\n")

	gpu := s.sampleGPU()

	s.mu.Lock()
	prev := s.prev
	s.mu.Unlock()
	metrics, cur, perr := parseSample(raw, prev)
	if perr != nil {
		s.fail()
		return s.shouldContinue()
	}
	s.mu.Lock()
	s.prev = cur
	s.failures = 0
	s.mu.Unlock()

	metrics.HostID = s.hostID
	metrics.SessionID = s.session
	metrics.TS = time.Now().UnixMilli()
	metrics.GPU = gpu
	s.em.Emit("monitor:sample", metrics)
	return true
}

// sampleGPU GPU 采样：首次探测，失败永久跳过；之后每 N 个周期刷新一次由调用节奏决定
func (s *Sampler) sampleGPU() []GPUInfo {
	s.mu.Lock()
	known := s.gpuOK
	s.mu.Unlock()
	if known != nil && !*known {
		return nil
	}
	out, err := s.src.Run(s.session, gpuProbeCommand)
	if err != nil || strings.TrimSpace(out) == "" {
		ok := false
		s.mu.Lock()
		if known == nil {
			s.gpuOK = &ok
		}
		s.mu.Unlock()
		return nil
	}
	ok := true
	s.mu.Lock()
	s.gpuOK = &ok
	s.mu.Unlock()
	return parseGPU(out)
}

func (s *Sampler) fail() {
	s.mu.Lock()
	s.failures++
	stopped := false
	if s.failures >= 3 {
		s.stopped = true
		stopped = true
	}
	s.mu.Unlock()
	if stopped {
		// 静默降级：通知一次前端（卡片置灰），不刷错误
		s.em.Emit("monitor:state", map[string]interface{}{
			"hostId": s.hostID, "sessionId": s.session, "state": "stopped",
		})
	}
}

func (s *Sampler) shouldContinue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.stopped
}

// Stop 置停止标记（循环在下个 tick 退出）
func (s *Sampler) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// parseGPU 解析 nvidia-smi CSV：index,name,util%,memUsedMB,memTotalMB,tempC
func parseGPU(out string) []GPUInfo {
	var gpus []GPUInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) < 6 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(f[0]))
		if err != nil {
			continue
		}
		gpus = append(gpus, GPUInfo{
			Index:     idx,
			Name:      strings.TrimSpace(f[1]),
			Util:      atof(strings.TrimSpace(f[2])),
			MemUsedMB: atofu64(strings.TrimSpace(f[3])),
			MemTotMB:  atofu64(strings.TrimSpace(f[4])),
			TempC:     atof(strings.TrimSpace(f[5])),
		})
	}
	return gpus
}
