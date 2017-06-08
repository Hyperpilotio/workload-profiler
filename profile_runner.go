package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nu7hatch/gouuid"
	"github.com/spf13/viper"
)

type ProfileRun struct {
	Id                        string
	DeployerClient            *DeployerClient
	BenchmarkControllerClient *BenchmarkControllerClient
	DeploymentId              string
	MetricsDB                 *MetricsDB
	ApplicationConfig         *ApplicationConfig
}

type CalibrationRun struct {
	ProfileRun
}

type BenchmarkRun struct {
	ProfileRun

	StartingIntensity    int
	Step                 int
	BenchmarkAgentClient *BenchmarkAgentClient
	Benchmarks           []Benchmark
}

type ProfileResults struct {
	Id           string
	StageResults []StageResult
}

type StageResult struct {
	Id        string
	StartTime string
	EndTime   string
}

func generateId(prefix string) (string, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return "", errors.New("Unable to generate stage id: " + err.Error())
	}
	return prefix + "-" + u4.String(), nil
}

func NewBenchmarkRun(
	applicationConfig *ApplicationConfig,
	benchmarks []Benchmark,
	deploymentId string,
	startingIntensity int,
	step int,
	config *viper.Viper) (*BenchmarkRun, error) {

	id, err := generateId("benchmark")
	if err != nil {
		return nil, errors.New("Unable to generate calibration Id: " + err.Error())
	}

	deployerClient, deployerErr := NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	url, urlErr := deployerClient.GetServiceUrl(deploymentId, "benchmark-agent")
	if urlErr != nil {
		return nil, errors.New("Unable to get benchmark-agent url: " + urlErr.Error())
	}

	benchmarkAgentClient, benchmarkAgentErr := NewBenchmarkAgentClient(url)
	if benchmarkAgentErr != nil {
		return nil, errors.New("Unable to create new benchmark agent client: " + benchmarkAgentErr.Error())
	}

	run := &BenchmarkRun{
		ProfileRun: ProfileRun{
			DeployerClient:            deployerClient,
			BenchmarkControllerClient: &BenchmarkControllerClient{},
			MetricsDB:                 NewMetricsDB(config),
			DeploymentId:              deploymentId,
			Id:                        id,
		},
		StartingIntensity:    startingIntensity,
		Step:                 step,
		BenchmarkAgentClient: benchmarkAgentClient,
		Benchmarks:           benchmarks,
	}

	return run, nil
}

func NewCalibrationRun(deploymentId string, applicationConfig *ApplicationConfig, config *viper.Viper) (*CalibrationRun, error) {
	id, err := generateId("calibrate")
	if err != nil {
		return nil, errors.New("Unable to generate calibration Id: " + err.Error())
	}

	deployerClient, deployerErr := NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	run := &CalibrationRun{
		ProfileRun: ProfileRun{
			Id:                        id,
			ApplicationConfig:         applicationConfig,
			DeployerClient:            deployerClient,
			BenchmarkControllerClient: &BenchmarkControllerClient{},
			MetricsDB:                 NewMetricsDB(config),
			DeploymentId:              deploymentId,
		},
	}

	return run, nil
}

func (run *BenchmarkRun) cleanupBenchmark(benchmark Benchmark) error {
	if err := run.BenchmarkAgentClient.DeleteBenchmark(benchmark.Name); err != nil {
		return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
			benchmark.Name, err.Error())
	}

	return nil
}

func (run *BenchmarkRun) setupBenchmark(benchmark Benchmark) error {
	return nil
}

func (run *CalibrationRun) runBenchmarkController(stageId string, controller *BenchmarkController) error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	startTime := time.Now()
	results, err := run.BenchmarkControllerClient.RunCalibration(url, stageId, controller, run.ApplicationConfig.SLO)
	if err != nil {
		return errors.New("Unable to run calibration: " + err.Error())
	}

	testResults := []CalibrationTestResult{}
	for _, runResult := range results.Results.RunResults {
		qosMetricString := runResult.Results[run.ApplicationConfig.SLO.Metric].(string)
		qosMetricFloat64, _ := strconv.ParseFloat(qosMetricString, 64)

		// TODO: For now we assume just one intensity argument, but we can support multiple
		// in the future.
		loadIntensity := runResult.IntensityArgs[controller.Command.IntensityArgs[0].Name].(float64)
		testResults = append(testResults, CalibrationTestResult{
			QosMetric:     int(qosMetricFloat64),
			LoadIntensity: int(loadIntensity),
		})
	}

	// Translate benchmark controller results to expected results format for analyzer
	finalIntensity := results.Results.FinalIntensityArgs[controller.Command.IntensityArgs[0].Name].(float64)
	calibrationResults := &CalibrationResults{
		TestId:         run.Id,
		AppName:        run.ApplicationConfig.Name,
		LoadTester:     loadTesterName,
		QosMetrics:     []string{run.ApplicationConfig.SLO.Unit},
		TestDuration:   time.Since(startTime).String(),
		TestResult:     testResults,
		FinalIntensity: int(finalIntensity),
	}

	if err := run.MetricsDB.WriteMetrics("calibration", calibrationResults); err != nil {
		return errors.New("Unable to store calibration results: " + err.Error())
	}

	return nil
}

