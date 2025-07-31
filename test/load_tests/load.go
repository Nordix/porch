package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	numUsers = 10
	rampUp   = 60 * time.Second
	duration = 600 * time.Second

	failedPkgs     []CreatedPackage
	failedPkgsLock sync.Mutex

	numUsersForChart int
	rampUpForChart   time.Duration
	durationForChart time.Duration
)

type LatencyDataPoint struct {
	Timestamp time.Time
	Latency   time.Duration
	Operation string
}

type Stats struct {
	sync.Mutex
	Success   int
	Fail      int
	Total     int
	Load      int
	Latencies []LatencyDataPoint
}

type CreatedPackage struct {
	Namespace string
	Name      string
}

func main() {
	stopDockerStats := make(chan struct{})
	go collectDockerStats(stopDockerStats, time.Second, "porch-test-control-plane", "docker_stats.csv")

	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil {
			numUsers = n
		}
	}
	if len(os.Args) > 2 {
		if s, err := strconv.Atoi(os.Args[2]); err == nil {
			rampUp = time.Duration(s) * time.Second
		}
	}
	if len(os.Args) > 3 {
		if s, err := strconv.Atoi(os.Args[3]); err == nil {
			duration = time.Duration(s) * time.Second
		}
	}

	numUsersForChart = numUsers
	rampUpForChart = rampUp
	durationForChart = duration

	var loadPercentage int
	if rampUp.Seconds() > 0 {
		loadPercentage = max(int(60.0*(float64(numUsers)/rampUp.Seconds())), 0)
	}
	fmt.Printf("Running with Users=%d, rampUp=%s, duration=%s\n", numUsers, rampUp, duration)
	fmt.Printf("Load:    %d%%\n", loadPercentage)

	var wg sync.WaitGroup
	stopCh := time.After(duration)
	userCh := make(chan int, numUsers)

	stats := &Stats{}
	stats.Load = loadPercentage

	createdPkgsCh := make(chan CreatedPackage, numUsers)
	var createdPkgs []CreatedPackage

	go func() {
		for i := range numUsers {
			userCh <- i
			time.Sleep(rampUp / time.Duration(numUsers))
		}
		close(userCh)
	}()

	runningTasks := 0
	allUsersDispatched := false

	for {
		select {
		case <-stopCh:
			fmt.Println("\nTest duration complete. Waiting for running tasks to finish...")
			wg.Wait()

			close(createdPkgsCh)
			for pkg := range createdPkgsCh {
				createdPkgs = append(createdPkgs, pkg)
			}

			fmt.Println("\n--- Starting cleanup packages phase ---")
			cleanupAllPackages(createdPkgs, stats)

			cleanup()
			printSummaryStats(stats)
			close(stopDockerStats)
			printDockerStatsSummary()
			return
		case userNum, ok := <-userCh:
			if !ok {
				allUsersDispatched = true
				if runningTasks == 0 {
					close(createdPkgsCh)
					for pkg := range createdPkgsCh {
						createdPkgs = append(createdPkgs, pkg)
					}

					fmt.Println("\n--- Starting cleanup packages phase ---")
					cleanupAllPackages(createdPkgs, stats)

					cleanup()
					printSummaryStats(stats)
					close(stopDockerStats)
					printDockerStatsSummary()
					return
				}
				continue
			}
			runningTasks++
			wg.Add(1)
			go func(n int) {
				defer func() {
					wg.Done()
					runningTasks--
				}()
				createdPkg := fullPackageLifecycle(n, stats)
				if createdPkg.Name != "" {
					createdPkgsCh <- createdPkg
				}
			}(userNum)
		}

		if allUsersDispatched && runningTasks == 0 {
			fmt.Println("\nAll tasks completed before test duration ended.")
			close(createdPkgsCh)
			for pkg := range createdPkgsCh {
				createdPkgs = append(createdPkgs, pkg)
			}
			fmt.Println("\n--- Starting cleanup packages phase ---")
			cleanupAllPackages(createdPkgs, stats)
			cleanup()
			printSummaryStats(stats)
			close(stopDockerStats)
			printDockerStatsSummary()
			return
		}
	}
}

func cleanupAllPackages(pkgs []CreatedPackage, stats *Stats) {
	fmt.Printf("Cleaning up %d packages...\n", len(pkgs))
	time.Sleep(5 * time.Second)
	for _, pkg := range pkgs {
		cleanupPackage(pkg, stats)
	}
	fmt.Println("Cleanup phase completed.")
}
