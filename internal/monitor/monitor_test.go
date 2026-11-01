package monitor

import (
	"strings"
	"testing"
	"time"
)

// 构造真实形态的 /proc 输出片段
func fixtureRaw(cpu1 string) string {
	return strings.Join([]string{
		"== /proc/loadavg",
		"0.15 0.28 0.31 1/912 12345",
		"== /proc/stat",
		cpu1,
		"cpu0 100 0 100 700 0 0 0 0 0 0",
		"intr 12345",
		"== /proc/meminfo",
		"MemTotal:       16384000 kB",
		"MemFree:         2048000 kB",
		"MemAvailable:    8192000 kB",
		"SwapTotal:       2097152 kB",
		"SwapFree:        1048576 kB",
		"== /proc/net/dev",
		"Inter-|   Receive                                                |  Transmit",
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed",
		"    lo: 1234567    9876    0    0    0     0          0         0  1234567    9876    0    0    0     0       0          0",
		"  eth0: 1000000000  800000    0    0    0     0          0         0 500000000  600000    0    0    0     0       0          0",
		"== /proc/diskstats",
		"   8       0 sda 12345 678 900000 12345 67890 123 4500000 23456 0 45678 35801",
		" 259       0 nvme0n1 999 111 22222 333 444 55 666666 777 0 888 999",
		"   7       0 loop0 5 0 10 0 0 0 0 0 0 0 0",
		"== df",
		"Filesystem     1024-blocks     Used Available Capacity Mounted on",
		"/dev/vda3       39015862  7803172  29182904      22% /",
		"tmpfs             819200        0    819200       0% /dev/shm",
		"== ps",
		"    PID COMMAND          %CPU %MEM",
		"   1234 mysqld           12.5  8.2",
		"    567 nginx            3.2  1.1",
	}, "\n")
}

const cpuT0 = "cpu  1000 0 1000 90000 0 0 0 0 0 0"
// Δ: user+2500 nice0 sys+2500 idle+5000 → 总 10000，非 idle 5000 → CPU 50%
const cpuT1 = "cpu  3500 0 3500 95000 0 0 0 0 0 0"

func TestParseSampleFirst(t *testing.T) {
	m, snap, err := parseSample(fixtureRaw(cpuT0), nil)
	if err != nil {
		t.Fatalf("first sample: %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot nil")
	}
	if m.LoadAvg[0] != 0.15 || m.LoadAvg[2] != 0.31 {
		t.Fatalf("loadavg: %v", m.LoadAvg)
	}
	if m.Mem.Total != 16384000*1024 {
		t.Fatalf("mem total: %d", m.Mem.Total)
	}
	if m.Mem.Available != 8192000*1024 {
		t.Fatalf("mem available: %d", m.Mem.Available)
	}
	if m.Mem.Used != (16384000-8192000)*1024 {
		t.Fatalf("mem used: %d", m.Mem.Used)
	}
	if m.Mem.SwapUsed != 1048576*1024 {
		t.Fatalf("swap used: %d", m.Mem.SwapUsed)
	}
	// 磁盘用量：tmpfs 过滤、/ 挂载保留（7803172/39015862 ≈ 20%）
	if len(m.Disks) != 1 || m.Disks[0].Mount != "/" || m.Disks[0].Percent < 19 || m.Disks[0].Percent > 21 {
		t.Fatalf("disks: %+v", m.Disks)
	}
	// Top 进程按 CPU 排序（ps 已排）
	if len(m.Processes) != 2 || m.Processes[0].Name != "mysqld" {
		t.Fatalf("processes: %+v", m.Processes)
	}
	// 首采无差分
	if m.CPU.Percent != 0 || m.Net.RecvBps != 0 || m.DiskIO.ReadBps != 0 {
		t.Fatalf("first sample rates must be zero: %+v", m)
	}
	// 差分基数：eth0 累计、sda 扇区（loop 过滤）
	if snap.net["eth0"] != [2]uint64{1000000000, 500000000} {
		t.Fatalf("net snapshot: %v", snap.net)
	}
	if snap.disk["sda"] != [2]uint64{900000, 4500000} {
		t.Fatalf("disk snapshot: %v", snap.disk)
	}
	if _, ok := snap.disk["loop0"]; ok {
		t.Fatal("loop device must be excluded")
	}
	// CPU 核数：cpu0（非 "cpu " 聚合行）
	if m.CPU.Cores != 1 {
		t.Fatalf("cores: %d", m.CPU.Cores)
	}
}

func TestParseSampleRates(t *testing.T) {
	_, prev, _ := parseSample(fixtureRaw(cpuT0), nil)

	// 1 秒后：CPU 非空闲 50%；eth0 收 +1000000 字节；sda 读 +2000000 扇区（=1GB）
	raw2 := strings.Replace(fixtureRaw(cpuT1), "  eth0: 1000000000", "  eth0: 1001000000", 1)
	raw2 = strings.Replace(raw2, "sda 12345 678 900000 12345 67890 123 4500000", "sda 12345 678 1100000 12345 67890 123 6500000", 1)
	raw2 = strings.Replace(raw2, "500000000  600000", "500000000  600000", 1)

	m, cur, err := parseSample(raw2, prev)
	if err != nil {
		t.Fatalf("second sample: %v", err)
	}
	if m.CPU.Percent < 49 || m.CPU.Percent > 51 {
		t.Fatalf("cpu percent: %v", m.CPU.Percent)
	}
	if m.Net.RecvBps != 1000000 {
		t.Fatalf("net recv: %v", m.Net)
	}
	// reads completed +200000（扇区数不变）→ 读速率 200000×512B
	if m.DiskIO.ReadBps != 200000*512 {
		t.Fatalf("disk read: %v", m.DiskIO)
	}
	if cur.net["eth0"][0] != 1001000000 {
		t.Fatalf("cur net snapshot: %v", cur.net)
	}
}

func TestParseGPUNvidiaCSV(t *testing.T) {
	gpus := parseGPU("0, NVIDIA A100, 42, 8192, 40960, 65\n1, NVIDIA RTX 4090, 97, 12288, 24564, 71\n")
	if len(gpus) != 2 {
		t.Fatalf("gpu count: %d", len(gpus))
	}
	if gpus[0].Name != "NVIDIA A100" || gpus[0].Util != 42 || gpus[0].MemUsedMB != 8192 || gpus[0].TempC != 65 {
		t.Fatalf("gpu0: %+v", gpus[0])
	}
	if gpus[1].Index != 1 || gpus[1].MemTotMB != 24564 {
		t.Fatalf("gpu1: %+v", gpus[1])
	}
	if gpus := parseGPU(""); gpus != nil {
		t.Fatalf("empty gpu output should be nil")
	}
}

func TestSampleCommandContainsAllSources(t *testing.T) {
	cmd := sampleCommand()
	for _, want := range []string{"/proc/loadavg", "/proc/stat", "/proc/meminfo", "/proc/net/dev", "/proc/diskstats", "df -P", "ps -eo"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("sample command missing %s", want)
		}
	}
}

// 防止将来有人把默认间隔改坏
func TestDefaultInterval(t *testing.T) {
	s := NewSampler(nil, nil, "h", "s", 0)
	if s.interval != 2*time.Second {
		t.Fatalf("default interval: %v", s.interval)
	}
}
