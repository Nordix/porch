package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

var (
	namespace = "porch-demo"
	repo      = "porch-test"
	workspace = "1"
)

type CommandResult struct {
	Success  bool
	Output   string
	Error    error
	Duration time.Duration
	Command  string
}

func runPorchctlCommand(ctx context.Context, args ...string) CommandResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "porchctl", args...)
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	result := CommandResult{
		Success:  err == nil,
		Output:   string(output),
		Error:    err,
		Duration: duration,
		Command:  "porchctl " + strings.Join(args, " "),
	}

	// if err != nil {
	// 	fmt.Printf("Command failed: %s\nError: %v\nOutput: %s\n", result.Command, err, output)
	// }

	return result
}

func editSampleFile(dir string) error {
	srcFile := "deployment.yaml"
	dstFile := fmt.Sprintf("%s/deployment.yaml", dir)

	src, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("failed to read deployment.yaml: %w", err)
	}

	err = os.WriteFile(dstFile, src, 0644)
	if err != nil {
		return fmt.Errorf("failed to write deployment.yaml to %s: %w", dir, err)
	}

	file := fmt.Sprintf("%s/Kptfile", dir)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open Kptfile: %w", err)
	}
	defer f.Close()

	pipelineYAML := `
pipeline:
  mutators:
    - image: gcr.io/kpt-fn/set-namespace:v0.4.1
      configMap:
        namespace: example-namespace
      selectors:
        - kind: Deployment
    - image: gcr.io/kpt-fn/search-replace:v0.2.0
      configMap:
        by-path: spec.replicas
        put-value: "4"
      selectors:
        - kind: Deployment
`
	_, err = f.WriteString(pipelineYAML)
	if err != nil {
		return fmt.Errorf("failed to write pipeline YAML: %w", err)
	}
	return nil
}

type PackageLifecycleStep struct {
	Description string
	Command     []string
	IsEdit      bool
}

func fullPackageLifecycle(n int, stats *Stats) CreatedPackage {
	pkgName := fmt.Sprintf("sample-package-%d", n)
	pkgRev := fmt.Sprintf("%s.%s.%s", repo, pkgName, workspace)
	tmpDir := fmt.Sprintf("./tmp/%s", pkgName)

	if err := os.MkdirAll("./tmp/", 0755); err != nil {
		fmt.Printf("[FAIL] (mkdir tmpdir): %v\n", err)
		return CreatedPackage{}
	}

	steps := []PackageLifecycleStep{
		{Description: "init", Command: []string{"rpkg", "init", pkgName, "--namespace=" + namespace, "--repository=" + repo, "--workspace=" + workspace}},
		{Description: "pull", Command: []string{"rpkg", "pull", pkgRev, "--namespace=" + namespace, tmpDir}},
		{Description: "edit", IsEdit: true},
		{Description: "push", Command: []string{"rpkg", "push", pkgRev, "--namespace=" + namespace, tmpDir}},
		{Description: "propose", Command: []string{"rpkg", "propose", pkgRev, "--namespace=" + namespace}},
		{Description: "approve", Command: []string{"rpkg", "approve", pkgRev, "--namespace=" + namespace}},
	}

	ctx := context.Background()
	for _, step := range steps {
		var err error
		var result CommandResult
		timestamp := time.Now()

		if step.IsEdit {
			err = editSampleFile(tmpDir)
			result = CommandResult{
				Success:  err == nil,
				Error:    err,
				Duration: 0,
				Command:  "edit " + tmpDir,
			}
		} else {
			result = runPorchctlCommand(ctx, step.Command...)
			err = result.Error
		}

		stats.Lock()
		stats.Total++
		stats.Latencies = append(stats.Latencies, LatencyDataPoint{
			Timestamp: timestamp,
			Latency:   result.Duration,
			Operation: step.Description,
			Error:     err,
		})
		if err == nil {
			stats.Success++
			fmt.Printf("[OK] %-20s (%-7s)(%.2fs)\n", pkgName, step.Description, result.Duration.Seconds())
		} else {
			stats.Fail++
			fmt.Printf("[FAIL] %-20s (%-7s) (%.2fs)\n", pkgName, step.Description, result.Duration.Seconds())
			stats.Unlock()
			failedPkgsLock.Lock()
			failedPkgs = append(failedPkgs, CreatedPackage{Namespace: namespace, Name: pkgName})
			failedPkgsLock.Unlock()
			return CreatedPackage{}
		}
		stats.Unlock()
	}
	return CreatedPackage{Namespace: namespace, Name: pkgName}
}

type CleanupStep struct {
	Description string
	Command     []string
}

