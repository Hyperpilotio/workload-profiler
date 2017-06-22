package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
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

	StartingIntensity     int
	Step                  int
	SloTolerance          float64
	BenchmarkAgentClients map[string]BenchmarkAgentClient
	BenchmarkSets         []BenchmarkSet
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
	benchmarkSets []BenchmarkSet,
	deploymentId string,
	startingIntensity int,
	step int,
	sloTolerance float64,
	config *viper.Viper) (*BenchmarkRun, error) {

	id, err := generateId("benchmark")
	if err != nil {
		return nil, errors.New("Unable to generate Id for benchmark run: " + err.Error())
	}
	glog.V(1).Infof("New benchmark run with id: %s", id)

	deployerClient, deployerErr := NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	benchmarkAgentClients := make(map[string]BenchmarkAgentClient)
	agentIds := []string{"benchmark-agent-2", "benchmark-agent"} //TODO: change benchmark-agent-2 to benchmark-agent-bench later
	for _, agentId := range agentIds {
		url, urlErr := deployerClient.GetServiceUrl(deploymentId, agentId)
		if urlErr != nil {
			return nil, errors.New("Unable to get url for " + agentId + ": " + urlErr.Error())
		}

		benchmarkAgentClient, benchmarkAgentErr := NewBenchmarkAgentClient(url)
		if benchmarkAgentErr != nil {
			return nil, errors.New("Unable to create new client for " + agentId + ": " + benchmarkAgentErr.Error())
		}
		glog.V(1).Infof("Created new benchmark agent client for %s:%+v", agentId, benchmarkAgentClient)

		benchmarkAgentClients[agentId] = *benchmarkAgentClient
	}

	run := &BenchmarkRun{
		ProfileRun: ProfileRun{
			Id:                        id,
			ApplicationConfig:         applicationConfig,
			DeployerClient:            deployerClient,
			BenchmarkControllerClient: &BenchmarkControllerClient{},
			MetricsDB:                 NewMetricsDB(config),
			DeploymentId:              deploymentId,
		},
		StartingIntensity:     startingIntensity,
		Step:                  step,
		SloTolerance:          sloTolerance,
		BenchmarkAgentClients: benchmarkAgentClients,
		BenchmarkSets:         benchmarkSets,
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

func (run *BenchmarkRun) deleteBenchmarkSet(benchmarkSet BenchmarkSet) error {
	for _, benchmark := range benchmarkSet.Benchmarks {
		agentId := benchmarkSet.AgentMap[benchmark.Name]
		agentClient := run.BenchmarkAgentClients[agentId]
		if err := agentClient.DeleteBenchmark(benchmark.Name); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

	return nil
}

func (run *CalibrationRun) runBenchmarkController(runId string, controller *BenchmarkController) error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	startTime := time.Now()
	results, err := run.BenchmarkControllerClient.RunCalibration(url, runId, controller, run.ApplicationConfig.SLO)
	if err != nil {
		return errors.New("Unable to run calibration: " + err.Error())
	}

	testResults := []CalibrationTestResult{}
	for _, runResult := range results.Results.RunResults {
		qosValue := runResult.Results[run.ApplicationConfig.SLO.Metric].(float64)

		// TODO: For now we assume just one intensity argument, but we can support multiple
		// in the future.
		loadIntensity := runResult.IntensityArgs[controller.Command.IntensityArgs[0].Name].(float64)
		testResults = append(testResults, CalibrationTestResult{
			QosValue:      qosValue,
			LoadIntensity: loadIntensity,
		})
	}

	finalIntensity := results.Results.FinalIntensityArgs[controller.Command.IntensityArgs[0].Name].(float64)
	// Translate benchmark controller results to expected results format for analyzer
	finalResult := &CalibrationTestResult{
		LoadIntensity: finalIntensity,
		QosValue:      20000, // TODO: remove fake data later
	}
	calibrationResults := &CalibrationResults{
		TestId:         run.Id,
		AppName:        run.ApplicationConfig.Name,
		LoadTester:     loadTesterName,
		QosMetrics:     []string{run.ApplicationConfig.SLO.Type},
		TestDuration:   time.Since(startTime).String(),
		TestResults:    testResults,
		FinalResult:    finalResult,
		FinalIntensity: finalIntensity,
	}

	if err := run.MetricsDB.WriteMetrics("calibration", calibrationResults); err != nil {
		return errors.New("Unable to store calibration results: " + err.Error())
	}

	return nil
}

func (run *BenchmarkRun) runBenchmarkController(
	stageId string,
	appIntensity float64,
	controller *BenchmarkController) (*RunBenchmarkResponse, error) {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return nil, fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	response, err := run.BenchmarkControllerClient.RunBenchmark(url, stageId, appIntensity, controller)
	if err != nil {
		return nil, errors.New("Unable to run benchmark: " + err.Error())
	}

	return response, nil
}

func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
	}
}

func (run *BenchmarkRun) runLocustController(runId string, appIntensity float64, controller *LocustController) (*RunBenchmarkResponse, error) {
	return nil, errors.New("Unimplemented")
}

