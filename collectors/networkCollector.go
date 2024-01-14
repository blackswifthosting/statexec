package collectors

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/net"
)

type NetworkMetrics struct {
	Interface      string
	SentTotalBytes uint64
	RecvTotalBytes uint64
}

func CollectNetworkMetrics() []NetworkMetrics {
	var networkMetrics []NetworkMetrics
	netStat, err := net.IOCounters(true)
	if err != nil {
		fmt.Println("Error retrieving Network IO Counters:", err)
		panic(err)
	}

	for _, netIO := range netStat {
		networkMetrics = append(networkMetrics, NetworkMetrics{Interface: netIO.Name, SentTotalBytes: netIO.BytesSent, RecvTotalBytes: netIO.BytesRecv})
	}

	return networkMetrics
}
