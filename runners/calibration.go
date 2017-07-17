package runners

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type CalibrationRun struct {
	ProfileRun
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
			MetricsDB:                 db.NewMetricsDB(config),
			DeploymentId:              deploymentId,
			ProfileLog:                log,
		},
	}

	return run, nil
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