func cleanupPackage(pkg CreatedPackage, stats *Stats) {
	pkgName := pkg.Name
	pkgRev := fmt.Sprintf("%s.%s.%s", repo, pkgName, workspace)
	pkgRevMain := fmt.Sprintf("%s.%s.main", repo, pkgName)

	cleanupSteps := []CleanupStep{
		{Description: "propose-delete", Command: []string{"rpkg", "propose-delete", pkgRev, "--namespace=" + namespace}},
		{Description: "propose-delete-main", Command: []string{"rpkg", "propose-delete", pkgRevMain, "--namespace=" + namespace}},
		{Description: "delete", Command: []string{"rpkg", "delete", pkgRev, "--namespace=" + namespace}},
		{Description: "delete-main", Command: []string{"rpkg", "delete", pkgRevMain, "--namespace=" + namespace}},
	}

	ctx := context.Background()
	for _, step := range cleanupSteps {
		start := time.Now()
		result := runPorchctlCommand(ctx, step.Command...)
		elapsed := time.Since(start)
		timestamp := time.Now()

		stats.Lock()
		stats.Total++
		stats.Latencies = append(stats.Latencies, LatencyDataPoint{
			Timestamp: timestamp,
			Latency:   elapsed,
			Operation: step.Description,
			Error:     result.Error,
		})
		if result.Error == nil {
			stats.Success++
			fmt.Printf("[OK] %-20s (%-19s)(%.2fs)\n", pkgName, step.Description, elapsed.Seconds())
		} else {
			stats.Fail++
			fmt.Printf("[FAIL] %-20s (%-19s) (%.2fs)\n", pkgName, step.Description, elapsed.Seconds())
		}
		stats.Unlock()
	}
}

func isPackageReady(pkg CreatedPackage) bool {
	pkgName := pkg.Name
	pkgRev := fmt.Sprintf("%s.%s.main", repo, pkgName)

	ctx := context.Background()
	result := runPorchctlCommand(ctx, "rpkg", "get", pkgRev, "--namespace="+namespace, "-o", "jsonpath='{.spec.lifecycle}'")
	if result.Error != nil {
		return false
	}

	if strings.Contains(result.Output, "Published") {
		return true
	} else {
		return false
	}
}

