package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type BenchmarkController struct {
	Initialize *Command          `json:"initialize"`
	Command    LoadTesterCommand `json:"command"`
}

type LocustController struct {
	StartCount   int    `json:"startCount"`
	EndCount     int    `json:"endCount"`
	StepCount    int    `json:"stepCount"`
	StepDuration string `json:"stepDuration"`
}

type Command struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type Stage struct {
	Benchmarks []apis.Benchmark `json:"benchmarks"`
}

type SLO struct {
	Metric string  `json:"metric"`
	Value  float32 `json:"value"`
	Unit   string  `json:"unit"`
}

type ApplicationConfig struct {
	Name       string     `json:"name"`
	LoadTester LoadTester `json:"loadTester"`
	Type       string     `json:"type"`
	SLO        SLO        `json:"slo"`
}

type Profile struct {
	ApplicationConfig *ApplicationConfig `json:"applicationConfig"`
	Stages            []Stage            `json:"stages"`
}

type IntensityArgument struct {
	Name          string `json:"name"`
	Arg           string `json:"arg"`
	StartingValue int    `json:"startingValue"`
	Step          int    `json:"step"`
}

type LoadTesterCommand struct {
	Path          string              `json:"path"`
	Args          []string            `json:"args"`
	IntensityArgs []IntensityArgument `json:"intensityArgs"`
}

type LoadTester struct {
	Name                string               `json:"name"`
	BenchmarkController *BenchmarkController `json:"benchmarkController"`
	LocustController    *LocustController    `json:"locustController"`
}

type CalibrationTestResult struct {
	LoadIntensity int `json:"loadIntensity"`
	QosMetric     int `json:"qosMetric"`
}

type CalibrationResults struct {
	TestId         string                  `json:"testId"`
	AppName        string                  `json:"appName"`
	LoadTester     string                  `json:"loadTester"`
	QosMetrics     []string                `json:"qosMetrics"`
	TestDuration   string                  `json:"testDuration"`
	TestResult     []CalibrationTestResult `json:"testResult"`
	FinalIntensity int                     `json:"finalIntensity"`
}
