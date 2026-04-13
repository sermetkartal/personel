//go:build windows

package main

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// numCPU is captured once at startup. GetProcessTimes returns aggregate
// kernel+user time across every logical core, so a process pegging ONE
// core on an 8-core machine yields a 100% / 8 = 12.5% reading. We
// divide by numCPU to normalise to whole-machine percent, which is what
// the throttle subsystem enforces and what the Phase 1 exit criterion
// ("CPU < 2%") means. runtime.NumCPU on Windows calls GetSystemInfo
// internally so the result matches Task Manager's "Logical processors".
var numCPU = func() int {
	n := runtime.NumCPU()
	if n <= 0 {
		return 1
	}
	return n
}()

// processMemoryCountersEx mirrors the Win32 PROCESS_MEMORY_COUNTERS_EX
// struct. Defined here because golang.org/x/sys/windows does not expose
// GetProcessMemoryInfo as a typed helper.
type processMemoryCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

var (
	psapiDLL                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = psapiDLL.NewProc("GetProcessMemoryInfo")
)

// Sample is a single point-in-time measurement of the child agent.
type Sample struct {
	Timestamp  time.Time `json:"timestamp"`
	CPUPercent float64   `json:"cpu_percent"` // whole-machine CPU% across all cores
	RSSBytes   uint64    `json:"rss_bytes"`
	PrivateKB  uint64    `json:"private_bytes"`
}

// Sampler holds the process handle plus the previous kernel/user/wall
// filetimes so consecutive Sample calls can compute a CPU delta.
type Sampler struct {
	handle       windows.Handle
	prevKernel   uint64
	prevUser     uint64
	prevWall     time.Time
	initialised  bool
}

// NewSampler binds a handle (caller keeps ownership — Sampler does not
// close it).
func NewSampler(h windows.Handle) *Sampler {
	return &Sampler{handle: h}
}

// Sample reads the current CPU times + memory counters. The first call
// after construction returns CPUPercent=0 because there is no delta;
// callers should discard the priming sample.
func (s *Sampler) Sample() (Sample, error) {
	now := time.Now()

	// CPU times.
	var createTime, exitTime, kernelTime, userTime windows.Filetime
	if err := windows.GetProcessTimes(s.handle, &createTime, &exitTime, &kernelTime, &userTime); err != nil {
		return Sample{}, fmt.Errorf("GetProcessTimes: %w", err)
	}
	kernel := filetimeToUint64(kernelTime)
	user := filetimeToUint64(userTime)

	var cpuPct float64
	if s.initialised {
		wallNS := now.Sub(s.prevWall).Nanoseconds()
		if wallNS > 0 {
			// filetime units are 100-ns ticks → convert to ns by *100.
			cpuNS := int64((kernel - s.prevKernel + user - s.prevUser) * 100)
			// Normalise across all logical CPUs so 100% = every core busy.
			cpuPct = (float64(cpuNS) / float64(wallNS)) * 100.0 / float64(numCPU)
			if cpuPct < 0 {
				cpuPct = 0
			}
		}
	}
	s.prevKernel = kernel
	s.prevUser = user
	s.prevWall = now
	s.initialised = true

	// Memory counters.
	var mem processMemoryCountersEx
	mem.CB = uint32(unsafe.Sizeof(mem))
	r1, _, callErr := procGetProcessMemoryInfo.Call(
		uintptr(s.handle),
		uintptr(unsafe.Pointer(&mem)),
		uintptr(mem.CB),
	)
	if r1 == 0 {
		return Sample{}, fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}

	return Sample{
		Timestamp:  now,
		CPUPercent: cpuPct,
		RSSBytes:   uint64(mem.WorkingSetSize),
		PrivateKB:  uint64(mem.PrivateUsage),
	}, nil
}

// filetimeToUint64 packs a Windows FILETIME into a single uint64 tick count.
func filetimeToUint64(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}