func (run *CalibrationRun) runLocustController(runId string, controller *LocustController) error {
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

func (run *BenchmarkRun) runBenchmarkSet(id string, benchmarkSet BenchmarkSet, intensity int) error {
	for _, benchmark := range benchmarkSet.Benchmarks {
		benchmark.Intensity = intensity
		agentId := benchmarkSet.AgentMap[benchmark.Name]
		agentClient := run.BenchmarkAgentClients[agentId]
		if err := agentClient.CreateBenchmark(&benchmark); err != nil {
			return fmt.Errorf("Unable to run benchmark %s with %s: %s",
				benchmark.Name, agentId, err.Error())
		}
	}

	return nil
}

func (run *BenchmarkRun) runApplicationLoadTest(stageId string, appIntensity float64) (*RunBenchmarkResponse, error) {
	loadTester := run.ApplicationConfig.LoadTester
	if loadTester.BenchmarkController != nil {
		return run.runBenchmarkController(stageId, appIntensity, loadTester.BenchmarkController)
	} else if loadTester.LocustController != nil {
		return run.runLocustController(stageId, appIntensity, loadTester.LocustController)
	} else {
		return nil, errors.New("No controller found in calibration request")
	}

}

func (run *BenchmarkRun) runAppWithBenchmark(benchmarkSet BenchmarkSet, appIntensity float64) ([]*BenchmarkResult, error) {
	currentIntensity := run.StartingIntensity
	results := []*BenchmarkResult{}

	counts := 0
	for {
		glog.Infof("Running benchmark %s at intensity %d along with app load test", benchmarkSet.Name, currentIntensity)
		stageId, err := generateId(benchmarkSet.Name)
		if err != nil {
			return nil, errors.New("Unable to generate stage id for benchmark " + benchmarkSet.Name + ": " + err.Error())
		}

		if err = run.runBenchmarkSet(stageId, benchmarkSet, currentIntensity); err != nil {
			run.deleteBenchmarkSet(benchmarkSet)
			return nil, errors.New("Unable to run benchmark " + benchmarkSet.Name + ": " + err.Error())
		}

		response, resultErr := run.runApplicationLoadTest(stageId, appIntensity)
		if resultErr != nil {
			// Run through all benchmarks even if one failed
			glog.Warningf("Unable to run app load test with benchmark %s: %s", benchmarkSet.Name, resultErr.Error())
		} else {
			for _, runResult := range response.Results {
				qosResults := runResult.Results
				qosMetric := fmt.Sprintf("%v", qosResults[run.ApplicationConfig.SLO.Metric])
				qosValue, parseErr := strconv.ParseFloat(qosMetric, 64)
				if parseErr != nil {
					return nil, fmt.Errorf("Unable to parse QoS value %s to float: %s", qosMetric, parseErr.Error())
				}

				result := &BenchmarkResult{
					Intensity: currentIntensity,
					QosValue:  qosValue,
					Benchmark: benchmarkSet.Name,
				}
				results = append(results, result)
			}
			counts += 1
		}

		if err := run.deleteBenchmarkSet(benchmarkSet); err != nil {
			return nil, errors.New("Unable to delete benchmark " + benchmarkSet.Name + ": " + err.Error())
		}

		if currentIntensity >= 100 {
			break
		}
		currentIntensity += run.Step
	}

	return results, nil
}

func (run *BenchmarkRun) Run() error {
	metric, err := run.MetricsDB.GetMetric("calibration", run.ApplicationConfig.Name, &CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + run.ApplicationConfig.Name + ": " + err.Error())
	}

	calibration := metric.(*CalibrationResults)
	glog.V(1).Infof("Read calibration results for app %s", run.ApplicationConfig.Name)

	runResults := &BenchmarkRunResults{
		TestId:        run.Id,
		AppName:       run.ApplicationConfig.Name,
		NumServices:   len(run.ApplicationConfig.ServiceNames),
		Services:      run.ApplicationConfig.ServiceNames,
		ServiceInTest: run.ApplicationConfig.Name, // TODO: We assume only one service for now
		LoadTester:    calibration.LoadTester,
		AppCapacity:   calibration.FinalIntensity,
		SloMetric:     run.ApplicationConfig.SLO.Metric,
		SloTolerance:  run.SloTolerance,
		Benchmarks:    []string{},
		TestResult:    []*BenchmarkResult{},
	}

	for _, benchmarkSet := range run.BenchmarkSets {
		glog.V(1).Infof("Starting benchmark runs with benchmark set: %+v", benchmarkSet)
		results, err := run.runAppWithBenchmark(benchmarkSet, calibration.FinalIntensity)
		if err != nil {
			return errors.New("Unable to run benchmark set " + benchmarkSet.Name + ": " + err.Error())
		}
		glog.V(1).Infof("Finished running app along with benchmark set %s", benchmarkSet.Name)
		runResults.Benchmarks = append(runResults.Benchmarks, benchmarkSet.Name)
		for _, result := range results {
			runResults.TestResult = append(runResults.TestResult, result)
		}
	}

	glog.V(1).Infof("Storing benchmark results for app %s: %+v", run.ApplicationConfig.Name, runResults.TestResult)
	if err := run.MetricsDB.WriteMetrics("profiling", runResults); err != nil {
		return errors.New("Unable to store benchmark results for app " + run.ApplicationConfig.Name + ": " + err.Error())
	}

	return nil
}
