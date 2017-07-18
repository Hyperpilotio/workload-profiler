package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/nu7hatch/gouuid"
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
	ProfileLog                *log.FileLog
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
	glog.V(1).Infof("Created new benchmark run with id: %s", id)

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	run := &BenchmarkRun{
		ProfileRun: ProfileRun{
			Id:                        id,
			ApplicationConfig:         applicationConfig,
			DeployerClient:            deployerClient,
			BenchmarkControllerClient: &clients.BenchmarkControllerClient{},
			SlowCookerClient:          &clients.SlowCookerClient{},
			MetricsDB:                 NewMetricsDB(config),
			DeploymentId:              deploymentId,
			ProfileLog:                log,
		},
		StartingIntensity:    startingIntensity,
		Step:                 step,
		SloTolerance:         sloTolerance,
		BenchmarkAgentClient: clients.NewBenchmarkAgentClient(),
		Benchmarks:           benchmarks,
	}

	return run, nil
}

func NewCalibrationRun(deploymentId string, applicationConfig *models.ApplicationConfig, config *viper.Viper) (*CalibrationRun, error) {
	id, err := generateId("calibrate")
	if err != nil {
		return nil, errors.New("Unable to generate calibration Id: " + err.Error())
	}

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	run := &CalibrationRun{
		ProfileRun: ProfileRun{
			Id:                        id,
			ApplicationConfig:         applicationConfig,
			DeployerClient:            deployerClient,
			BenchmarkControllerClient: &clients.BenchmarkControllerClient{},
			MetricsDB:                 NewMetricsDB(config),
			DeploymentId:              deploymentId,
			ProfileLog:                log,
		},
	}

	return run, nil
}

func (run *BenchmarkRun) deleteBenchmark(service string, benchmark models.Benchmark) error {
	for _, config := range benchmark.Configs {
		glog.V(1).Infof("Deleting benchmark config %s", config.Name)
		agentUrl, err := run.getBenchmarkAgentUrl(service, config)
		if err != nil {
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if err := run.BenchmarkAgentClient.DeleteBenchmark(agentUrl, config.Name, run.ProfileLog.Logger); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

	return nil
}

func replaceTargetingServiceAddress(controller *models.BenchmarkController, deployerClient *clients.DeployerClient, deploymentId string) error {
	errMsg := "Unable to replace the targeting service address because"
	if controller == nil {
		return fmt.Errorf("%s the pointer of controller is nil", errMsg)
	}
	if deployerClient == nil {
		return fmt.Errorf("%s the pointer of deployerClient is nil", errMsg)
	}
	if deploymentId == "" {
		return fmt.Errorf("%s the DeploymentId is a empty string", errMsg)
	}

	glog.V(3).Infof("func replaceTargetingServiceAddress: Initialize %+v", controller.Initialize)
	if controller.Initialize.ServiceConfigs != nil {
		for _, targetingService := range *controller.Initialize.ServiceConfigs {
			// NOTE we assume the targeting service is an unique one in this deployment process.
			// As a result, we should use GetServiceAddress function instead of GetColocatedServiceUrl
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name)
			if err != nil {
				return fmt.Errorf(
					"Unable to get service %s address: %s",
					targetingService.Name,
					err.Error())
			}
			// Initialize
			if targetingService.PortConfig != nil {
				controller.Initialize.Args = append(
					[]string{
						targetingService.PortConfig.Arg,
						strconv.FormatInt(serviceAddress.Port, 10),
					},
					controller.Initialize.Args...)
			}
			if targetingService.HostConfig != nil {
				controller.Initialize.Args = append(
					[]string{
						targetingService.HostConfig.Arg,
						serviceAddress.Host,
					},
					controller.Initialize.Args...)
			}
			glog.V(2).Infof("Arguments of Initialize command are %s", controller.Initialize.Args)
		}
	}

	glog.V(3).Infof("func replaceTargetingServiceAddress: Command %+v", controller.Command)
	if controller.Command.ServiceConfigs != nil {
		for _, targetingService := range *controller.Command.ServiceConfigs {
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name)
			if err != nil {
				return fmt.Errorf(
					"Unable to get service %s address: %s",
					targetingService.Name,
					err.Error())
			}

			// LoadTesterCommand
			if targetingService.PortConfig != nil {
				controller.Command.Args = append(
					[]string{
						targetingService.PortConfig.Arg,
						strconv.FormatInt(serviceAddress.Port, 10),
					},
					controller.Command.Args...)
			}
			if targetingService.HostConfig != nil {
				controller.Command.Args = append(
					[]string{
						targetingService.HostConfig.Arg,
						serviceAddress.Host,
					},
					controller.Command.Args...)
			}

			glog.V(2).Infof("Arguments of load testing command are %s", controller.Command.Args)
		}
	}

	return nil
}

