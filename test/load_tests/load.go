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
	duration = 60 * time.Second

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

			fmt.Println("\n--- Starting cleanup phase ---")
			cleanupAllPackages(createdPkgs, stats)

			cleanup()
			printSummaryStats(stats)
			return
		case userNum, ok := <-userCh:
			if !ok {
				allUsersDispatched = true
				if runningTasks == 0 {
					close(createdPkgsCh)
					for pkg := range createdPkgsCh {
						createdPkgs = append(createdPkgs, pkg)
					}

					fmt.Println("\n--- Starting cleanup phase ---")
					cleanupAllPackages(createdPkgs, stats)

					cleanup()
					printSummaryStats(stats)
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

			fmt.Println("\n--- Starting cleanup phase ---")
			cleanupAllPackages(createdPkgs, stats)

			cleanup()
			printSummaryStats(stats)
			return
		}
	}
}

func cleanupAllPackages(pkgs []CreatedPackage, stats *Stats) {
	fmt.Printf("Cleaning up %d packages...\n", len(pkgs))

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) 

	for _, pkg := range pkgs {
		wg.Add(1)
		go func(pkg CreatedPackage) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			cleanupPackage(pkg, stats)
		}(pkg)
	}

	wg.Wait()
	fmt.Println("Cleanup phase completed.")
}