func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
	}
}

func (run *CalibrationRun) runLocustController(stageId string, controller *LocustController) error {
	/*
		waitTime, err := time.ParseDuration(controller.StepDuration)
		if err != nil {
			return fmt.Errorf("Unable to parse wait time %s: %s", controller.StepDuration, err.Error())
		}

		url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, "locust-master")
		if urlErr != nil {
			return fmt.Errorf("Unable to retrieve locust master url: %s", urlErr.Error())
		}

		lastUserCount := 0
		nextUserCount := controller.StartCount

		for lastUserCount < nextUserCount {
			body := make(map[string]string)
			body["locust_count"] = strconv.Itoa(nextUserCount)
			body["hatch_rate"] = strconv.Itoa(nextUserCount)
			body["stage_id"] = stageId

			startRequest := HTTPRequest{
				HTTPMethod: "POST",
				UrlPath:    "/swarm",
				FormData:   body,
			}

			glog.Infof("Starting locust run with id %s, count %d", stageId, nextUserCount)
			if response, err := sendHTTPRequest(url, startRequest); err != nil {
				return fmt.Errorf("Unable to send start request for locust test %v: %s", startRequest, err.Error())
			} else if response.StatusCode() >= 300 {
				return fmt.Errorf("Unexpected response code when starting locust: %d, body: %s",
					response.StatusCode(), response.String())
			}

			glog.Infof("Waiting locust run for %s..", controller.StepDuration)
			<-time.After(waitTime)

			lastUserCount = nextUserCount
			nextUserCount = min(nextUserCount+controller.StepCount, controller.EndCount)
		}

		stopRequest := HTTPRequest{
			HTTPMethod: "GET",
			UrlPath:    "/stop",
		}

		glog.Infof("Stopping locust run..")

		if response, err := sendHTTPRequest(url, stopRequest); err != nil {
			return fmt.Errorf("Unable to send stop request for locust test: %s", err.Error())
		} else if response.StatusCode() >= 300 {
			return fmt.Errorf("Unexpected response code when stopping locust: %d, body: %s",
				response.StatusCode(), response.String())
		}
	*/

	return errors.New("Unimplemented")
}

func (run *CalibrationRun) Run() error {
	loadTester := run.ApplicationConfig.LoadTester
	if loadTester.BenchmarkController != nil {
		return run.runBenchmarkController(run.Id, loadTester.BenchmarkController)
	} else if loadTester.LocustController != nil {
		return run.runLocustController(run.Id, loadTester.LocustController)
	}

	return errors.New("No controller found in calibration request")
}

func (run *BenchmarkRun) runBenchmark(id string, benchmark Benchmark, intensity int) error {
	benchmark.Intensity = intensity
	if err := run.BenchmarkAgentClient.CreateBenchmark(&benchmark); err != nil {
		return fmt.Errorf("Unable to create benchmark %s: %s",
			benchmark.Name, err.Error())
	}

	return nil
}

func (run *BenchmarkRun) runAppWithBenchmark(benchmark Benchmark) (*ProfilingTestResult, error) {
	currentIntensity := run.StartingIntensity
	loadTester := run.ApplicationConfig.LoadTester
	for {
		if err := run.runBenchmark(run.Id, benchmark, currentIntensity); err != nil {
			run.cleanupBenchmark(benchmark)
			return nil, errors.New("Unable to setup stage: " + err.Error())
		}

		if loadTester.LocustController != nil {
			// run.runLocustController(run.Id, loadTester.LocustController)
		}

		if currentIntensity == 100 {
			break
		}
		currentIntensity += run.Step
	}

	return nil, nil
}

func (run *BenchmarkRun) Run() error {
	calibration, err := run.MetricsDB.GetMetric("calibration", run.ApplicationConfig.Name)
	if err != nil {
		return errors.New("Unable to get calibration: " + err.Error())
	}

	profilingResults := &ProfilingResults{
		TestId:       calibration.(*CalibrationResults).TestId,
		AppName:      run.ApplicationConfig.Name,
		LoadTester:   calibration.(*CalibrationResults).LoadTester,
		TestDuration: calibration.(*CalibrationResults).TestDuration,
	}

	if run.StartingIntensity <= 0 {
		run.StartingIntensity = calibration.(*CalibrationResults).FinalIntensity
	}

	benchmarks := []string{}
	profilingTestResults := []ProfilingTestResult{}
	for _, benchmark := range run.Benchmarks {
		testResults, err := run.runAppWithBenchmark(benchmark)
		if err != nil {
			return errors.New("Unable to run benchmark: " + err.Error())
		}
		benchmarks = append(benchmarks, benchmark.Name)
		profilingTestResults = append(profilingTestResults, *testResults)
	}
	profilingResults.Benchmarks = benchmarks
	profilingResults.TestResult = profilingTestResults

	if err := run.MetricsDB.WriteMetrics("profiling", profilingResults); err != nil {
		return errors.New("Unable to store profiling results: " + err.Error())
	}

	return nil
}
