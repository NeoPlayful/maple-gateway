//go:build linux

package hostmetrics

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Collect 采样节点主机资源使用率（Linux：/proc + statfs）。
func Collect() (Metrics, error) {
	first, err := readCPU()
	if err != nil {
		return Metrics{}, err
	}
	// 两次采样求差值，得到瞬时 CPU 使用率（单点读的是开机以来均值）。
	time.Sleep(120 * time.Millisecond)
	second, err := readCPU()
	if err != nil {
		return Metrics{}, err
	}
	m := Metrics{Available: true}
	m.CPUPercent = cpuPercent(first, second)

	if total, avail, err := readMem(); err == nil {
		m.MemTotal = total
		m.MemUsed = total - avail
		if total > 0 {
			m.MemPercent = round1(float64(m.MemUsed) / float64(total) * 100)
		}
	}
	if total, used, err := readDisk("/"); err == nil {
		m.DiskTotal = total
		m.DiskUsed = used
		if total > 0 {
			m.DiskPercent = round1(float64(used) / float64(total) * 100)
		}
	}
	return m, nil
}

// cpuTimes 是一组累计 CPU 时间片（jiffies）。
type cpuTimes struct{ total, idle uint64 }

// readCPU 读取 /proc/stat 首行的累计 CPU 时间。
func readCPU() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTimes{}, sc.Err()
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, nil
	}
	var t cpuTimes
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		t.total += n
		if i == 3 || i == 4 { // idle + iowait 视为空闲
			t.idle += n
		}
	}
	return t, nil
}

// cpuPercent 由两次采样的差值算使用率。
func cpuPercent(a, b cpuTimes) float64 {
	dt := float64(b.total - a.total)
	if dt <= 0 {
		return 0
	}
	di := float64(b.idle - a.idle)
	return round1((dt - di) / dt * 100)
}

// readMem 读取 /proc/meminfo，返回总量与可用量（字节）。
func readMem() (total, avail int64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = meminfoVal(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = meminfoVal(line)
		}
		if total > 0 && avail > 0 {
			break
		}
	}
	return total, avail, sc.Err()
}

// meminfoVal 从 "MemTotal:  16384000 kB" 解析出字节数。
func meminfoVal(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	kb, _ := strconv.ParseInt(fields[1], 10, 64)
	return kb * 1024
}

// readDisk 用 statfs 读取挂载点总量与已用量（字节）。
func readDisk(path string) (total, used int64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := int64(st.Bsize)
	total = int64(st.Blocks) * bsize
	free := int64(st.Bavail) * bsize // 非特权用户可用空间
	return total, total - free, nil
}

// round1 保留一位小数。
func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}
