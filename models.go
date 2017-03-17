package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type WorkloadBenchmarkRequest struct {
	HTTPMethod     string                 `json:"method"`
	Component      string                 `json:"component"`
	UrlPath        string                 `json:"path"`
	Body           map[string]interface{} `json:"body"`
	FormData       map[string]string      `json:"formData"`
	Duration       string                 `json:"duration"`
	StartBenchmark bool                   `json:"startBenchmark"`
}

type WorkloadBenchmark struct {
	Requests []WorkloadBenchmarkRequest `json:"requests"`
}

type Stage struct {
	ContainerBenchmarks []apis.Benchmark  `json:"containerBenchmarks"`
	WorkloadBenchmark   WorkloadBenchmark `json:"workloadBenchmark"`
}

type Profile struct {
	Deployment string  `json:"deployment"`
	Stages     []Stage `json:"stages"`
}
