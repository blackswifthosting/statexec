package collectors

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/mem"
)

type MemoryMetrics struct {
	Total       uint64
	Available   uint64
	Used        uint64
	Free        uint64
	Buffers     uint64
	Cached      uint64
	UsedPercent float64
}

func CollectMemoryMetrics() MemoryMetrics {
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		fmt.Println("Error retrieving Virtual Memory Usage:", err)
		panic(err)
	}

	return MemoryMetrics{
		Total:       vmStat.Total,
		Available:   vmStat.Available,
		Used:        vmStat.Used,
		Free:        vmStat.Free,
		Buffers:     vmStat.Buffers,
		Cached:      vmStat.Cached,
		UsedPercent: vmStat.UsedPercent,
	}
}
