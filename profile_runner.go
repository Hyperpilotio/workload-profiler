package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/nu7hatch/gouuid"
	logging "github.com/op/go-logging"
	"github.com/spf13/viper"
)

type ProfileRun struct {
	Id                        string
	DeployerClient            *clients.DeployerClient
	BenchmarkControllerClient *clients.BenchmarkControllerClient
	SlowCookerClient          *clients.SlowCookerClient
	DeploymentId              string
	MetricsDB                 *MetricsDB
	ApplicationConfig         *models.ApplicationConfig
	Log                       *logging.Logger
}

type CalibrationRun struct {
	ProfileRun
}

type BenchmarkRun struct {
	ProfileRun

	StartingIntensity    int
	Step                 int
	SloTolerance         float64
	BenchmarkAgentClient *clients.BenchmarkAgentClient
	Benchmarks           []models.Benchmark
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
	applicationConfig *models.ApplicationConfig,
	benchmarks []models.Benchmark,
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

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	run := &BenchmarkRun{
		ProfileRun: ProfileRun{
			Id:                        id,
			ApplicationConfig:         applicationConfig,
			DeployerClient:            deployerClient,
			BenchmarkControllerClient: &clients.BenchmarkControllerClient{},
			SlowCookerClient:          &clients.SlowCookerClient{},
			MetricsDB:                 NewMetricsDB(config),
		},
		StartingIntensity:    startingIntensity,
		Step:                 step,
		SloTolerance:         sloTolerance,
		BenchmarkAgentClient: clients.NewBenchmarkAgentClient(),
		Benchmarks:           benchmarks,
	}

	return run, nil
}

func NewCalibrationRun(applicationConfig *ApplicationConfig, config *viper.Viper, log *logging.Logger) (*CalibrationRun, error) {
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
			BenchmarkControllerClient: &clients.BenchmarkControllerClient{},
			MetricsDB:                 NewMetricsDB(config),
			Log:                       log,
		},
	}

	return run, nil
}

func (run *BenchmarkRun) deleteBenchmark(benchmark models.Benchmark) error {
	for _, config := range benchmark.Configs {
		agentUrl, err := run.getBenchmarkAgentUrl(config)
		if err != nil {
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if err := run.BenchmarkAgentClient.DeleteBenchmark(agentUrl, benchmark.Name); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

	return nil
}

func (run *BenchmarkRun) GetId() string {
	return run.RunId
}

func (run *BenchmarkRun) GetApplicationConfig() *ApplicationConfig {
	return run.ApplicationConfig
}

func (run *BenchmarkRun) GetLog() *log.DeploymentLog {
	return run.ProfileRun.DeploymentLog
}

func (run *CalibrationRun) GetId() string {
	return run.RunId
}

func (run *CalibrationRun) GetApplicationConfig() *ApplicationConfig {
	return run.ApplicationConfig
}

func (run *CalibrationRun) GetLog() *log.DeploymentLog {
	return run.ProfileRun.DeploymentLog
}

func (run *CalibrationRun) runBenchmarkController(runId string, controller *BenchmarkController) error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	startTime := time.Now()
	results, err := run.BenchmarkControllerClient.RunCalibration(url, runId, controller, run.ApplicationConfig.SLO, run.ProfileRun.Log)
	if err != nil {
		return errors.New("Unable to run calibration: " + err.Error())
	}

	testResults := []models.CalibrationTestResult{}
	for _, runResult := range results.Results.RunResults {
		qosValue := runResult.Results[run.ApplicationConfig.SLO.Metric].(float64)

		// TODO: For now we assume just one intensity argument, but we can support multiple
		// in the future.
		loadIntensity := runResult.IntensityArgs[controller.Command.IntensityArgs[0].Name].(float64)
		testResults = append(testResults, models.CalibrationTestResult{
			QosValue:      qosValue,
			LoadIntensity: loadIntensity,
		})
	}

	finalIntensity := results.Results.FinalResults.IntensityArgs[controller.Command.IntensityArgs[0].Name].(float64)
	// Translate benchmark controller results to expected results format for analyzer
	finalResult := &models.CalibrationTestResult{
		LoadIntensity: finalIntensity,
		QosValue:      results.Results.FinalResults.Qos,
	}
	calibrationResults := &models.CalibrationResults{
		TestId:       run.Id,
		AppName:      run.ApplicationConfig.Name,
		LoadTester:   loadTesterName,
		QosMetrics:   []string{run.ApplicationConfig.SLO.Type},
		TestDuration: time.Since(startTime).String(),
		TestResults:  testResults,
		FinalResult:  finalResult,
	}

	if err := run.MetricsDB.WriteMetrics("calibration", calibrationResults); err != nil {
		return errors.New("Unable to store calibration results: " + err.Error())
	}

	return nil
}

func (run *CalibrationRun) runSlowCookerController(runId string, controller *models.SlowCookerController) error {
	glog.V(1).Infof("Running slow cooker with controller: %+v", controller)
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	startTime := time.Now()
	results, err := run.SlowCookerClient.RunCalibration(url, runId, run.ApplicationConfig.SLO, controller)
	if err != nil {
		return errors.New("Unable to run calibration with slow cooker: " + err.Error())
	}

	testResults := []models.CalibrationTestResult{}
	for _, runResult := range results.Results {
		qosValue := runResult.LatencyMs
		loadIntensity := runResult.Concurrency
		testResults = append(testResults, models.CalibrationTestResult{
			QosValue:      float64(qosValue),
			LoadIntensity: float64(loadIntensity),
		})
	}

	finalIntensity := results.FinalResult
	// Translate benchmark controller results to expected results format for analyzer
	finalResult := &models.CalibrationTestResult{
		LoadIntensity: float64(finalIntensity.Concurrency),
		QosValue:      float64(finalIntensity.LatencyMs),
	}
	calibrationResults := &models.CalibrationResults{
		TestId:       run.Id,
		AppName:      run.ApplicationConfig.Name,
		LoadTester:   loadTesterName,
		QosMetrics:   []string{run.ApplicationConfig.SLO.Type},
		TestDuration: time.Since(startTime).String(),
		TestResults:  testResults,
		FinalResult:  finalResult,
	}

	if err := run.MetricsDB.WriteMetrics("calibration", calibrationResults); err != nil {
		return errors.New("Unable to store calibration results: " + err.Error())
	}

	if b, err := json.MarshalIndent(calibrationResults, "", "  "); err == nil {
		log.Logger.Infof("Store calibration results: %s", string(b))
	}

	return nil
}

func (run *BenchmarkRun) runBenchmarkController(
	stageId string,
	appIntensity float64,
	benchmarkIntensity int,
	benchmarkName string,
	controller *models.BenchmarkController) ([]*models.BenchmarkResult, error) {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return nil, fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	response, err := run.BenchmarkControllerClient.RunBenchmark(url, stageId, appIntensity, controller)
	if err != nil {
		return nil, errors.New("Unable to run benchmark: " + err.Error())
	}

	results := []*models.BenchmarkResult{}
	for _, runResult := range response.Results {
		qosResults := runResult.Results
		qosMetric := fmt.Sprintf("%v", qosResults[run.ApplicationConfig.SLO.Metric])
		qosValue, parseErr := strconv.ParseFloat(qosMetric, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("Unable to parse QoS value %s to float: %s", qosMetric, parseErr.Error())
		}

		result := &models.BenchmarkResult{
			Benchmark: benchmarkName,
			Intensity: benchmarkIntensity,
			QosValue:  qosValue,
		}
		results = append(results, result)
	}

	return results, nil
}

func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
	}
}