func (run *CalibrationRun) runBenchmarkController(runId string, controller *models.BenchmarkController) error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	if err := replaceTargetingServiceAddress(controller, run.DeployerClient, run.DeploymentId); err != nil {
		return fmt.Errorf("Unable to replace service address [%v]: %s", run.ApplicationConfig.ServiceNames, err.Error())
	}

	startTime := time.Now()
	results, err := run.BenchmarkControllerClient.RunCalibration(
		loadTesterName, url, runId, controller, run.ApplicationConfig.SLO, run.ProfileLog.Logger)
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

	if b, err := json.MarshalIndent(calibrationResults, "", "  "); err == nil {
		run.ProfileLog.Logger.Infof("Store calibration results: %s", string(b))
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
	results, err := run.SlowCookerClient.RunCalibration(
		url, runId, run.ApplicationConfig.SLO, controller, run.ProfileLog.Logger)
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
		run.ProfileLog.Logger.Infof("Store calibration results: %s", string(b))
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

	response, err := run.BenchmarkControllerClient.RunBenchmark(
		loadTesterName, url, stageId, appIntensity, controller, run.ProfileLog.Logger)
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

	response, err := run.SlowCookerClient.RunBenchmark(
		url, stageId, appIntensity, &run.ApplicationConfig.SLO, controller, run.ProfileLog.Logger)
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

func (run *CalibrationRun) Run() error {
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

func (run *BenchmarkRun) getBenchmarkAgentUrl(service string, config models.BenchmarkConfig) (string, error) {
	var colocatedService string
	switch config.PlacementHost {
	case "loadtester":
		colocatedService = run.ApplicationConfig.LoadTester.Name
	case "service":
		colocatedService = service
	default:
		return "", errors.New("Unknown placement host for benchmark agent: " + config.PlacementHost)
	}

	glog.V(1).Infof("Getting benchmark agent url for colocated service %s from deployer client %+v", colocatedService, *run.DeployerClient)
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

func (run *BenchmarkRun) runBenchmark(id string, service string, benchmark models.Benchmark, intensity int) error {
	for _, config := range benchmark.Configs {
		glog.V(1).Infof("Run benchmark config: %+v", config)

		agentUrl, err := run.getBenchmarkAgentUrl(service, config)
		if err != nil {
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if err := run.BenchmarkAgentClient.CreateBenchmark(
			agentUrl, &benchmark, &config, intensity, run.ProfileLog.Logger); err != nil {
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

	glog.V(1).Infof("Starting app load test at intensity %.2f along with benchmark %s", appIntensity, benchmarkName)

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

	return nil, errors.New("No controller found in app load test request")
}

func (run *BenchmarkRun) runAppWithBenchmark(service string, benchmark models.Benchmark, appIntensity float64) ([]*models.BenchmarkResult, error) {
	currentIntensity := run.StartingIntensity
	results := []*models.BenchmarkResult{}

	counts := 0

	for {
		glog.V(1).Infof("Running benchmark %s at intensity %d along with app load test at intensity %.2f with service",
			benchmark.Name, currentIntensity, appIntensity, service)

		stageId, err := generateId(benchmark.Name)
		if err != nil {
			return nil, errors.New("Unable to generate stage id for benchmark " + benchmark.Name + ": " + err.Error())
		}

		if err = run.runBenchmark(stageId, service, benchmark, currentIntensity); err != nil {
			run.deleteBenchmark(service, benchmark)
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

		if err := run.deleteBenchmark(service, benchmark); err != nil {
			return nil, errors.New("Unable to delete benchmark " + benchmark.Name + ": " + err.Error())
		}

		if currentIntensity >= 100 {
			break
		}
		currentIntensity += run.Step
	}

	return results, nil
}

func (run *BenchmarkRun) Run() error {
	appName := run.ApplicationConfig.Name
	glog.V(1).Infof("Reading calibration results for app %s", appName)
	metric, err := run.MetricsDB.GetMetric("calibration", run.ApplicationConfig.Name, &models.CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + run.ApplicationConfig.Name + ": " + err.Error())
	}
	calibration := metric.(*models.CalibrationResults)

	// FIXME should support all the load tester includes slow cooker and locust
	// For now, only benchmark controller works
	if controller := run.ApplicationConfig.LoadTester.BenchmarkController; controller != nil {
		if err := replaceTargetingServiceAddress(controller, run.DeployerClient, run.DeploymentId); err != nil {
			return fmt.Errorf("Unable to replace service address [%v]: %s", run.ApplicationConfig.ServiceNames, err.Error())
		}
	}

	for _, service := range run.ApplicationConfig.ServiceNames {
		runResults := &models.BenchmarkRunResults{
			TestId:        run.Id,
			AppName:       appName,
			NumServices:   len(run.ApplicationConfig.ServiceNames),
			Services:      run.ApplicationConfig.ServiceNames,
			ServiceInTest: service,
			LoadTester:    calibration.LoadTester,
			AppCapacity:   calibration.FinalResult.LoadIntensity,
			SloMetric:     run.ApplicationConfig.SLO.Metric,
			SloTolerance:  run.SloTolerance,
			Benchmarks:    []string{},
			TestResult:    []*models.BenchmarkResult{},
		}

		for _, benchmark := range run.Benchmarks {
			glog.V(1).Infof("Starting benchmark runs for app %s with benchmark: %+v", appName, benchmark)
			results, err := run.runAppWithBenchmark(service, benchmark, calibration.FinalResult.LoadIntensity)
			if err != nil {
				return errors.New("Unable to run app " + appName + " along with benchmark " + benchmark.Name + ": " + err.Error())
			}
			glog.V(1).Infof("Finished running app %s along with benchmark %s", appName, benchmark.Name)
			runResults.Benchmarks = append(runResults.Benchmarks, benchmark.Name)
			for _, result := range results {
				runResults.TestResult = append(runResults.TestResult, result)
			}
		}

		glog.V(1).Infof("Storing benchmark results for app %s: %+v", run.ApplicationConfig.Name, runResults.TestResult)
		if err := run.MetricsDB.WriteMetrics("profiling", runResults); err != nil {
			return errors.New("Unable to store benchmark results for app " + run.ApplicationConfig.Name + ": " + err.Error())
		}

		if b, err := json.MarshalIndent(runResults, "", "  "); err != nil {
			run.ProfileLog.Logger.Errorf("Unable to indent run results: " + err.Error())
		} else {
			run.ProfileLog.Logger.Infof("Store benchmark results: %s", string(b))
		}
	}

	return nil
}
