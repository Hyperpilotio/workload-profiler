package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type BenchmarkController struct {
	Initialize *Command          `bson:"initialize" json:"initialize"`
	Command    LoadTesterCommand `bson:"command" json:"command"`
}

type LocustController struct {
	StartCount   int    `bson:"startCount" json:"startCount"`
	EndCount     int    `bson:"endCount" json:"endCount"`
	StepCount    int    `bson:"stepCount" json:"stepCount"`
	StepDuration string `bson:"stepDuration" json:"stepDuration"`
}

type Command struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type SLO struct {
	Metric string  `bson:"metric" json:"metric"`
	Value  float32 `bson:"value" json:"value"`
	Unit   string  `bson:"unit" json:"unit"`
}

type ApplicationConfig struct {
	Name       string     `bson:"name" json:"name"`
	LoadTester LoadTester `bson:"loadTester" json:"loadTester"`
	Type       string     `bson:"type" json:"type"`
	SLO        SLO        `bson:"slo" json:"slo"`
}

type IntensityArgument struct {
	Name          string `bson:"name" json:"name"`
	Arg           string `bson:"arg" json:"arg"`
	StartingValue int    `bson:"startingValue" json:"startingValue"`
	Step          int    `bson:"step" json:"step"`
}

type LoadTesterCommand struct {
	Path          string              `bson:"path" json:"path"`
	Args          []string            `bson:"args" json:"args"`
	IntensityArgs []IntensityArgument `bson:"intensityArgs" json:"intensityArgs"`
}

type LoadTester struct {
	Name                string               `bson:"name" json:"name"`
	BenchmarkController *BenchmarkController `bson:"benchmarkController" json:"benchmarkController"`
	LocustController    *LocustController    `bson:"locustController" json:"locustController"`
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

type CgroupConfig struct {
	SetCpuQuota bool `bson:"setCpuQuota" json:"setCpuQuota"`
}

type Benchmark struct {
	Name         string        `bson:"name" json:"name"`
	ResourceType string        `bson:"resourceType" json:"resourceType"`
	Image        string        `bson:"image" json:"image"`
	Intensity    int           `bson:"intensity" json:"intensity"`
	Command      Command       `bson:"command" json:"command"`
	Count        int           `bson:"count" json:"count"`
	CgroupConfig *CgroupConfig `bson:"cgroupConfig json:"cgroupConfig"`
}
