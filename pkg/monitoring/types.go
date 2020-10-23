package monitoring

type GPUSpec struct {
	Index       int `json:"index"`
	MemoryTotal int `json:"mem_total"`
}

type Spec struct {
	MemoryTotal int       `json:"mem_total"`
	GPUS        []GPUSpec `json:"gpu"`
}

type GPURecord struct {
	Index      int `json:"index"`
	MemoryUsed int `json:"mem_used"`
}

type Record struct {
	Timestamp      int64       `json:"timestamp"`
	CpuUtilization int64       `json:"cpu_util"`
	MemoryUsed     int64       `json:"mem_used"`
	GPURecords     []GPURecord `json:"gpu"`
}

type Datasets struct {
	FifteenMinutes []Record `json:"15m"`
	OneHour        []Record `json:"1h"`
	ThreeHours     []Record `json:"3h"`
	LifeTime       []Record `json:"lifetime"`
}

type Monitoring struct {
	Spec     Spec     `json:"spec"`
	Datasets Datasets `json:"datasets"`
}
