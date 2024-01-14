package collectors

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/disk"
)

type DiskMetrics struct {
	Device          string
	ReadBytesTotal  uint64
	WriteBytesTotal uint64
}

func CollectDiskMetrics() []DiskMetrics {
	var diskMetrics []DiskMetrics
	diskStat, err := disk.IOCounters()
	if err != nil {
		fmt.Println("Error retrieving Disk IO Counters:", err)
		panic(err)
	}

	for device, diskIO := range diskStat {
		diskMetrics = append(diskMetrics, DiskMetrics{Device: device, ReadBytesTotal: diskIO.ReadBytes, WriteBytesTotal: diskIO.WriteBytes})
	}

	return diskMetrics
}
