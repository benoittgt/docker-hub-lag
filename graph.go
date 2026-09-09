package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const graphDir = "graphs"

type measurement struct {
	timestamp   time.Time
	hubLagMs    int64
	registryMs  int64
}

var windows = []struct {
	name     string
	duration time.Duration
	label    string
}{
	{"hour", time.Hour, "Last hour"},
	{"day", 24 * time.Hour, "Last 24 hours"},
	{"week", 7 * 24 * time.Hour, "Last 7 days"},
	{"month", 30 * 24 * time.Hour, "Last 30 days"},
}

func sampleData() []measurement {
	now := time.Now().UTC()
	var data []measurement
	totalPoints := 30 * 24 * 12 // 30 days at 5min intervals
	for i := 0; i < totalPoints; i++ {
		ts := now.Add(-time.Duration(totalPoints-1-i) * 5 * time.Minute)
		hour := ts.Hour()
		hub := int64(80 + (i*7)%40 + hour*2)
		reg := int64(100 + (i*11)%30 + hour)
		// inject timeouts: ~1% of points
		if i%97 == 0 {
			hub = -1
		}
		if i%131 == 0 {
			reg = -1
		}
		// occasional spike
		if i%200 == 0 {
			hub = int64(300 + (i*3)%200)
		}
		data = append(data, measurement{
			timestamp:  ts,
			hubLagMs:   hub,
			registryMs: reg,
		})
	}
	return data
}

func runGraph(sample bool) error {
	var data []measurement
	var err error
	if sample {
		data = sampleData()
	} else {
		data, err = readCSV()
		if err != nil {
			return fmt.Errorf("reading CSV: %w", err)
		}
	}

	if err := os.MkdirAll(graphDir, 0755); err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, w := range windows {
		cutoff := now.Add(-w.duration)
		var filtered []measurement
		for _, m := range data {
			if m.timestamp.After(cutoff) {
				filtered = append(filtered, m)
			}
		}

		path := filepath.Join(graphDir, w.name+".svg")
		if err := writeSVG(path, w.label, filtered, w.duration); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}

	return nil
}

func readCSV() ([]measurement, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	var data []measurement
	for i, row := range records {
		if i == 0 {
			continue
		}
		if len(row) < 5 {
			continue
		}

		ts, err := time.Parse(time.RFC3339, row[0])
		if err != nil {
			continue
		}
		hubLag, _ := strconv.ParseInt(row[3], 10, 64)
		regLag, _ := strconv.ParseInt(row[4], 10, 64)

		data = append(data, measurement{
			timestamp:  ts,
			hubLagMs:   hubLag,
			registryMs: regLag,
		})
	}
	return data, nil
}

