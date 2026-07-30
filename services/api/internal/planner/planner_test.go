package planner

import (
	"testing"
	"time"
)

func TestFiveHundredHoursAcrossThirtyOneDays(t *testing.T) {
	days := make([]Day, 31)
	for i := range days {
		days[i] = Day{Date: time.Date(2026, 7, i+1, 0, 0, 0, 0, time.UTC), CapacitySeconds: 24 * 3600}
	}
	f := Calculate(500*3600, 0, 0, days)
	if f.RequiredDailySeconds != 58065 {
		t.Fatalf("required daily = %d, want 58065", f.RequiredDailySeconds)
	}
}

func TestSixHundredHoursAcrossThirtyOneDays(t *testing.T) {
	days := make([]Day, 31)
	for i := range days {
		days[i] = Day{CapacitySeconds: 24 * 3600}
	}
	f := Calculate(600*3600, 0, 0, days)
	if f.RequiredDailySeconds != 69678 {
		t.Fatalf("required daily = %d, want 69678", f.RequiredDailySeconds)
	}
}

func TestImpossibleGoalReportsShortfall(t *testing.T) {
	days := []Day{{CapacitySeconds: 3600}, {CapacitySeconds: 3600}}
	f := Calculate(10*3600, 0, 0, days)
	if f.IsFeasible || f.ShortfallSeconds != 8*3600 {
		t.Fatalf("unexpected forecast: %+v", f)
	}
}
