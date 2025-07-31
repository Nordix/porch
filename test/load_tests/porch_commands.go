package main

import (
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

func runPorchctlCommand(args ...string) error {
	cmd := exec.Command("porchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error: %v, Output: %s\n", err, output)
	}
	return err
}

func editSampleFile(dir string) error {
	srcFile := "deployment.yaml"
	dstFile := fmt.Sprintf("%s/deployment.yaml", dir)

	src, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("failed to read deployment.yaml: %v", err)
	}

	err = os.WriteFile(dstFile, src, 0644)
	if err != nil {
		return fmt.Errorf("failed to write deployment.yaml to %s: %v", dir, err)
	}

	file := fmt.Sprintf("%s/Kptfile", dir)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
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
	return err
}

func fullPackageLifecycle(n int, stats *Stats) CreatedPackage {
	pkgName := fmt.Sprintf("sample-package-%d", n)
	pkgRev := fmt.Sprintf("%s.%s.%s", repo, pkgName, workspace)
	tmpDir := fmt.Sprintf("./tmp/%s", pkgName)

	if err := os.MkdirAll("./tmp/", 0755); err != nil {
		fmt.Printf("[FAIL] (mkdir tmpdir): %v\n", err)
		return CreatedPackage{}
	}

	steps := []struct {
		desc string
		cmd  []string
		edit bool
	}{
		{"init", []string{"rpkg", "init", pkgName, "--namespace=" + namespace, "--repository=" + repo, "--workspace=" + workspace}, false},
		{"pull", []string{"rpkg", "pull", pkgRev, "--namespace=" + namespace, tmpDir}, false},
		{"edit", nil, true},
		{"push", []string{"rpkg", "push", pkgRev, "--namespace=" + namespace, tmpDir}, false},
		{"propose", []string{"rpkg", "propose", pkgRev, "--namespace=" + namespace}, false},
		{"approve", []string{"rpkg", "approve", pkgRev, "--namespace=" + namespace}, false},
	}

	for _, step := range steps {
		start := time.Now()
		var err error
		if step.edit {
			err = editSampleFile(tmpDir)
		} else {
			err = runPorchctlCommand(step.cmd...)
		}
		elapsed := time.Since(start)
		timestamp := time.Now()

		stats.Lock()
		stats.Total++
		stats.Latencies = append(stats.Latencies, LatencyDataPoint{Timestamp: timestamp, Latency: elapsed, Operation: step.desc})
		if err == nil {
			stats.Success++
			fmt.Printf("[OK] %s (%s) (%.2fs)\n", pkgName, step.desc, elapsed.Seconds())
		} else {
			stats.Fail++
			fmt.Printf("[FAIL] %s (%s) (%.2fs)\n", pkgName, step.desc, elapsed.Seconds())
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

func cleanupPackage(pkg CreatedPackage, stats *Stats) {
	pkgName := pkg.Name
	pkgRev := fmt.Sprintf("%s.%s.%s", repo, pkgName, workspace)
	pkgRevMain := fmt.Sprintf("%s.%s.main", repo, pkgName)

	cleanupSteps := []struct {
		desc string
		cmd  []string
	}{
		{"propose-delete", []string{"rpkg", "propose-delete", pkgRev, "--namespace=" + namespace}},
		{"propose-delete-main", []string{"rpkg", "propose-delete", pkgRevMain, "--namespace=" + namespace}},
		{"delete", []string{"rpkg", "delete", pkgRev, "--namespace=" + namespace}},
		{"delete-main", []string{"rpkg", "delete", pkgRevMain, "--namespace=" + namespace}},
	}

	for _, step := range cleanupSteps {
		start := time.Now()
		err := runPorchctlCommand(step.cmd...)
		elapsed := time.Since(start)
		timestamp := time.Now()
		stats.Lock()
		stats.Total++
		stats.Latencies = append(stats.Latencies, LatencyDataPoint{Timestamp: timestamp, Latency: elapsed, Operation: step.desc})
		if err == nil {
			stats.Success++
			fmt.Printf("[OK] %s (%s) (%.2fs)\n", pkgName, step.desc, elapsed.Seconds())
		} else {
			stats.Fail++
			fmt.Printf("[FAIL] %s (%s) (%.2fs)\n", pkgName, step.desc, elapsed.Seconds())
		}
		stats.Unlock()
	}
}

func printSummaryStats(stats *Stats) {
	stats.Lock()
	defer stats.Unlock()

	fmt.Printf("\n--- Load Test Summary ---\nTotal:   %d\nSuccess: %d\nFail:    %d\nLoad:    %d%%\n",
		stats.Total, stats.Success, stats.Fail, stats.Load)

	if len(stats.Latencies) > 0 {
		durations := make([]time.Duration, len(stats.Latencies))
		for i, ldp := range stats.Latencies {
			durations[i] = ldp.Latency
		}
		min, max := slices.Min(durations), slices.Max(durations)
		var sum time.Duration
		for _, d := range durations {
			sum += d
		}
		avg := sum / time.Duration(len(durations))
		fmt.Printf("Overall Latency (s): min=%.2f avg=%.2f max=%.2f\n", min.Seconds(), avg.Seconds(), max.Seconds())

		operationStats := make(map[string][]time.Duration)
		for _, ldp := range stats.Latencies {
			operationStats[ldp.Operation] = append(operationStats[ldp.Operation], ldp.Latency)
		}

		fmt.Printf("\n--- Per-Operation Statistics ---\n")
		fmt.Printf("%-15s %8s %8s %8s %8s %8s\n", "Operation", "Count", "Min(s)", "Avg(s)", "Max(s)", "StdDev(s)")
		fmt.Printf("%s\n", strings.Repeat("-", 70))

		operations := make([]string, 0, len(operationStats))
		for op := range operationStats {
			operations = append(operations, op)
		}
		slices.Sort(operations)

		for _, op := range operations {
			durations := operationStats[op]
			count := len(durations)
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

			fmt.Printf("%-15s %8d %8.2f %8.2f %8.2f %8.2f\n",
				displayName, count, min.Seconds(), avg.Seconds(), max.Seconds(), stdDev)
		}
	}

	createLineChart(stats)
	fmt.Println("Line chart created: latency_chart.html")
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
