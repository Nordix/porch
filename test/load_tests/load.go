package main

import (
	"context"
	"fmt"
	"log"
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
	Error     error
	UserID    int
}

type Stats struct {
	sync.Mutex
	Success   int
	Fail      int
	Total     int
	Load      int
	Latencies []LatencyDataPoint
	StartTime time.Time
	EndTime   time.Time
}

type CreatedPackage struct {
	Namespace string
	Name      string
}

type LoadTestMetrics struct {
	TotalRequests    int
	SuccessfulRequests int
	FailedRequests   int
	AverageLatency   time.Duration
	MinLatency       time.Duration
	MaxLatency       time.Duration
	Throughput       float64
	ErrorRate        float64
}

func parseArgs() (*LoadTestConfig, error) {
	config := &LoadTestConfig{
		NumUsers: numUsers,
		RampUp:   rampUp,
		Duration: duration,
	}

	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err != nil {
			return nil, fmt.Errorf("invalid number of users: %w", err)
		} else {
			config.NumUsers = n
		}
	}
	if len(os.Args) > 2 {
		if s, err := strconv.Atoi(os.Args[2]); err != nil {
			return nil, fmt.Errorf("invalid ramp-up time: %w", err)
		} else {
			config.RampUp = time.Duration(s) * time.Second
		}
	}
	if len(os.Args) > 3 {
		if s, err := strconv.Atoi(os.Args[3]); err != nil {
			return nil, fmt.Errorf("invalid duration: %w", err)
		} else {
			config.Duration = time.Duration(s) * time.Second
		}
	}

	return config, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting load test...")

	config, err := parseArgs()
	if err != nil {
		log.Fatalf("Error parsing arguments: %v", err)
	}

	numUsersForChart = config.NumUsers
	rampUpForChart = config.RampUp
	durationForChart = config.Duration

	ctx, cancel := context.WithTimeout(context.Background(), config.Duration)
	defer cancel()

	stopDockerStats := make(chan struct{})
	go collectDockerStats(stopDockerStats, time.Second, "porch-test-control-plane", "docker_stats.csv")

	var loadPercentage int
	if config.RampUp.Seconds() > 0 {
		loadPercentage = max(int(60.0*(float64(config.NumUsers)/config.RampUp.Seconds())), 0)
	}
	
	log.Printf("Configuration: Users=%d, RampUp=%s, Duration=%s, Load=%d%%", 
		config.NumUsers, config.RampUp, config.Duration, loadPercentage)

	var wg sync.WaitGroup
	userCh := make(chan int, config.NumUsers)

	stats := &Stats{
		StartTime: time.Now(),
		Load:      loadPercentage,
	}

	createdPkgsCh := make(chan CreatedPackage, config.NumUsers)
	var createdPkgs []CreatedPackage

	go func() {
		defer close(userCh)
		log.Printf("Starting user dispatch with %d users over %s", config.NumUsers, config.RampUp)
		for i := range config.NumUsers {
			select {
			case userCh <- i:
				time.Sleep(config.RampUp / time.Duration(config.NumUsers))
			case <-ctx.Done():
				log.Println("User dispatch cancelled due to context timeout")
				return
			}
		}
	}()

	runningTasks := 0
	allUsersDispatched := false

	for {
		select {
		case <-ctx.Done():
			log.Println("Test duration complete. Waiting for running tasks to finish...")
			wg.Wait()

			close(createdPkgsCh)
			for pkg := range createdPkgsCh {
				createdPkgs = append(createdPkgs, pkg)
			}

			log.Println("Starting cleanup packages phase...")
			cleanupAllPackages(createdPkgs, stats)

			cleanup()
			stats.EndTime = time.Now()
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

					log.Println("Starting cleanup packages phase...")
					cleanupAllPackages(createdPkgs, stats)
					cleanup()
					stats.EndTime = time.Now()
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
					select {
					case createdPkgsCh <- createdPkg:
					case <-ctx.Done():
					}
				}
			}(userNum)
		}

		if allUsersDispatched && runningTasks == 0 {
			log.Println("All tasks completed before test duration ended.")
			close(createdPkgsCh)
			for pkg := range createdPkgsCh {
				createdPkgs = append(createdPkgs, pkg)
			}
			log.Println("Starting cleanup packages phase...")
			cleanupAllPackages(createdPkgs, stats)
			cleanup()
			stats.EndTime = time.Now()
			printSummaryStats(stats)
			close(stopDockerStats)
			printDockerStatsSummary()
			return
		}
	}
}
