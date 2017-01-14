package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type ContainerBenchmark struct {
	Name      string         `json:"name"`
	Count     int            `json:"count"`
	Resources apis.Resources `json:"resources"`
	Image     string         `json:"image"`
	Command   []string       `json:"command"`
}

type BenchmarkHTTPRequest struct {
	Method    string            `json:"method"`
	Component string            `json:"component"`
	Path      string            `json:"path"`
	Body      map[string]string `json:"body"`
}

type WorkloadBenchmark struct {
	BenchmarkHTTPRequests []BenchmarkHTTPRequest `json:"httpRequests"`
}

type Locust struct {
	Hatch int `json:"count"`
	Max   int `json:"max"`
}

type Stage struct {
	ContainerBenchmarks []ContainerBenchmark `json:"containerBenchmarks"`
	WorkloadBenchmark   WorkloadBenchmark    `json:"workloadBenchmark"`
	Duration            string               `json:"duration"`
}

type Profile struct {
	Deployment string  `json:"deployment"`
	Stages     []Stage `json:"stages"`
}
