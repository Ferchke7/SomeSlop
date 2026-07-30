package planner

import (
	"errors"
	"math"
	"sort"
	"time"
)

type Forecast struct {
	TargetSeconds        int64   `json:"targetSeconds"`
	TrackedSeconds       int64   `json:"trackedSeconds"`
	PlannedSeconds       int64   `json:"plannedSeconds"`
	RemainingSeconds     int64   `json:"remainingSeconds"`
	UnscheduledSeconds   int64   `json:"unscheduledSeconds"`
	EligibleDays         int     `json:"eligibleDays"`
	RequiredDailySeconds int64   `json:"requiredDailySeconds"`
	AvailableSeconds     int64   `json:"availableSeconds"`
	ShortfallSeconds     int64   `json:"shortfallSeconds"`
	CompletionPercent    float64 `json:"completionPercent"`
	ForecastPercent      float64 `json:"forecastPercent"`
	IsFeasible           bool    `json:"isFeasible"`
}

type Day struct {
	Date            time.Time
	CapacitySeconds int64
	AlreadyPlanned  int64
}

type Allocation struct {
	Date    time.Time `json:"date"`
	Seconds int64     `json:"seconds"`
}

func Calculate(target, tracked, planned int64, days []Day) Forecast {
	remaining := max64(target-tracked, 0)
	unscheduled := max64(target-tracked-planned, 0)
	var available int64
	for _, day := range days {
		available += max64(day.CapacitySeconds-day.AlreadyPlanned, 0)
	}
	required := int64(0)
	if len(days) > 0 && remaining > 0 {
		required = int64(math.Ceil(float64(remaining) / float64(len(days))))
	}
	shortfall := max64(unscheduled-available, 0)
	completion := percentage(tracked, target)
	forecast := percentage(tracked+planned, target)
	return Forecast{
		TargetSeconds: target, TrackedSeconds: tracked, PlannedSeconds: planned,
		RemainingSeconds: remaining, UnscheduledSeconds: unscheduled,
		EligibleDays: len(days), RequiredDailySeconds: required,
		AvailableSeconds: available, ShortfallSeconds: shortfall,
		CompletionPercent: completion, ForecastPercent: forecast,
		IsFeasible: shortfall == 0,
	}
}

func Distribute(seconds int64, days []Day, sessionSeconds int64) ([]Allocation, error) {
	if seconds < 0 || sessionSeconds <= 0 {
		return nil, errors.New("invalid planner input")
	}
	if seconds == 0 {
		return []Allocation{}, nil
	}
	sorted := append([]Day(nil), days...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
	remaining := seconds
	result := make([]Allocation, 0, len(sorted))
	for remaining > 0 {
		progress := false
		for _, day := range sorted {
			free := max64(day.CapacitySeconds-day.AlreadyPlanned, 0)
			for _, item := range result {
				if sameDay(item.Date, day.Date) {
					free -= item.Seconds
				}
			}
			if free <= 0 {
				continue
			}
			chunk := min64(sessionSeconds, min64(free, remaining))
			for i := range result {
				if sameDay(result[i].Date, day.Date) {
					result[i].Seconds += chunk
					chunk = 0
					break
				}
			}
			if chunk > 0 {
				result = append(result, Allocation{Date: day.Date, Seconds: chunk})
			}
			remaining -= min64(sessionSeconds, min64(free, remaining))
			progress = true
			if remaining == 0 {
				break
			}
		}
		if !progress {
			return result, errors.New("not enough calendar capacity")
		}
	}
	return result, nil
}

func percentage(value, target int64) float64 {
	if target <= 0 {
		return 0
	}
	return math.Round(math.Min(float64(value)/float64(target)*100, 1000)*10) / 10
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
