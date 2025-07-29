package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DockerStat struct {
	CPU float64
	Mem float64
}

var (
	dockerStats     []DockerStat
	dockerStatsLock sync.Mutex
)

func collectDockerStats(stopCh <-chan struct{}, interval time.Duration, containerName string, outputFile string) {
	f, _ := os.Create(outputFile)
	defer f.Close()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			cmd := exec.Command("docker", "stats", containerName, "--no-stream", "--format", "{{.CPUPerc}},{{.MemUsage}}")
			out, err := cmd.Output()
			if err != nil {
				continue
			}
			line := strings.TrimSpace(string(out))
			f.WriteString(line + "\n")
			parts := strings.Split(line, ",")
			if len(parts) != 2 {
				continue
			}
			cpuStr := strings.TrimSuffix(parts[0], "%")
			cpu, _ := strconv.ParseFloat(strings.TrimSpace(cpuStr), 64)

			memStr := strings.TrimSpace(parts[1])
			memParts := strings.Fields(memStr)
			if len(memParts) >= 1 {
				memValue := memParts[0]
				memNumStr := strings.TrimSuffix(memValue, "GiB")
				memNumStr = strings.TrimSuffix(memNumStr, "MiB")
				memNumStr = strings.TrimSuffix(memNumStr, "KiB")
				memNumStr = strings.TrimSuffix(memNumStr, "B")

				mem, _ := strconv.ParseFloat(memNumStr, 64)

				if strings.HasSuffix(memValue, "GiB") {
					mem *= 1024
				} else if strings.HasSuffix(memValue, "KiB") {
					mem /= 1024
				} else if strings.HasSuffix(memValue, "B") && !strings.HasSuffix(memValue, "iB") {
					mem /= (1024 * 1024)
				}

				dockerStatsLock.Lock()
				dockerStats = append(dockerStats, DockerStat{CPU: cpu, Mem: mem})
				dockerStatsLock.Unlock()
			}
		}
	}
}

func printDockerStatsSummary() {
	dockerStatsLock.Lock()
	defer dockerStatsLock.Unlock()
	if len(dockerStats) == 0 {
		fmt.Println("No docker stats collected.")
		return
	}
	minCPU, maxCPU, sumCPU := dockerStats[0].CPU, dockerStats[0].CPU, 0.0
	minMem, maxMem, sumMem := dockerStats[0].Mem, dockerStats[0].Mem, 0.0
	for _, stat := range dockerStats {
		if stat.CPU < minCPU {
			minCPU = stat.CPU
		}
		if stat.CPU > maxCPU {
			maxCPU = stat.CPU
		}
		sumCPU += stat.CPU
		if stat.Mem < minMem {
			minMem = stat.Mem
		}
		if stat.Mem > maxMem {
			maxMem = stat.Mem
		}
		sumMem += stat.Mem
	}
	avgCPU := sumCPU / float64(len(dockerStats))
	avgMem := sumMem / float64(len(dockerStats))
	fmt.Printf("\n--- Docker Stats for porch-test-control-plane ---\n")
	fmt.Printf("CPU Usage (%%): min=%.2f avg=%.2f max=%.2f\n", minCPU, avgCPU, maxCPU)
	fmt.Printf("Mem Usage (MiB): min=%.2f avg=%.2f max=%.2f\n", minMem, avgMem, maxMem)
}
