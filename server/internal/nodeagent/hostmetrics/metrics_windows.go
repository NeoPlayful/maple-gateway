//go:build windows

package hostmetrics

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// kernel32 入口：直接经 LazySystemDLL 调用，避免依赖 x/sys 的封装是否齐全。
var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes    = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStat  = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceW = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// memoryStatusEx 对应 Win32 MEMORYSTATUSEX。
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// Collect 采样 Windows 主机资源使用率（GetSystemTimes / GlobalMemoryStatusEx / GetDiskFreeSpaceExW）。
func Collect() (Metrics, error) {
	first, err := readCPU()
	if err != nil {
		return Metrics{}, err
	}
	// 两次采样求差值，得到瞬时 CPU 使用率。
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
	if total, used, err := readDisk(systemDrive()); err == nil {
		m.DiskTotal = total
		m.DiskUsed = used
		if total > 0 {
			m.DiskPercent = round1(float64(used) / float64(total) * 100)
		}
	}
	return m, nil
}

// cpuTimes 是一组累计 CPU 时间（100ns 单位）。
type cpuTimes struct{ total, idle uint64 }

// readCPU 读取系统累计 CPU 时间（idle/kernel/user）。
// Windows 的 kernel 时间包含 idle，故忙时 = (kernel - idle) + user，总量 = kernel + user。
func readCPU() (cpuTimes, error) {
	var idle, kernel, user windows.Filetime
	r, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return cpuTimes{}, err
	}
	return cpuTimes{
		total: filetimeToUint64(kernel) + filetimeToUint64(user),
		idle:  filetimeToUint64(idle),
	}, nil
}

// cpuPercent 由两次采样的差值算使用率（total 含 idle）。
func cpuPercent(a, b cpuTimes) float64 {
	dt := float64(b.total - a.total)
	if dt <= 0 {
		return 0
	}
	di := float64(b.idle - a.idle)
	return round1((dt - di) / dt * 100)
}

// readMem 用 GlobalMemoryStatusEx 返回物理内存总量与可用量（字节）。
func readMem() (total, avail int64, err error) {
	var ms memoryStatusEx
	ms.length = uint32(unsafe.Sizeof(ms))
	r, _, callErr := procGlobalMemoryStat.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return 0, 0, callErr
	}
	return int64(ms.totalPhys), int64(ms.availPhys), nil
}

// readDisk 用 GetDiskFreeSpaceExW 读取指定驱动器总量与已用量（字节）。
func readDisk(path string) (total, used int64, err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeAvail, totalBytes, totalFree uint64
	r, _, callErr := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, 0, callErr
	}
	return int64(totalBytes), int64(totalBytes - totalFree), nil
}

// systemDrive 返回系统盘根路径（如 "C:\\"）；取不到则回退 C 盘。
func systemDrive() string {
	if d := os.Getenv("SystemDrive"); d != "" {
		return d + "\\"
	}
	return "C:\\"
}

// filetimeToUint64 把 Filetime 组合为 64 位计数。
func filetimeToUint64(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// round1 保留一位小数。
func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}
