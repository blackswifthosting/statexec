package collectors

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/cpu"
)

type CpuMetrics struct {
	Cpu            string
	CpuTimePerMode map[string]float64
}

// Get CPU time by state
func getCpuTimeByMode(cpuTimeStat *cpu.TimesStat, mode string) float64 {
	switch mode {
	case "user":
		return cpuTimeStat.User
	case "system":
		return cpuTimeStat.System
	case "idle":
		return cpuTimeStat.Idle
	case "nice":
		return cpuTimeStat.Nice
	case "iowait":
		return cpuTimeStat.Iowait
	case "irq":
		return cpuTimeStat.Irq
	case "softirq":
		return cpuTimeStat.Softirq
	case "steal":
		return cpuTimeStat.Steal
	case "guest":
		return cpuTimeStat.Guest
	case "guestNice":
		return cpuTimeStat.GuestNice
	default:
		return 0
	}
}

func CollectCpuMetrics() []CpuMetrics {
	var cpuMetrics []CpuMetrics
	cpuTimeStat, err := cpu.Times(true)
	if err != nil {
		fmt.Println("Error retrieving CPU Times:", err)
		panic(err)
	}

	// CpuFreqStat, _ := cpu.Info()
	// fmt.Println("cpuFreq object is %v", CpuFreqStat)

	for _, cpuTime := range cpuTimeStat {
		cpuTimePerMode := make(map[string]float64)
		modes := []string{"user", "system", "idle", "nice", "iowait", "irq", "softirq", "steal", "guest", "guestNice"}
		for _, mode := range modes {
			cpuTimePerMode[mode] = getCpuTimeByMode(&cpuTime, mode)
		}

		cpuMetrics = append(cpuMetrics, CpuMetrics{Cpu: cpuTime.CPU, CpuTimePerMode: cpuTimePerMode})
	}
	return cpuMetrics
}
