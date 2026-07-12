package bench

import (
	"runtime"
	"time"
)

// Report is the machine-readable output of a full benchmark run: the
// section 7 latency table (one Result per Command) plus enough environment
// detail to judge whether two reports are comparable. See
// docs/benchmark.md for the reference-machine assumptions this format
// depends on.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Binary      string    `json:"binary"`
	Fixture     string    `json:"fixture"`
	Seed        int64     `json:"fixture_seed"`
	CardCount   int       `json:"fixture_card_count"`
	Iterations  int       `json:"iterations"`
	Warmup      int       `json:"warmup"`
	Host        Host      `json:"host"`
	Results     []Result  `json:"results"`
}

// Host records the environment a Report was generated on, since subprocess
// latency is highly machine-dependent.
type Host struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	NumCPU int    `json:"num_cpu"`
	Go     string `json:"go_version"`
}

// CurrentHost returns Host for the machine this process is running on.
func CurrentHost() Host {
	return Host{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		NumCPU: runtime.NumCPU(),
		Go:     runtime.Version(),
	}
}
