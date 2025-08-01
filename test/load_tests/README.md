# Porch Load Test

A comprehensive load testing tool for Porch (Package Orchestration for Kubernetes) that measures performance and reliability of package lifecycle operations.

## Features

- **Concurrent User Simulation**: Simulates multiple users performing package operations simultaneously
- **Configurable Load Patterns**: Supports ramp-up periods and custom test durations
- **Real-time Metrics**: Tracks latency, throughput, and error rates
- **Visual Analytics**: Generates interactive charts showing performance over time
- **Docker Monitoring**: Collects Docker container statistics during tests
- **Structured Logging**: Comprehensive logging with different log levels
- **Configuration Management**: YAML/JSON configuration files for easy customization

## Quick Start

### Basic Usage

```bash
# Run with default settings (10 users, 60s ramp-up, 600s duration)
go run .

# Run with custom parameters
go run . 5 30 300  # 5 users, 30s ramp-up, 300s duration
```

### Using Configuration File

```bash
# Create a default configuration
go run . --config config.yaml

# Run with custom config file
go run . --config my-config.yaml
```

## Output

The tool generates several output files:

- `latency_chart.html`: Interactive chart showing latency over time by operation
- `docker_stats.csv`: Docker container statistics during the test
- Console output with detailed statistics

### Sample Output

```
=== Load Test Summary ===
Total Requests:     50
Successful:         50
Failed:             0
Error Rate:         0.00%
Load:               300%
Test Duration:      7.627600275s
Throughput:         6.56 req/s

=== Overall Latency Statistics ===
Min:    0.00s
Avg:    0.09s
Max:    0.14s

=== Per-Operation Statistics ===
Operation        Count   Errors   Min(s)   Avg(s)   Max(s) StdDev(s)
----------------------------------------------------------------------
approve              5        0     0.09     0.11     0.14     0.01
delete               5        0     0.09     0.10     0.13     0.02
del-main             5        0     0.08     0.09     0.11     0.01
edit                 5        0     0.00     0.00     0.00     0.00
init                 5        0     0.10     0.11     0.12     0.01
propose              5        0     0.11     0.12     0.14     0.01
prop-del             5        0     0.08     0.09     0.10     0.01
prop-del-main        5        0     0.08     0.09     0.10     0.01
pull                 5        0     0.07     0.07     0.08     0.00
push                 5        0     0.11     0.12     0.14     0.02

--- Docker Stats for porch-test-control-plane ---
CPU Usage (%): min=20.30 avg=80.20 max=121.93
Mem Usage (MiB): min=2563.07 avg=2579.80 max=2596.86
```

### Prerequisites

- Go 1.24 or later
- Porch installed and configured
- Kind cluster
- Docker (for container monitoring)