func (run *BenchmarkRun) runLocustController(runId string, appIntensity float64, controller *models.LocustController) ([]*models.BenchmarkResult, error) {
	return nil, errors.New("Unimplemented")
}

func getSlowcookerBenchmarkQos(result *clients.SlowCookerBenchmarkResult, metric string) (int64, error) {
	switch metric {
	case "50":
		return result.Percentile50, nil
	case "95":
		return result.Percentile95, nil
	case "99":
		return result.Percentile99, nil
	}

	return 0, errors.New("Unsupported latency metric: " + metric)
}

func (run *BenchmarkRun) runSlowCookerController(
	stageId string,
	appIntensity float64,
	benchmarkIntensity int,
	benchmarkName string,
	controller *models.SlowCookerController) ([]*models.BenchmarkResult, error) {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return nil, fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	response, err := run.SlowCookerClient.RunBenchmark(url, stageId, appIntensity, &run.ApplicationConfig.SLO, controller)
	if err != nil {
		return nil, errors.New("Unable to run benchmark with slow cooker: " + err.Error())
	}

	results := []*models.BenchmarkResult{}
	for _, runResult := range response.Results {
		qosValue, err := getSlowcookerBenchmarkQos(&runResult, run.ApplicationConfig.SLO.Metric)
		if err != nil {
			return nil, errors.New("Unable to get benchmark qos from slow cooker result: " + err.Error())
		}

		result := &models.BenchmarkResult{
			Benchmark: benchmarkName,
			Intensity: benchmarkIntensity,
			QosValue:  float64(qosValue),
			Failures:  runResult.Failures,
		}
		results = append(results, result)
	}

	return results, nil
}

