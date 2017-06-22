package main

type BenchmarkController struct {
	Initialize *Command          `bson:"initialize" json:"initialize"`
	Command    LoadTesterCommand `bson:"command" json:"command"`
}

type SlowCookerController struct {
	StartCount   int    `bson:"startCount" json:"startCount"`
	EndCount     int    `bson:"endCount" json:"endCount"`
	StepCount    int    `bson:"stepCount" json:"stepCount"`
	StepDuration string `bson:"stepDuration" json:"stepDuration"`
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
	Type   string  `bson:"type" json:"type"`
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
	Path          string              `bson:"path" json:"path"`
	Args          []string            `bson:"args" json:"args"`
	IntensityArgs []IntensityArgument `bson:"intensityArgs" json:"intensityArgs"`
}

type LoadTester struct {
	Name                 string                `bson:"name" json:"name"`
	BenchmarkController  *BenchmarkController  `bson:"benchmarkController" json:"benchmarkController"`
	LocustController     *LocustController     `bson:"locustController" json:"locustController"`
	SlowCookerController *SlowCookerController `bson:"slowCookerController" json:"slowCookerController"`
}

type BenchmarkMapping struct {
	Name         string `bson:"name" json:"name"`
	AgentMapping []struct {
		BenchmarkName string `bson:"benchmarkName" json:"benchmarkName"`
		AgentId       string `bson:"agentId" json:"agentId"`
	} `bson:"agentMapping" json:"agentMapping"`
}

type CalibrationTestResult struct {
	LoadIntensity float64 `bson:"loadIntensity" json:"loadIntensity"`
	QosValue      float64 `bson:"qosValue" json:"qosValue"`
}

type CalibrationResults struct {
	TestId         string                  `bson:"testId" json:"testId"`
	AppName        string                  `bson:"appName" json:"appName"`
	LoadTester     string                  `bson:"loadTester" json:"loadTester"`
	QosMetrics     []string                `bson:"qosMetrics" json:"qosMetrics"`
	TestDuration   string                  `bson:"testDuration" json:"testDuration"`
	TestResults    []CalibrationTestResult `bson:"testResult" json:"testResult"`
	FinalResult    *CalibrationTestResult  `bson:"finalResult" json:"finalResult"`
	FinalIntensity float64                 `bson:"finalIntensity" json:"finalIntensity"`
}

type BenchmarkResult struct {
	Benchmark string  `bson:"benchmark" json:"benchmark"`
	Intensity int     `bson:"intensity" json:"intensity"`
	QosValue  float64 `bson:"qosValue" json:"qosValue"`
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
