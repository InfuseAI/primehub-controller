package monitoring

import "time"

type DataPoint struct {
	Value int64
}

type Buffer struct {
	Buffer        []DataPoint
	NextIndex     int64
	Max           int64
	Interval      int
	AverageByLast int
	LastUpdated   time.Time
}

func (b *Buffer) Add(value int64) {
	b.Buffer[b.NextIndex%b.Max].Value = value
	b.NextIndex++
	b.LastUpdated = time.Now()
}

func (b *Buffer) Average() int64 {
	var sum int64 = 0

	if b.NextIndex >= b.Max {
		for _, point := range b.Buffer {
			sum += point.Value
		}
		return sum / b.Max
	} else {
		for i, point := range b.Buffer {
			if i == int(b.NextIndex%b.Max) {
				break
			}
			sum += point.Value
		}
		return sum / (b.NextIndex % b.Max)
	}
}

func (b *Buffer) LastAverage(last int) int64 {
	if b.NextIndex < int64(last) {
		return b.Average()
	}
	var sum int64 = 0
	fromIndex := b.NextIndex - int64(last)
	for i := fromIndex; i < fromIndex+int64(last); i++ {
		sum += b.Buffer[int(i%b.Max)].Value
	}

	return sum / int64(last)
}

func (b *Buffer) Last(last int) []DataPoint {
	if !b.HasLast(last) {
		// give a empty slice if data is not ready
		return []DataPoint{}
	}
	p := make([]DataPoint, last)
	fromIndex := b.NextIndex - int64(last)
	for i := fromIndex; i < fromIndex+int64(last); i++ {
		p[i-fromIndex] = b.Buffer[int(i%b.Max)]
	}

	return p
}

func (b *Buffer) HasLast(last int) bool {
	return b.NextIndex >= int64(last) && int64(last) <= b.Max
}

func (b *Buffer) IsTimeToUpdate() bool {
	if b.NextIndex == 0 {
		return true
	}

	return b.LastUpdated.Before(time.Now().Add(time.Duration(-b.Interval) * time.Second))
}

func NewBuffer(interval int, size int64) *Buffer {
	p := new(Buffer)
	p.Interval = interval
	p.Max = size
	p.NextIndex = 0
	p.Buffer = make([]DataPoint, size)
	return p
}
