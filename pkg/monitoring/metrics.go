package monitoring

import "log"

type Metrics struct {
	FifteenMinutes *Buffer
	OneHour        *Buffer
	ThreeHours     *Buffer
	LifeTime       *Buffer
}

func NewMetrics() *Metrics {
	m := new(Metrics)

	// 15m: 10s → 15 * 60 / 10 = 90 points
	m.FifteenMinutes = NewBuffer(10, 90)

	// 1h: 30s → 60 * 60 / 30 = 120 points
	m.OneHour = NewBuffer(30, 120)
	m.OneHour.AverageByLast = m.OneHour.Interval / m.FifteenMinutes.Interval
	log.Printf("OneHour.AverageByLast=%d\n", m.OneHour.AverageByLast)

	// 3h: 2m → 3 * 60 * 60 / 120 = 90 points
	m.ThreeHours = NewBuffer(2*60, 90)
	m.ThreeHours.AverageByLast = m.ThreeHours.Interval / m.FifteenMinutes.Interval
	log.Printf("ThreeHours.AverageByLast=%d\n", m.ThreeHours.AverageByLast)

	// TODO the size should be configurable
	// 4 week: 5m → 4 * 7 * 24 * 60 * 60 / 300 = 8064 points
	m.LifeTime = NewBuffer(5*60, 8064)
	m.LifeTime.AverageByLast = m.LifeTime.Interval / m.FifteenMinutes.Interval
	log.Printf("LifeTime.AverageByLast=%d\n", m.LifeTime.AverageByLast)
	return m
}

func (m *Metrics) Add(value int64) {
	if m.FifteenMinutes.IsTimeToUpdate() {
		m.FifteenMinutes.Add(value)
	}

	if m.FifteenMinutes.HasLast(m.OneHour.AverageByLast) && m.OneHour.IsTimeToUpdate() {
		m.OneHour.Add(m.FifteenMinutes.LastAverage(m.OneHour.AverageByLast))
	}

	if m.FifteenMinutes.HasLast(m.ThreeHours.AverageByLast) && m.ThreeHours.IsTimeToUpdate() {
		m.ThreeHours.Add(m.FifteenMinutes.LastAverage(m.ThreeHours.AverageByLast))
	}

	if m.FifteenMinutes.HasLast(m.LifeTime.AverageByLast) && m.LifeTime.IsTimeToUpdate() {
		m.LifeTime.Add(m.FifteenMinutes.LastAverage(m.LifeTime.AverageByLast))
	}
}