func cleanupAllPackages(pkgs []CreatedPackage, stats *Stats) {
	fmt.Printf("Waiting for packages to be ready\n")
	startTime := time.Now()
	const maxRetries = 10

	for i := range pkgs {
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if isPackageReady(pkgs[i]) {
				break
			}
			if attempt == maxRetries {
				fmt.Printf("Package %v failed to become ready after %d attempt\n", pkgs[i], maxRetries)
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
	fmt.Printf("All packages are ready in (%.2fs) seconds\n", time.Since(startTime).Seconds())
	fmt.Printf("Cleaning up %d packages...\n", len(pkgs))
	for _, pkg := range pkgs {
		cleanupPackage(pkg, stats)
	}
	fmt.Println("Cleanup phase completed.")
}

func calculateMetrics(stats *Stats) LoadTestMetrics {
	stats.Lock()
	defer stats.Unlock()

	if len(stats.Latencies) == 0 {
		return LoadTestMetrics{}
	}

	var totalLatency time.Duration
	minLatency := stats.Latencies[0].Latency
	maxLatency := stats.Latencies[0].Latency

	for _, latency := range stats.Latencies {
		totalLatency += latency.Latency
		if latency.Latency < minLatency {
			minLatency = latency.Latency
		}
		if latency.Latency > maxLatency {
			maxLatency = latency.Latency
		}
	}

	avgLatency := totalLatency / time.Duration(len(stats.Latencies))

	var throughput float64
	if stats.EndTime.Sub(stats.StartTime) > 0 {
		throughput = float64(stats.Total) / stats.EndTime.Sub(stats.StartTime).Seconds()
	}

	errorRate := 0.0
	if stats.Total > 0 {
		errorRate = float64(stats.Fail) / float64(stats.Total) * 100
	}

	return LoadTestMetrics{
		TotalRequests:      stats.Total,
		SuccessfulRequests: stats.Success,
		FailedRequests:     stats.Fail,
		AverageLatency:     avgLatency,
		MinLatency:         minLatency,
		MaxLatency:         maxLatency,
		Throughput:         throughput,
		ErrorRate:          errorRate,
	}
}

func printSummaryStats(stats *Stats) {
	metrics := calculateMetrics(stats)

	fmt.Printf("\n=== Load Test Summary ===\n")
	fmt.Printf("Total Requests:     %d\n", metrics.TotalRequests)
	fmt.Printf("Successful:         %d\n", metrics.SuccessfulRequests)
	fmt.Printf("Failed:             %d\n", metrics.FailedRequests)
	fmt.Printf("Error Rate:         %.2f%%\n", metrics.ErrorRate)
	fmt.Printf("Load:               %d%%\n", stats.Load)
	fmt.Printf("Test Duration:      %s\n", stats.EndTime.Sub(stats.StartTime))
	fmt.Printf("Throughput:         %.2f req/s\n", metrics.Throughput)

	if len(stats.Latencies) > 0 {
		fmt.Printf("\n=== Overall Latency Statistics ===\n")
		fmt.Printf("Min:\t%.2fs\n", metrics.MinLatency.Seconds())
		fmt.Printf("Avg:\t%.2fs\n", metrics.AverageLatency.Seconds())
		fmt.Printf("Max:\t%.2fs\n", metrics.MaxLatency.Seconds())

		operationStats := make(map[string][]time.Duration)
		operationErrors := make(map[string]int)

		for _, ldp := range stats.Latencies {
			operationStats[ldp.Operation] = append(operationStats[ldp.Operation], ldp.Latency)
			if ldp.Error != nil {
				operationErrors[ldp.Operation]++
			}
		}

		fmt.Printf("\n=== Per-Operation Statistics ===\n")
		fmt.Printf("%-13s %8s %8s %8s %8s %8s %8s\n", "Operation", "Count", "Errors", "Min(s)", "Avg(s)", "Max(s)", "StdDev(s)")
		fmt.Printf("%s\n", strings.Repeat("-", 70))

		operations := make([]string, 0, len(operationStats))
		for op := range operationStats {
			operations = append(operations, op)
		}
		slices.Sort(operations)

		for _, op := range operations {
			durations := operationStats[op]
			count := len(durations)
			errors := operationErrors[op]
			min := slices.Min(durations)
			max := slices.Max(durations)

			var sum time.Duration
			for _, d := range durations {
				sum += d
			}
			avg := sum / time.Duration(count)

			var varianceSum float64
			avgSeconds := avg.Seconds()
			for _, d := range durations {
				diff := d.Seconds() - avgSeconds
				varianceSum += diff * diff
			}
			stdDev := math.Sqrt(varianceSum / float64(count))

			displayName := op
			switch op {
			case "propose-delete":
				displayName = "prop-del"
			case "propose-delete-main":
				displayName = "prop-del-main"
			case "delete-main":
				displayName = "del-main"
			}

			fmt.Printf("%-13s %8d %8d %8.2f %8.2f %8.2f %8.2f\n",
				displayName, count, errors, min.Seconds(), avg.Seconds(), max.Seconds(), stdDev)
		}

		if metrics.FailedRequests > 0 {
			fmt.Printf("\n=== Error Details ===\n")
			errorTypes := make(map[string]int)
			for _, ldp := range stats.Latencies {
				if ldp.Error != nil {
					errorTypes[ldp.Error.Error()]++
				}
			}

			for errMsg, count := range errorTypes {
				fmt.Printf("  %s: %d occurrences\n", errMsg, count)
			}
		}
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Chart generation failed: %v\n", r)
			}
		}()
		createLineChart(stats)
	}()

}

func forceCleanupFailedPackages(pkgs []CreatedPackage) []CreatedPackage {
	var failed []CreatedPackage
	for _, pkg := range pkgs {
		failedAny := false
		for _, rev := range []string{
			fmt.Sprintf("porch-test.%s.1", pkg.Name),
			fmt.Sprintf("porch-test.%s.main", pkg.Name),
		} {
			cmd := exec.Command("porchctl", "rpkg", "propose-delete", rev, "--namespace="+pkg.Namespace)
			cmd.Stdout = nil
			cmd.Stderr = nil
			if err := cmd.Run(); err != nil {
				failedAny = true
			}
			cmd = exec.Command("porchctl", "rpkg", "delete", rev, "--namespace="+pkg.Namespace)
			cmd.Stdout = nil
			cmd.Stderr = nil
			if err := cmd.Run(); err != nil {
				failedAny = true
			}
		}
		if failedAny {
			failed = append(failed, pkg)
		}

	}
	return failed
}

func cleanup() {
	fmt.Printf("\n--- Cleaning up ---\n")
	failedPkgsLock.Lock()
	pkgsToCleanup := make([]CreatedPackage, len(failedPkgs))
	copy(pkgsToCleanup, failedPkgs)
	failedPkgsLock.Unlock()
	fmt.Printf("Packages to clean up: %d\n", len(pkgsToCleanup))
	failedPkgs := forceCleanupFailedPackages(pkgsToCleanup)
	if len(failedPkgs) > 0 {
		fmt.Println("Failed to delete the following packages:")
		for _, pkg := range failedPkgs {
			fmt.Printf("Namespace: %s, Name: %s\n", pkg.Namespace, pkg.Name)
		}
	}
	if err := os.RemoveAll("./tmp/"); err != nil {
		fmt.Printf("Failed to delete tmp directory: %v\n", err)
	}
}
