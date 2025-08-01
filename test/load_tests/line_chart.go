package main

import (
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

func generateLineItems(dataPoints []LatencyDataPoint) []opts.LineData {
	items := make([]opts.LineData, 0, len(dataPoints))
	for _, v := range dataPoints {
		items = append(items, opts.LineData{
			Value: []any{v.Timestamp, v.Latency.Seconds()},
		})
	}
	return items
}

func createLineChart(stats *Stats) {
	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:  types.ThemeWesteros,
			Width:  "1800px",
			Height: "900px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title: "Command Latency Over Time by Operation",
			Subtitle: fmt.Sprintf("Users: %d, Ramp-up: %s, Duration: %s",
				numUsersForChart, rampUpForChart, durationForChart),
			Top:  "3%",
			Left: "center",
		}),

		charts.WithLegendOpts(opts.Legend{
			Show:   opts.Bool(true),
			Top:    "8%",
			Left:   "center",
			Orient: "horizontal",
		}),

		charts.WithDataZoomOpts(opts.DataZoom{
			Type: "slider",
		}),

		charts.WithYAxisOpts(opts.YAxis{
			Type:         "value",
			Name:         "Latency (s)",
			NameLocation: "middle",
			NameGap:      60,
			AxisLabel: &opts.AxisLabel{
				FontSize: 12,
				Margin:   10,
			},
		}),

		charts.WithXAxisOpts(opts.XAxis{
			Type:         "time",
			Name:         "Time of Execution",
			NameLocation: "middle",
			NameGap:      40,
			AxisLabel: &opts.AxisLabel{
				FontSize: 12,
				Margin:   10,
			},
		}),

		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
		}),

		charts.WithGridOpts(opts.Grid{
			Top:    "18%",
			Bottom: "18%",
			Left:   "8%",
			Right:  "5%",
		}),
	)

	seriesData := make(map[string][]LatencyDataPoint)
	for _, v := range stats.Latencies {
		seriesData[v.Operation] = append(seriesData[v.Operation], v)
	}

	operations := make([]string, 0, len(seriesData))
	for op := range seriesData {
		operations = append(operations, op)
	}
	sort.Strings(operations)

	for _, operation := range operations {
		dataPoints := seriesData[operation]
		line.AddSeries(operation, generateLineItems(dataPoints)).
			SetSeriesOptions(
				charts.WithSeriesSymbolKeepAspect(false),
				charts.WithLineStyleOpts(opts.LineStyle{
					Width: 3,
				}),
				charts.WithLineChartOpts(opts.LineChart{
					Smooth: opts.Bool(false),
				}),
			)
	}

	f, err := os.Create("latency_chart.html")
	if err != nil {
		log.Printf("Error creating chart file: %v", err)
		return
	}
	defer f.Close()

	if err := line.Render(f); err != nil {
		log.Printf("Error rendering chart: %v", err)
		return
	}

	log.Println("Line chart created: latency_chart.html")
}
