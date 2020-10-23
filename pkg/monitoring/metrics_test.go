package monitoring

import (
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := NewMetrics()
	t.Log("Create a metrics with 30 points")
	AddData(m, 1)

	t.Log("When add 1 data")
	if m.FifteenMinutes.NextIndex != 1 {
		t.Fatal("FifteenMinutes length should be 1")
	}
	if m.OneHour.NextIndex != 0 {
		t.Fatal("OneHour length should be 0, because no enough data")
	}
	if m.ThreeHours.NextIndex != 0 {
		t.Fatal("ThreeHours length should be 0, because no enough data")
	}
	if m.LifeTime.NextIndex != 0 {
		t.Fatal("LifeTime length should be 0, because no enough data")
	}

	AddData(m, 29)
	t.Log("When add 11 data")
	if m.FifteenMinutes.NextIndex != 30 {
		t.Fatal("FifteenMinutes length should be 30")
	}

	if m.LifeTime.NextIndex != 1 {
		t.Fatal("LifeTime length should be 1,")
	}
}

func AddData(m *Metrics, numberOfData int) {
	for i := 0; i < numberOfData; i++ {
		m.Add(100)
		// fake last-update to allow adding new data
		lastUpdate := time.Now().Add(time.Second * time.Duration(-300))
		m.FifteenMinutes.LastUpdated = lastUpdate
		m.OneHour.LastUpdated = lastUpdate
		m.ThreeHours.LastUpdated = lastUpdate
		m.LifeTime.LastUpdated = lastUpdate
	}
}
