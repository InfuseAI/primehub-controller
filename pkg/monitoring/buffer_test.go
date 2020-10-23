package monitoring

import (
	"reflect"
	"testing"
	"time"
)

func TestBufferOperations(t *testing.T) {
	t.Log("Give a Buffer with size=3")
	buffer := NewBuffer(0, 3)

	t.Log("Add elements [1, 3]")
	buffer.Add(1)
	buffer.Add(3)

	if buffer.Average() != 2 {
		t.Fatal("Average should be 2")
	}
	t.Log("Average is 2")

	t.Log("Add new element [2] => [1, 3, 2]")
	buffer.Add(2)
	if buffer.Average() != 2 {
		t.Fatal("Average should be 2")
	}
	t.Log("Average is still 2")

	t.Log("Add new element [1] => [1, 3, 2, 1] => [3, 2, 1]")
	buffer.Add(1)
	if buffer.Average() != 2 {
		t.Fatal("Average should be 2")
	}
	t.Log("Average is still 2")
}

func TestLastElements(t *testing.T) {
	t.Log("Give a Buffer with size=5")
	buffer := NewBuffer(0, 10)

	t.Log("Set initial elements [1234, 55, 66]")
	buffer.Add(1234)
	buffer.Add(55)
	buffer.Add(66)

	if !reflect.DeepEqual([]DataPoint{
		{55}, {66},
	}, buffer.Last(2)) {
		t.Fatal("Last 2 should be [55, 66]")
	}
	t.Log("Last 2 should is [55, 66]")

	t.Log("Add elements [78, 763]")
	buffer.Add(78)
	buffer.Add(763)
	if !reflect.DeepEqual([]DataPoint{
		{78}, {763},
	}, buffer.Last(2)) {
		t.Fatal("Last 2 should be [78, 763]")
	}
	t.Log("Last 2 should is [78, 763]")

}

func TestLastElementsAverage(t *testing.T) {

	t.Log("Give a Buffer with size=10")
	buffer := NewBuffer(0, 10)
	buffer.Add(3)
	buffer.Add(2)
	buffer.Add(3)
	buffer.Add(4)

	t.Log("Add 4 elements [3, 2, 3, 4]")
	if buffer.Average() != 3 {
		t.Fatal("All elements average is 3")
	}
	t.Log("All elements average is 3")

	if buffer.LastAverage(3) != 3 {
		t.Fatal("Last 3 elements average should be 3")
	}
	t.Log("Last 3 elements average is 3")

	t.Log("Add 3 elements [3, 2, 3, 4] + [1, 90, 5]")
	buffer.Add(1)
	buffer.Add(90)
	buffer.Add(5)
	if buffer.LastAverage(4) != 25 {
		t.Fatal("Last 4 elements average should be 25")
	}
	t.Log("Last 4 elements average is 25")

	t.Log("Add 4 elements [...] + [25, 25, 25, 25]")
	buffer.Add(25)
	buffer.Add(25)
	buffer.Add(25)
	buffer.Add(25)
	if buffer.LastAverage(2) != 25 {
		t.Fatal("Last 2 elements average should be 25")
	}
	t.Log("Last 2 elements average is 25")
}

func TestIsTimeToUpdate(t *testing.T) {

	t.Log("Give a Buffer with size=1")
	buffer := NewBuffer(30, 1)

	if !buffer.IsTimeToUpdate() {
		t.Fatal("IsTimeToUpdate should be true if there is no data")
	}
	t.Log("IsTimeToUpdate is true if there is no data")

	buffer.Add(0)
	if buffer.IsTimeToUpdate() {
		t.Fatal("IsTimeToUpdate should be false if [lastUpdate + interval] > now")
	}
	t.Log("IsTimeToUpdate is false if [lastUpdate + interval] > now")

	buffer.LastUpdated = buffer.LastUpdated.Add(time.Second * time.Duration(-buffer.Interval))
	if !buffer.IsTimeToUpdate() {
		t.Fatal("IsTimeToUpdate should be true if [lastUpdate + interval] < now")
	}
	t.Log("IsTimeToUpdate is true if [lastUpdate + interval] < now")

}
