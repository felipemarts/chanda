package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Rate is a price in USD per one million tokens.
type Rate struct {
	InputCacheHit  float64 `json:"input_cache_hit"`
	InputCacheMiss float64 `json:"input_cache_miss"`
	Output         float64 `json:"output"`
}

// Cost returns the USD cost of a call with the given token counts.
func (r Rate) Cost(hit, miss, out int64) float64 {
	return (float64(hit)*r.InputCacheHit + float64(miss)*r.InputCacheMiss + float64(out)*r.Output) / 1e6
}

// ModelPrices holds the peak and off-peak rates of one model.
type ModelPrices struct {
	Peak    Rate `json:"peak"`
	OffPeak Rate `json:"offpeak"`
}

// Window is a recurring UTC time window on some weekdays. End is exclusive.
type Window struct {
	Days  string `json:"days"`  // "Mon-Fri" or "all"
	Start string `json:"start"` // "HH:MM" UTC
	End   string `json:"end"`   // "HH:MM" UTC, exclusive; "24:00" allowed

	weekdaysOnly bool
	startMin     int
	endMin       int
}

// PriceTable is the price file (see prices.deepseek.json).
type PriceTable struct {
	Source         string                 `json:"source"`
	Currency       string                 `json:"currency"`
	PeakWindowsUTC []Window               `json:"peak_windows_utc"`
	Models         map[string]ModelPrices `json:"models"`
}

// ParsePriceTable decodes and validates a price file.
func ParsePriceTable(b []byte) (*PriceTable, error) {
	var pt PriceTable
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pt); err != nil {
		return nil, fmt.Errorf("price table: %w", err)
	}
	if len(pt.Models) == 0 {
		return nil, fmt.Errorf("price table: no models")
	}
	for name, m := range pt.Models {
		for _, r := range []Rate{m.Peak, m.OffPeak} {
			if r.InputCacheHit <= 0 || r.InputCacheMiss <= 0 || r.Output <= 0 {
				return nil, fmt.Errorf("price table: model %q has a zero or negative rate", name)
			}
		}
	}
	for i := range pt.PeakWindowsUTC {
		if err := pt.PeakWindowsUTC[i].parse(); err != nil {
			return nil, fmt.Errorf("price table: peak window %d: %w", i, err)
		}
	}
	return &pt, nil
}

func parseHHMM(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, fmt.Errorf("bad time %q, want HH:MM", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || mm < 0 || mm > 59 || hh*60+mm > 24*60 {
		return 0, fmt.Errorf("bad time %q, want HH:MM", s)
	}
	return hh*60 + mm, nil
}

func (w *Window) parse() error {
	switch w.Days {
	case "Mon-Fri":
		w.weekdaysOnly = true
	case "all", "":
		w.weekdaysOnly = false
	default:
		return fmt.Errorf("days %q: want Mon-Fri or all", w.Days)
	}
	var err error
	if w.startMin, err = parseHHMM(w.Start); err != nil {
		return err
	}
	if w.endMin, err = parseHHMM(w.End); err != nil {
		return err
	}
	if w.endMin <= w.startMin {
		return fmt.Errorf("end %s must be after start %s (split windows that cross midnight)", w.End, w.Start)
	}
	return nil
}

func (w Window) contains(t time.Time) bool {
	t = t.UTC()
	if w.weekdaysOnly && (t.Weekday() == time.Saturday || t.Weekday() == time.Sunday) {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	return m >= w.startMin && m < w.endMin
}

// IsPeak reports whether t falls in a peak window. Chinese public holidays
// (off-peak per DeepSeek) are not modelled: they are treated as peak, which
// can only over-estimate the cost.
func (pt *PriceTable) IsPeak(t time.Time) bool {
	for _, w := range pt.PeakWindowsUTC {
		if w.contains(t) {
			return true
		}
	}
	return false
}

// RateFor returns the rate billed for a call that ran from start to end.
// A call that touches a peak window at either end is priced at peak
// (conservative: the provider's exact rule for straddling calls is not
// documented).
func (pt *PriceTable) RateFor(model string, start, end time.Time) (Rate, string, bool) {
	m, ok := pt.Models[model]
	if !ok {
		return Rate{}, "", false
	}
	if pt.IsPeak(start) || pt.IsPeak(end) {
		return m.Peak, "peak", true
	}
	return m.OffPeak, "offpeak", true
}

// MaxRate is the highest rate of a model across windows, used for the
// pre-call reservation.
func (pt *PriceTable) MaxRate(model string) (Rate, bool) {
	m, ok := pt.Models[model]
	if !ok {
		return Rate{}, false
	}
	return Rate{
		InputCacheHit:  math.Max(m.Peak.InputCacheHit, m.OffPeak.InputCacheHit),
		InputCacheMiss: math.Max(m.Peak.InputCacheMiss, m.OffPeak.InputCacheMiss),
		Output:         math.Max(m.Peak.Output, m.OffPeak.Output),
	}, true
}

// DailyWindow is an allowed UTC window that may cross midnight
// ("22:00-06:00"). Used by -window.
type DailyWindow struct {
	startMin, endMin int
}

// ParseDailyWindow parses "HH:MM-HH:MM".
func ParseDailyWindow(s string) (*DailyWindow, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil, fmt.Errorf("window %q: want HH:MM-HH:MM", s)
	}
	sm, err := parseHHMM(strings.TrimSpace(a))
	if err != nil {
		return nil, err
	}
	em, err := parseHHMM(strings.TrimSpace(b))
	if err != nil {
		return nil, err
	}
	if sm == em {
		return nil, fmt.Errorf("window %q is empty", s)
	}
	return &DailyWindow{startMin: sm, endMin: em}, nil
}

// Contains reports whether t (UTC) is inside the window.
func (d *DailyWindow) Contains(t time.Time) bool {
	t = t.UTC()
	m := t.Hour()*60 + t.Minute()
	if d.startMin < d.endMin {
		return m >= d.startMin && m < d.endMin
	}
	return m >= d.startMin || m < d.endMin
}
