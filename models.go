package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type LoadTestWithIntensity struct {
	Command   *Command `json:"command"`
	Intensity int      `json:"intensity"`
}

type LoadTestController struct {
	ServiceName string                   `json:"serviceName"`
	Initialize  *Command                 `json:"initialize"`
	LoadTests   []*LoadTestWithIntensity `json:"loadTests"`
	Cleanup     *Command                 `json:"cleanup"`
}

type LocustController struct {
	StartCount   int    `json:"startCount"`
	EndCount     int    `json:"endCount"`
	StepCount    int    `json:"stepCount"`
	StepDuration string `json:"stepDuration"`
}

type LoadController struct {
	LoadTestController *LoadTestController `json:"loadController"`
	LocustController   *LocustController   `json:"locustController"`
}

type Command struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type HTTPRequest struct {
	HTTPMethod string                 `json:"method"`
	Component  string                 `json:"component"`
	UrlPath    string                 `json:"path"`
	Body       map[string]interface{} `json:"body"`
	FormData   map[string]string      `json:"formData"`
	Duration   string                 `json:"duration"`
}

type Stage struct {
	Benchmarks  []apis.Benchmark `json:"benchmarks"`
	AppLoadTest LoadController   `json:"loadTest"`
}

type Profile struct {
	Stages []Stage `json:"stages"`
}
