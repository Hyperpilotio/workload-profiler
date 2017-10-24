package runners

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type BaseBenchmarkRun struct {
	ProfileRun

	BenchmarkAgentClient *clients.BenchmarkAgentClient
}

type BenchmarkRun struct {
	BaseBenchmarkRun

	StartingIntensity int
	Step              int
	SloTolerance      float64
	Benchmarks        []models.Benchmark
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

func NewBenchmarkRun(
	applicationConfig *models.ApplicationConfig,
	benchmarks []models.Benchmark,
	startingIntensity int,
	step int,
	sloTolerance float64,
	config *viper.Viper) (*BenchmarkRun, error) {

	id, err := generateId("benchmarks")
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
		BaseBenchmarkRun: BaseBenchmarkRun{
			ProfileRun: ProfileRun{
				Id:                        id,
				ApplicationConfig:         applicationConfig,
				DeployerClient:            deployerClient,
				BenchmarkControllerClient: &clients.BenchmarkControllerClient{},
				SlowCookerClient:          &clients.SlowCookerClient{},
				MetricsDB:                 db.NewMetricsDB(config),
				ProfileLog:                log,
				Created:                   time.Now(),
				DirectJob:                 false,
			},
			BenchmarkAgentClient: clients.NewBenchmarkAgentClient(),
		},
		StartingIntensity: startingIntensity,
		Step:              step,
		SloTolerance:      sloTolerance,
		Benchmarks:        benchmarks,
	}

	return run, nil
}

func (run *BaseBenchmarkRun) GetResults() <-chan *jobs.JobResults {
	return nil
}

func (run *BaseBenchmarkRun) SetFailed(error string) {}

func (run *BenchmarkRun) deleteBenchmark(service string, benchmark models.Benchmark) error {
	for _, config := range benchmark.Configs {
		run.ProfileLog.Logger.Infof("Deleting benchmark config %s", config.Name)
		agentUrls, err := run.ProfileRun.GetColocatedAgentUrls("benchmark-agent", service, config.PlacementHost)
		if err != nil {
			run.ProfileLog.Logger.Warningf("Unable to get benchmark agent url: " + err.Error())
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if err := run.BenchmarkAgentClient.DeleteBenchmark(agentUrls[0], config.Name, run.ProfileLog.Logger); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				benchmark.Name, err.Error())
		}
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
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
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

func (run *BenchmarkRun) runLocustController(runId string, appIntensity float64, controller *models.LocustController) ([]*models.BenchmarkResult, error) {
	return nil, errors.New("Unimplemented")
}

func (run *BenchmarkRun) runSlowCookerController(
	stageId string,
	appIntensity float64,
	benchmarkIntensity int,
	benchmarkName string,
	controller *models.SlowCookerController) ([]*models.BenchmarkResult, error) {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
	if urlErr != nil {
		return nil, fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	response, err := run.SlowCookerClient.RunBenchmark(
		url, stageId, appIntensity, controller.Calibrate.InitialConcurrency, controller, run.ProfileLog.Logger)
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

func (run *BaseBenchmarkRun) runBenchmark(id string, service string, benchmark models.Benchmark, intensity int) error {
	for _, config := range benchmark.Configs {
		run.ProfileLog.Logger.Infof("Starting to run benchmark config: %+v", config)

		agentUrls, err := run.ProfileRun.GetColocatedAgentUrls("benchmark-agent", service, config.PlacementHost)
		if err != nil {
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if err := run.BenchmarkAgentClient.CreateBenchmark(
			agentUrls[0], &benchmark, &config, intensity, run.ProfileLog.Logger); err != nil {
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

	run.ProfileLog.Logger.Infof("Starting app load test at intensity %.2f along with benchmark %s", appIntensity, benchmarkName)

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
		run.ProfileLog.Logger.Infof(
			"Running benchmark %s at intensity %d along with app load test at intensity %.2f with service",
			benchmark.Name,
			currentIntensity,
			appIntensity,
			service)

		stageId, err := generateId(benchmark.Name)
		if err != nil {
			return nil, errors.New("Unable to generate stage id for benchmark " + benchmark.Name + ": " + err.Error())
		}

		if err = run.BaseBenchmarkRun.runBenchmark(stageId, service, benchmark, currentIntensity); err != nil {
			run.deleteBenchmark(service, benchmark)
			return nil, errors.New("Unable to run benchmark " + benchmark.Name + ": " + err.Error())
		}

		runResults, resultErr := run.runApplicationLoadTest(stageId, appIntensity, currentIntensity, benchmark.Name)
		if resultErr != nil {
			run.deleteBenchmark(service, benchmark)
			return nil, fmt.Errorf("Unable to run app load test with benchmark %s: %s", benchmark.Name, resultErr.Error())
		}

		for _, result := range runResults {
			results = append(results, result)
		}

		counts += 1

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

func (run *BenchmarkRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId
	appName := run.ApplicationConfig.Name
	run.ProfileLog.Logger.Infof("Reading calibration results for app %s", appName)
	metric, err := run.MetricsDB.GetMetric("calibration", run.ApplicationConfig.Name, &models.CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + run.ApplicationConfig.Name + ": " + err.Error())
	}
	calibration := metric.(*models.CalibrationResults)

	// FIXME should support all the load tester includes slow cooker and locust
	// For now, only benchmark controller works
	if controller := run.ApplicationConfig.LoadTester.BenchmarkController; controller != nil {
		if err := replaceTargetingServiceAddress(controller, run.DeployerClient, run.DeploymentId, run.ProfileLog.Logger); err != nil {
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
			run.ProfileLog.Logger.Infof("Starting benchmark runs for app %s with benchmark: %+v", appName, benchmark)
			results, err := run.runAppWithBenchmark(service, benchmark, calibration.FinalResult.LoadIntensity)
			if err != nil {
				run.ProfileLog.Logger.Warningf("Unable to run app %s with benchmark %s: %s ", appName, benchmark.Name, err.Error())
			} else {
				run.ProfileLog.Logger.Infof("Finished running app %s along with benchmark %s", appName, benchmark.Name)
			}
			runResults.Benchmarks = append(runResults.Benchmarks, benchmark.Name)
			for _, result := range results {
				runResults.TestResult = append(runResults.TestResult, result)
			}
		}

		run.ProfileLog.Logger.Infof("Storing benchmark results for app %s: %+v", run.ApplicationConfig.Name, runResults.TestResult)
		if err := run.MetricsDB.WriteMetrics("profiling", runResults); err != nil {
			message := "Unable to store benchmark results for app " + run.ApplicationConfig.Name + ": " + err.Error()
			run.ProfileLog.Logger.Warningf(message)
			return errors.New(message)
		}

		if b, err := json.MarshalIndent(runResults, "", "  "); err != nil {
			run.ProfileLog.Logger.Errorf("Unable to indent run results: " + err.Error())
		} else {
			run.ProfileLog.Logger.Infof("Store benchmark results: %s", string(b))
		}
	}

	return nil
}