func (run *CalibrationRun) runLocustController(runId string, controller *models.LocustController) error {
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

func (run *CalibrationRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	loadTester := run.ApplicationConfig.LoadTester
	if loadTester.BenchmarkController != nil {
		return run.runBenchmarkController(run.Id, loadTester.BenchmarkController)
	} else if loadTester.LocustController != nil {
		return run.runLocustController(run.Id, loadTester.LocustController)
	} else if loadTester.SlowCookerController != nil {
		return run.runSlowCookerController(run.Id, loadTester.SlowCookerController)
	}

	return errors.New("No controller found in calibration request")
}

func (run *BenchmarkRun) getBenchmarkAgentUrl(config models.BenchmarkConfig) (string, error) {
	var colocatedService string
	switch config.PlacementHost {
	case "loadtester":
		colocatedService = run.ApplicationConfig.LoadTester.Name
	case "service":
		// TODO: We will have multiple services in the future
		colocatedService = run.ApplicationConfig.ServiceNames[0]
	default:
		return "", errors.New("Unknown placement host for benchmark: " + config.PlacementHost)
	}

	serviceUrl, err := run.DeployerClient.GetColocatedServiceUrl(run.DeploymentId, colocatedService, "benchmark-agent")
	if err != nil {
		return "", fmt.Errorf(
			"Unable to get service %s url located next to %s: %s",
			"benchmark-agent",
			colocatedService,
			err.Error())
	}

	return serviceUrl, nil
}

func (run *BenchmarkRun) runBenchmark(id string, benchmark models.Benchmark, intensity int) error {
	for _, config := range benchmark.Configs {
		agentUrl, err := run.getBenchmarkAgentUrl(config)
		if err != nil {
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if err := run.BenchmarkAgentClient.CreateBenchmark(agentUrl, &benchmark, &config, intensity); err != nil {
			return fmt.Errorf("Unable to run benchmark %s with intensity %d: %s",
				benchmark.Name, intensity, err.Error())
		}
	}

	return nil
}

func (run *BenchmarkRun) runApplicationLoadTest(
	stageId string,
	appIntensity float64,
	benchmarkIntensity int,
	benchmarkName string) ([]*models.BenchmarkResult, error) {
	loadTester := run.ApplicationConfig.LoadTester
	if loadTester.BenchmarkController != nil {
		return run.runBenchmarkController(
			stageId,
			appIntensity,
			benchmarkIntensity,
			benchmarkName,
			loadTester.BenchmarkController)
	} else if loadTester.LocustController != nil {
		return run.runLocustController(
			stageId,
			appIntensity,
			loadTester.LocustController)
	} else if loadTester.SlowCookerController != nil {
		return run.runSlowCookerController(
			stageId,
			appIntensity,
			benchmarkIntensity,
			benchmarkName,
			loadTester.SlowCookerController)
	}

	return nil, errors.New("No controller found in calibration request")
}

func (run *BenchmarkRun) runAppWithBenchmark(benchmark models.Benchmark, appIntensity float64) ([]*models.BenchmarkResult, error) {
	currentIntensity := run.StartingIntensity
	results := []*models.BenchmarkResult{}

	counts := 0
	for {
		glog.Infof("Running benchmark %s at intensity %d along with app load test intensity %d",
			benchmark.Name, currentIntensity, appIntensity)
		stageId, err := generateId(benchmark.Name)
		if err != nil {
			return nil, errors.New("Unable to generate stage id for benchmark " + benchmark.Name + ": " + err.Error())
		}

		if err = run.runBenchmark(stageId, benchmark, currentIntensity); err != nil {
			run.deleteBenchmark(benchmark)
			return nil, errors.New("Unable to run benchmark " + benchmark.Name + ": " + err.Error())
		}

		runResults, resultErr := run.runApplicationLoadTest(stageId, appIntensity, currentIntensity, benchmark.Name)
		if resultErr != nil {
			// Run through all benchmarks even if one failed
			glog.Warningf("Unable to run app load test with benchmark %s: %s", benchmark.Name, resultErr.Error())
		} else {
			for _, result := range runResults {
				results = append(results, result)
			}

			counts += 1
		}

		if err := run.deleteBenchmark(benchmark); err != nil {
			return nil, errors.New("Unable to delete benchmark " + benchmark.Name + ": " + err.Error())
		}

		if currentIntensity >= 100 {
			break
		}
		currentIntensity += run.Step
	}

	return results, nil
}

func (run *BenchmarkRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	metric, err := run.MetricsDB.GetMetric("calibration", run.ApplicationConfig.Name, &models.CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + run.ApplicationConfig.Name + ": " + err.Error())
	}

	calibration := metric.(*models.CalibrationResults)
	glog.V(1).Infof("Read calibration results for app %s", run.ApplicationConfig.Name)

	runResults := &models.BenchmarkRunResults{
		TestId:        run.Id,
		AppName:       run.ApplicationConfig.Name,
		NumServices:   len(run.ApplicationConfig.ServiceNames),
		Services:      run.ApplicationConfig.ServiceNames,
		ServiceInTest: run.ApplicationConfig.Name, // TODO: We assume only one service for now
		LoadTester:    calibration.LoadTester,
		AppCapacity:   calibration.FinalResult.LoadIntensity,
		SloMetric:     run.ApplicationConfig.SLO.Metric,
		SloTolerance:  run.SloTolerance,
		Benchmarks:    []string{},
		TestResult:    []*models.BenchmarkResult{},
	}

	for _, benchmark := range run.Benchmarks {
		glog.V(1).Infof("Starting benchmark runs with benchmark: %+v", benchmark)
		results, err := run.runAppWithBenchmark(benchmark, calibration.FinalResult.LoadIntensity)
		if err != nil {
			return errors.New("Unable to run benchmark " + benchmark.Name + ": " + err.Error())
		}
		glog.V(1).Infof("Finished running app along with benchmark %s", benchmark.Name)
		runResults.Benchmarks = append(runResults.Benchmarks, benchmark.Name)
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
