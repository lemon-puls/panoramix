// Package monitor 提供会话级系统监控：经独立的监控通道周期读取
// /proc、/sys 与 df/ps 输出，解析为结构化指标并经事件推送到前端。
// 失败静默降级（不报错刷屏）；本包不感知 Wails（见 ARCHITECTURE.md）。
package monitor

import (
	"fmt"
	"strconv"
	"strings"
)

// Metrics 单次采样的全部指标（monitor:sample 事件负载）
type Metrics struct {
	HostID    string      `json:"hostId"`
	SessionID string      `json:"sessionId"`
	TS        int64       `json:"ts"` // 毫秒时间戳
	CPU       CPUUsage    `json:"cpu"`
	LoadAvg   [3]float64  `json:"loadAvg"`
	Mem       MemInfo     `json:"mem"`
	Disks     []DiskUsage `json:"disks"`
	DiskIO    IORates     `json:"diskIo"`
	Net       NetRates    `json:"net"`
	Processes []Process   `json:"processes"`
	GPU       []GPUInfo   `json:"gpu,omitempty"`
}

type CPUUsage struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
}

type MemInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
	SwapTotal uint64 `json:"swapTotal"`
	SwapUsed  uint64 `json:"swapUsed"`
}

type DiskUsage struct {
	Mount   string  `json:"mount"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

type IORates struct {
	ReadBps  float64 `json:"readBps"`
	WriteBps float64 `json:"writeBps"`
}

type NetRates struct {
	RecvBps float64 `json:"recvBps"`
	SentBps float64 `json:"sentBps"`
}

type Process struct {
	PID  int     `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	MEM  float64 `json:"mem"`
}

type GPUInfo struct {
	Index     int     `json:"index"`
	Name      string  `json:"name"`
	Util      float64 `json:"util"`
	MemUsedMB uint64  `json:"memUsedMB"`
	MemTotMB  uint64  `json:"memTotMB"`
	TempC     float64 `json:"tempC"`
}

// sampleCommand 一次采样执行的远端命令：带标记地拼接全部 /proc 数据源
// 与 df/ps 输出。任何子项缺失（如容器内无 diskstats）静默跳过。
func sampleCommand() string {
	files := "/proc/loadavg /proc/stat /proc/meminfo /proc/net/dev /proc/diskstats"
	return `for f in ` + files + `; do echo "== $f"; cat "$f" 2>/dev/null; done` +
		`; echo "== df"; df -P 2>/dev/null` +
		`; echo "== ps"; ps -eo pid,comm,pcpu,pmem --sort=-pcpu 2>/dev/null | head -16`
}

// parseSample 解析一次采样的原始输出。
// prev 为上一次快照（用于 CPU/网络/磁盘 IO 的速率差分）；首采时 prev 为 nil，
// 速率类指标为 0。返回本次快照供调用方保存。
func parseSample(raw string, prev *snapshot) (Metrics, *snapshot, error) {
	var m Metrics
	var cpuLine string
	statCores := 0
	var netLines, diskLines, dfLines, psLines []string
	current := ""

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "== ") {
			current = strings.TrimSpace(line[3:])
			continue
		}
		switch current {
		case "/proc/loadavg":
			if parts := strings.Fields(line); len(parts) >= 3 && m.LoadAvg == [3]float64{} {
				m.LoadAvg = [3]float64{atof(parts[0]), atof(parts[1]), atof(parts[2])}
			}
		case "/proc/stat":
			if strings.HasPrefix(line, "cpu ") && cpuLine == "" {
				cpuLine = strings.TrimSpace(line)
			} else if strings.HasPrefix(line, "cpu") && line != "cpu " {
				statCores++
			}
		case "/proc/meminfo":
			parseMemLine(line, &m.Mem)
		case "/proc/net/dev":
			// 数据行形如 "  eth0: bytes packets ..."；表头行无冒号
			if strings.Contains(line, ":") {
				netLines = append(netLines, line)
			}
		case "/proc/diskstats":
			if strings.TrimSpace(line) != "" {
				diskLines = append(diskLines, line)
			}
		case "df":
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "Filesystem") {
				dfLines = append(dfLines, line)
			}
		case "ps":
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "PID") {
				psLines = append(psLines, line)
			}
		}
	}

	// CPU：与上次快照差分
	cur := newSnapshot(cpuLine, netLines, diskLines)
	if prev != nil && cur != nil {
		m.CPU.Percent = cpuPercent(prev.cpuTotal, cur.cpuTotal)
		m.CPU.Cores = int(statCores)
		m.Net = netRates(prev.net, cur.net)
		m.DiskIO = ioRates(prev.disk, cur.disk)
	} else {
		m.CPU.Cores = int(statCores)
	}

	// 磁盘用量（df -P：仅 /dev 开头的真实块设备，按挂载点去重）
	seen := map[string]bool{}
	for _, line := range dfLines {
		f := strings.Fields(line)
		if len(f) < 6 || !strings.HasPrefix(f[0], "/dev") || seen[f[5]] {
			continue
		}
		seen[f[5]] = true
		total := atofu64(f[1]) * 1024
		used := atofu64(f[2]) * 1024
		pct := 0.0
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}
		m.Disks = append(m.Disks, DiskUsage{Mount: f[5], Total: total, Used: used, Percent: pct})
	}

	// Top 进程
	for _, line := range psLines {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		m.Processes = append(m.Processes, Process{
			PID: pid, Name: f[1], CPU: atof(f[2]), MEM: atof(f[3]),
		})
	}

	if cur == nil {
		return m, nil, fmt.Errorf("no cpu data")
	}
	return m, cur, nil
}

