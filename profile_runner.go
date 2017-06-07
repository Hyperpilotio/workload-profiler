package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nu7hatch/gouuid"
	"github.com/spf13/viper"
)

type CalibrationRun struct {
	Id                        string
	DeployerClient            *DeployerClient
	BenchmarkControllerClient *BenchmarkControllerClient
	DeploymentId              string
	MetricsDB                 *MetricsDB
	ApplicationConfig         *ApplicationConfig
}

type BenchmarkRun struct {
	BenchmarkAgentClient *BenchmarkAgentClient
	DeploymentId         string
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

func generateId() (string, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return "", errors.New("Unable to generate stage id: " + err.Error())
	}
	return u4.String(), nil
}

func NewBenchmarkRun() {
	/*
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
	*/
}

func NewCalibrationRun(deploymentId string, applicationConfig *ApplicationConfig, config *viper.Viper) (*CalibrationRun, error) {
	id, err := generateId()
	if err != nil {
		return nil, errors.New("Unable to generate calibration Id: " + err.Error())
	}

	deployerClient, deployerErr := NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	run := &CalibrationRun{
		Id:                        id,
		ApplicationConfig:         applicationConfig,
		DeployerClient:            deployerClient,
		BenchmarkControllerClient: &BenchmarkControllerClient{},
		MetricsDB:                 NewMetricsDB(config),
		DeploymentId:              deploymentId,
	}

	return run, nil
}

func (run *BenchmarkRun) cleanupBenchmark(stage *Stage) error {
	for _, benchmark := range stage.Benchmarks {
		if err := run.BenchmarkAgentClient.DeleteBenchmark(benchmark.Name); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

	return nil
}

func (run *BenchmarkRun) setupBenchmark(stage *Stage) error {
	for _, benchmark := range stage.Benchmarks {
		if err := run.BenchmarkAgentClient.CreateBenchmark(&benchmark); err != nil {
			return fmt.Errorf("Unable to create benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

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

	return nil
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

func (run *BenchmarkRun) runBenchmark(deployment string, stage *Stage) (*StageResult, error) {
	stageId, err := generateId()
	if err != nil {
		return nil, err
	}
	st := time.Now()
	startTime := st.Format(time.RFC3339)
	// TODO: Replace this with actually benchmark run
	//err = run.runAppLoadTest(stageId)
	results := &StageResult{
		Id:        stageId,
		StartTime: startTime,
	}

	return results, err
}

func (run *BenchmarkRun) Run(config *viper.Viper, profile *Profile) (*ProfileResults, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return nil, errors.New("Unable to generate profile id: " + err.Error())
	}

	profileId := u4.String()
	results := &ProfileResults{
		Id: profileId,
	}

	for _, stage := range profile.Stages {
		if err := run.setupBenchmark(&stage); err != nil {
			run.cleanupBenchmark(&stage)
			return nil, errors.New("Unable to setup stage: " + err.Error())
		}

		// TODO: Store stage results
		if result, err := run.runBenchmark(run.DeploymentId, &stage); err != nil {
			run.cleanupBenchmark(&stage)
			return nil, errors.New("Unable to run stage benchmark: " + err.Error())
		} else {
			et := time.Now()
			result.EndTime = et.Format(time.RFC3339)
			results.addProfileResult(*result)
		}

		if err := run.cleanupBenchmark(&stage); err != nil {
			return nil, errors.New("Unable to clean stage: " + err.Error())
		}
	}

	return results, nil
}

func (pr *ProfileResults) addProfileResult(sr StageResult) []StageResult {
	pr.StageResults = append(pr.StageResults, sr)
	return pr.StageResults
}
