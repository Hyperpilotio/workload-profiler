package models

import (
	benchmarkagent "github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

// ServiceConfig a struct that describes the address of the corresponding service
type ServiceConfig struct {
	Name       string            `bson:"name" json:"name"`
	HostConfig *CommandParameter `bson:"hostConfig,omitempty" json:"hostConfig,omitempty"`
	PortConfig *CommandParameter `bson:"portConfig,omitempty" json:"portConfig,omitempty"`
}

type Command struct {
	Image          string           `bson:"image" json:"image"`
	ParserURL      *string          `bson:"parserUrl,omitempty" json:"parserUrl,omitempty"`
	Path           string           `bson:"path" json:"path"`
	Args           []string         `bson:"args" json:"args"`
	ServiceConfigs *[]ServiceConfig `bson:"serviceConfigs,omitempty" json:"serviceConfigs,omitempty"`
}

type BenchmarkController struct {
	InitializeType string            `bson:"initializeType" json:"initializeType"`
	Initialize     *Command          `bson:"initialize" json:"initialize"`
	Command        LoadTesterCommand `bson:"command" json:"command"`
}

type SlowCookerAppLoad struct {
	Qps           int    `bson:"qps" json:"qps"`
	Concurrency   int    `bson:"concurrency" json:"concurrency"`
	Url           string `bson:"url" json:"url"`
	Method        string `bson:"method" json:"method"`
	TotalRequests int    `bson:"totalRequests" json:"totalRequests"`
}

type SlowCookerCalibrate struct {
	InitialConcurrency int `bson:"initialConcurrency" json:"initialConcurrency"`
	Step               int `bson:"step" json:"step"`
	RunsPerIntensity   int `bson:"runsPerIntensity" json:"runsPerIntensity"`
}

type SlowCookerController struct {
	AppLoad   *SlowCookerAppLoad   `bson:"appLoad" json:"appLoad"`
	Calibrate *SlowCookerCalibrate `bson:"calibrate" json:"calibrate"`
	LoadTime  string               `bson:"loadTime" json:"loadTime"`
}

type LocustController struct {
	StartCount   int    `bson:"startCount" json:"startCount"`
	EndCount     int    `bson:"endCount" json:"endCount"`
	StepCount    int    `bson:"stepCount" json:"stepCount"`
	StepDuration string `bson:"stepDuration" json:"stepDuration"`
}

type SLO struct {
	Metric string  `bson:"metric" json:"metric"`
	Value  float32 `bson:"value" json:"value"`
	Type   string  `bson:"type" json:"type"`
}

type BenchmarkConfig struct {
	Name           string                         `bson:"name" json:"name"`
	DurationConfig *benchmarkagent.DurationConfig `bson:"durationConfig" json:"durationConfig" binding:"required`
	CgroupConfig   *benchmarkagent.CgroupConfig   `bson:"cgroupConfig" json:"cgroupConfig"`
	HostConfig     *benchmarkagent.HostConfig     `bson:"hostConfig" json:"hostConfig"`
	NetConfig      *benchmarkagent.NetConfig      `bson:"netConfig" json:"netConfig"`
	IOConfig       *benchmarkagent.IOConfig       `bson:"ioConfig" json:"ioConfig"`
	Command        Command                        `bson:"command" json:"command" binding:"required"`
	PlacementHost  string                         `bson:"placementHost" json:"placementHost"`
}

type Benchmark struct {
	Name         string            `bson:"name" json:"name"`
	ResourceType string            `bson:"resourceType" json:"resourceType"`
	Image        string            `bson:"image" json:"image"`
	Intensity    int               `bson:"intensity" json:"intensity"`
	Configs      []BenchmarkConfig `bson:"configs" json:"configs"`
}

type ApplicationConfig struct {
	Name         string     `bson:"name" json:"name"`
	ServiceNames []string   `bson:"serviceNames" json:"serviceNames"`
	LoadTester   LoadTester `bson:"loadTester" json:"loadTester"`
	Type         string     `bson:"type" json:"type"`
	SLO          SLO        `bson:"slo" json:"slo"`
}

type IntensityArgument struct {
	Name          string `bson:"name" json:"name"`
	Arg           string `bson:"arg" json:"arg"`
	StartingValue int    `bson:"startingValue" json:"startingValue"`
	Step          int    `bson:"step" json:"step"`
}

type LoadTesterCommand struct {
	Image          string              `bson:"image" json:"image"`
	ParserURL      *string             `bson:"parserUrl,omitempty" json:"parserUrl,omitempty"`
	Path           string              `bson:"path" json:"path"`
	Args           []string            `bson:"args" json:"args"`
	ServiceConfigs *[]ServiceConfig    `bson:"serviceConfigs,omitempty" json:"serviceConfigs,omitempty"`
	IntensityArgs  []IntensityArgument `bson:"intensityArgs" json:"intensityArgs"`
}

type LoadTester struct {
	Name                 string                `bson:"name" json:"name"`
	BenchmarkController  *BenchmarkController  `bson:"benchmarkController" json:"benchmarkController"`
	LocustController     *LocustController     `bson:"locustController" json:"locustController"`
	SlowCookerController *SlowCookerController `bson:"slowCookerController" json:"slowCookerController"`
}

type CalibrationTestResult struct {
	LoadIntensity float64 `bson:"loadIntensity" json:"loadIntensity"`
	QosValue      float64 `bson:"qosValue" json:"qosValue"`
	Failures      uint64  `bson:"failures" json:"failures"`
}

type CalibrationResults struct {
	TestId       string                  `bson:"testId" json:"testId"`
	AppName      string                  `bson:"appName" json:"appName"`
	LoadTester   string                  `bson:"loadTester" json:"loadTester"`
	QosMetrics   []string                `bson:"qosMetrics" json:"qosMetrics"`
	TestDuration string                  `bson:"testDuration" json:"testDuration"`
	TestResults  []CalibrationTestResult `bson:"testResult" json:"testResult"`
	FinalResult  *CalibrationTestResult  `bson:"finalResult" json:"finalResult"`
}

type BenchmarkResult struct {
	Benchmark string  `bson:"benchmark" json:"benchmark"`
	Intensity int     `bson:"intensity" json:"intensity"`
	QosValue  float64 `bson:"qosValue" json:"qosValue"`
	Failures  uint64  `bson:"failures" json:"failures"`
}

type BenchmarkRunResults struct {
	TestId                string             `bson:"testId" json:"testId"`
	AppName               string             `bson:"appName" json:"appName"`
	NumServices           int                `bson:"numServices" json:"numServices"`
	Services              []string           `bson:"services" json:"services"`
	ServiceInTest         string             `bson:"serviceInTest" json:"serviceInTest"`
	ServiceNode           string             `bson:"serviceNode" json:"serviceNode"`
	LoadTester            string             `bson:"loadTester" json:"loadTester"`
	AppCapacity           float64            `bson:"appCapacity" json:"appCapacity"`
	SloMetric             string             `bson:"sloMetric" json:"sloMetric"`
	SloTolerance          float64            `bson:"sloTolerance" json:"sloTolerance"`
	TestDuration          string             `bson:"testDuration" json:"testDuration"`
	Benchmarks            []string           `bson:"benchmarks" json:"benchmarks"`
	TestResult            []*BenchmarkResult `bson:"testResult" json:"testResult"`
	ToleratedInterference []struct {
		Benchmark string `bson:"benchmark" json:"benchmark"`
		Intensity int    `bson:"intensity" json:"intensity"`
	} `bson:"toleratedInterference" json: "toleratedInterference"`
}