// parseMemLine 解析 meminfo 单行（关注 Total/Free/Available/Swap）
func parseMemLine(line string, mem *MemInfo) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return
	}
	key := strings.TrimSpace(line[:idx])
	kb := atofu64(strings.Fields(strings.TrimSpace(line[idx+1:]))[0]) * 1024
	switch key {
	case "MemTotal":
		mem.Total = kb
	case "MemAvailable":
		mem.Available = kb
	case "MemFree":
		// 仅当无 Available 时兜底（老内核）
		if mem.Available == 0 {
			mem.Available = kb
		}
	case "SwapTotal":
		mem.SwapTotal = kb
	case "SwapFree":
		mem.SwapUsed = mem.SwapTotal - min64(mem.SwapTotal, kb)
	}
	if mem.Total > 0 {
		mem.Used = mem.Total - min64(mem.Total, mem.Available)
	}
}

func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// snapshot 采样的差分基数（CPU 累计时间、网卡/磁盘累计字节/扇区）
type snapshot struct {
	cpuTotal []uint64
	net      map[string][2]uint64 // iface → recv/sent bytes
	disk     map[string][2]uint64 // device → read/written sectors
}

func newSnapshot(cpuLine string, netLines, diskLines []string) *snapshot {
	s := &snapshot{net: map[string][2]uint64{}, disk: map[string][2]uint64{}}
	if cpuLine != "" {
		for _, f := range strings.Fields(strings.TrimPrefix(cpuLine, "cpu")) {
			if v, err := strconv.ParseUint(f, 10, 64); err == nil {
				s.cpuTotal = append(s.cpuTotal, v)
			}
		}
	}
	for _, line := range netLines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		f := strings.Fields(parts[1])
		if len(f) >= 9 {
			s.net[name] = [2]uint64{atofu64(f[0]), atofu64(f[8])}
		}
	}
	for _, line := range diskLines {
		f := strings.Fields(line)
		if len(f) < 11 || strings.HasPrefix(f[2], "loop") || strings.HasPrefix(f[2], "ram") {
			continue
		}
		// f[5]=读完成数 f[9]=写完成数（扇区数在 f[5]/f[9] 之后：sectors read 是第 6 列即 f[5]？）
		// diskstats 布局：major minor name reads completed sectorsRead msReading writes
		// completed sectorsWritten msWriting ...
		s.disk[f[2]] = [2]uint64{atofu64(f[5]), atofu64(f[9])}
	}
	return s
}

func cpuPercent(prev, cur []uint64) float64 {
	if len(prev) == 0 || len(cur) == 0 || len(prev) != len(cur) {
		return 0
	}
	var dTotal, dIdle uint64
	for i := range cur {
		d := cur[i] - prev[i]
		if cur[i] < prev[i] { // 计数器回绕/热插拔：放弃本周期
			return 0
		}
		dTotal += d
		if i >= 3 && i <= 4 { // idle + iowait
			dIdle += d
		}
	}
	if dTotal == 0 {
		return 0
	}
	pct := float64(dTotal-dIdle) / float64(dTotal) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

func netRates(prev, cur map[string][2]uint64) NetRates {
	var r NetRates
	for name, c := range cur {
		p, ok := prev[name]
		if !ok || c[0] < p[0] || c[1] < p[1] {
			continue // 新出现的接口或计数器回绕：跳过本周期
		}
		r.RecvBps += float64(c[0] - p[0])
		r.SentBps += float64(c[1] - p[1])
	}
	return r
}

func ioRates(prev, cur map[string][2]uint64) IORates {
	var r IORates
	for dev, c := range cur {
		p, ok := prev[dev]
		if !ok || c[0] < p[0] || c[1] < p[1] {
			continue
		}
		r.ReadBps += float64(c[0]-p[0]) * 512
		r.WriteBps += float64(c[1]-p[1]) * 512
	}
	return r
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func atofu64(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}
