// Package serverstatus generate the server system status
package serverstatus

import (
	"fmt"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

// GetSystemInfo get the system info of a given periodic
func GetSystemInfo() (Cpu float64, Mem float64, Disk float64, Uptime int, err error) {
	cpuPercent, err := cpu.Percent(0, false)
	// Check if cpuPercent is empty
	if len(cpuPercent) > 0 {
		Cpu = cpuPercent[0]
	} else {
		Cpu = 0
	}

	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("get cpu usage failed: %s", err)
	}

	memUsage, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("get mem usage failed: %s", err)
	}

	diskUsage, err := disk.Usage("/")
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("get disk usage failed: %s", err)
	}

	// System uptime in seconds (not the wall-time of this function call).
	u, err := host.Uptime()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("get host uptime failed: %s", err)
	}
	Uptime = int(u)

	return Cpu, memUsage.UsedPercent, diskUsage.UsedPercent, Uptime, nil
}