func writeSVG(path string, title string, data []measurement, window time.Duration) error {
	const (
		width      = 800
		height     = 340
		padLeft    = 70
		padRight   = 20
		padTop     = 40
		padBottom  = 90
	)

	chartW := float64(width - padLeft - padRight)
	chartH := float64(height - padTop - padBottom)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" font-family="monospace" font-size="12">`, width, height)
	fmt.Fprintf(f, "\n")

	fmt.Fprintf(f, `<rect width="%d" height="%d" fill="#1a1a2e"/>`, width, height)
	fmt.Fprintf(f, "\n")

	fmt.Fprintf(f, `<text x="%d" y="24" fill="#e0e0e0" font-size="14" text-anchor="middle">%s</text>`, width/2, title)
	fmt.Fprintf(f, "\n")

	if len(data) == 0 {
		fmt.Fprintf(f, `<text x="%d" y="%d" fill="#888" text-anchor="middle">No data</text>`, width/2, height/2)
		fmt.Fprintf(f, "\n</svg>\n")
		return nil
	}

	var maxVal int64
	for _, m := range data {
		if m.hubLagMs > maxVal {
			maxVal = m.hubLagMs
		}
		if m.registryMs > maxVal {
			maxVal = m.registryMs
		}
	}
	if maxVal <= 0 {
		maxVal = 100
	}
	maxVal = int64(math.Ceil(float64(maxVal)/100) * 100)

	now := time.Now().UTC()
	tMin := now.Add(-window)

	fmt.Fprintf(f, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#444" stroke-width="1"/>`,
		padLeft, padTop, padLeft, height-padBottom)
	fmt.Fprintf(f, "\n")
	fmt.Fprintf(f, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#444" stroke-width="1"/>`,
		padLeft, height-padBottom, width-padRight, height-padBottom)
	fmt.Fprintf(f, "\n")

	gridLines := 4
	for i := 0; i <= gridLines; i++ {
		val := maxVal * int64(i) / int64(gridLines)
		y := float64(padTop) + chartH*(1-float64(val)/float64(maxVal))
		fmt.Fprintf(f, `<line x1="%d" y1="%.0f" x2="%d" y2="%.0f" stroke="#333" stroke-width="1" stroke-dasharray="4"/>`,
			padLeft, y, width-padRight, y)
		fmt.Fprintf(f, "\n")
		fmt.Fprintf(f, `<text x="%d" y="%.0f" fill="#888" text-anchor="end" dominant-baseline="middle">%dms</text>`,
			padLeft-5, y, val)
		fmt.Fprintf(f, "\n")
	}

	timeLabels := 5
	for i := 0; i <= timeLabels; i++ {
		t := tMin.Add(window * time.Duration(i) / time.Duration(timeLabels))
		x := float64(padLeft) + chartW*float64(i)/float64(timeLabels)
		var label string
		if window <= time.Hour {
			label = t.Format("15:04")
		} else if window <= 24*time.Hour {
			label = t.Format("15:04")
		} else {
			label = t.Format("Jan 02")
		}
		fmt.Fprintf(f, `<text x="%.0f" y="%d" fill="#888" text-anchor="middle">%s</text>`,
			x, height-padBottom+20, label)
		fmt.Fprintf(f, "\n")
	}

	type point struct{ x, y float64 }

	toPoints := func(data []measurement, getValue func(m measurement) int64) []point {
		var pts []point
		for _, m := range data {
			val := getValue(m)
			if val < 0 {
				continue
			}
			xRatio := float64(m.timestamp.Sub(tMin)) / float64(window)
			if xRatio < 0 || xRatio > 1 {
				continue
			}
			x := float64(padLeft) + chartW*xRatio
			y := float64(padTop) + chartH*(1-float64(val)/float64(maxVal))
			pts = append(pts, point{x, y})
		}
		return pts
	}

	drawSeries := func(pts []point, color string) {
		if len(pts) == 0 {
			return
		}
		strokeW := "1.5"
		opacity := "0.6"
		dotR := "1.5"
		if len(pts) <= 20 {
			strokeW = "2"
			opacity = "1"
			dotR = "3"
		}
		if len(pts) >= 2 {
			var polyline string
			for _, p := range pts {
				if polyline != "" {
					polyline += " "
				}
				polyline += fmt.Sprintf("%.1f,%.1f", p.x, p.y)
			}
			fmt.Fprintf(f, `<polyline points="%s" fill="none" stroke="%s" stroke-width="%s" opacity="%s"/>`, polyline, color, strokeW, opacity)
			fmt.Fprintf(f, "\n")
		}
		if len(pts) <= 50 {
			for _, p := range pts {
				fmt.Fprintf(f, `<circle cx="%.1f" cy="%.1f" r="%s" fill="%s"/>`, p.x, p.y, dotR, color)
				fmt.Fprintf(f, "\n")
			}
		}
	}

	drawSeries(toPoints(data, func(m measurement) int64 { return m.hubLagMs }), "#4ecdc4")
	drawSeries(toPoints(data, func(m measurement) int64 { return m.registryMs }), "#ff6b6b")

	drawTimeouts := func(data []measurement, getValue func(m measurement) int64, color string) {
		for _, m := range data {
			if getValue(m) >= 0 {
				continue
			}
			xRatio := float64(m.timestamp.Sub(tMin)) / float64(window)
			if xRatio < 0 || xRatio > 1 {
				continue
			}
			x := float64(padLeft) + chartW*xRatio
			fmt.Fprintf(f, `<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="%s" stroke-width="1.5" stroke-dasharray="4,3" opacity="0.7"/>`,
				x, padTop, x, height-padBottom, color)
			fmt.Fprintf(f, "\n")
		}
	}

	drawTimeouts(data, func(m measurement) int64 { return m.hubLagMs }, "#4ecdc4")
	drawTimeouts(data, func(m measurement) int64 { return m.registryMs }, "#ff6b6b")

	fmt.Fprintf(f, `<circle cx="%d" cy="%d" r="4" fill="#4ecdc4"/>`, width-padRight-120, padTop+10)
	fmt.Fprintf(f, `<text x="%d" y="%d" fill="#e0e0e0" dominant-baseline="middle">Hub API</text>`, width-padRight-110, padTop+10)
	fmt.Fprintf(f, "\n")
	fmt.Fprintf(f, `<circle cx="%d" cy="%d" r="4" fill="#ff6b6b"/>`, width-padRight-120, padTop+28)
	fmt.Fprintf(f, `<text x="%d" y="%d" fill="#e0e0e0" dominant-baseline="middle">Registry</text>`, width-padRight-110, padTop+28)
	fmt.Fprintf(f, "\n")
	fmt.Fprintf(f, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#e0e0e0" stroke-width="1.5" stroke-dasharray="4,3" opacity="0.7"/>`,
		width-padRight-124, padTop+42, width-padRight-116, padTop+42)
	fmt.Fprintf(f, `<text x="%d" y="%d" fill="#e0e0e0" dominant-baseline="middle">Timeout</text>`, width-padRight-110, padTop+46)
	fmt.Fprintf(f, "\n")

	statsLine := func(getValue func(m measurement) int64, color string, y int) {
		var vals []int64
		timeouts := 0
		for _, m := range data {
			v := getValue(m)
			if v < 0 {
				timeouts++
				continue
			}
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			return
		}
		minV, maxV := vals[0], vals[0]
		var sum int64
		for _, v := range vals {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
			sum += v
		}
		avg := sum / int64(len(vals))
		fmt.Fprintf(f, `<text x="%d" y="%d" fill="%s" font-size="11">min: %dms  max: %dms  avg: %dms  timeouts: %d</text>`,
			padLeft, y, color, minV, maxV, avg, timeouts)
		fmt.Fprintf(f, "\n")
	}

	statsY := height - padBottom + 38
	statsLine(func(m measurement) int64 { return m.hubLagMs }, "#4ecdc4", statsY)
	statsLine(func(m measurement) int64 { return m.registryMs }, "#ff6b6b", statsY+16)

	fmt.Fprintf(f, "</svg>\n")
	return nil
}
