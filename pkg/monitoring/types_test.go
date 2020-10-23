package monitoring

import (
	"encoding/json"
	"testing"
)

func TestJsonOutput(t *testing.T) {
	monitoring := Monitoring{
		Spec: Spec{
			MemoryTotal: 0,
			GPUS: []GPUSpec{
				{
					Index:       0,
					MemoryTotal: 0,
				},
			},
		},
		Datasets: Datasets{
			FifteenMinutes: []Record{{
				Timestamp:      123,
				CpuUtilization: 0,
				MemoryUsed:     0,
				GPURecords:     nil,
			}},
			OneHour:    nil,
			ThreeHours: nil,
			LifeTime:   nil,
		},
	}
	output, _ := json.Marshal(monitoring)

	restore := &Monitoring{}
	err := json.Unmarshal(output, restore)
	if err != nil {
		panic(err)
	}

	if monitoring.Datasets.FifteenMinutes[0].Timestamp != restore.Datasets.FifteenMinutes[0].Timestamp {
		t.Fatal("Json Output should be same with restored data")
	}

}
