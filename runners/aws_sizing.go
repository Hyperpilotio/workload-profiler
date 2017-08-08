package runners

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type SizeRunResults struct {
	Error        string
	InstanceType string
	RunId        string
	Duration     string
	AppName      string
	QosValue     models.SLO
}

type AWSSizingRunResults struct {
	RunId       string             `bson:"runId" json:"runId"`
	Duration    string             `bson:"duration" json:"duration"`
	AppName     string             `bson:"appName" json:"appName"`
	TestResults map[string]float32 `bson:"testResult" json:"testResult"`
}

// AWSSizingRun is the overall app request for find best instance type in AWS.
// It spawns multiple AWSSizingSingleRun based on analyzer recommendations.
// Note that AWSSizingRun doesn't implement the job interface, and won't be queued
// up to the job manager to run.
type AWSSizingRun struct {
	ProfileRun

	Config         *viper.Viper
	JobManager     *jobs.JobManager
	AnalyzerClient *clients.AnalyzerClient
}

// AWSSizingSingleRun represents a single benchmark run for a particular
// AWS instance type.
type AWSSizingSingleRun struct {
	ProfileRun

	InstanceType string

	ResultsChan chan SizeRunResults
}

func NewAWSSizingRun(jobManager *jobs.JobManager, applicationConfig *models.ApplicationConfig, config *viper.Viper) (*AWSSizingRun, error) {
	id, err := generateId("awssizing")
	if err != nil {
		return nil, errors.New("Unable to generate id: " + err.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	analyzerClient, err := clients.NewAnalyzerClient(config)
	if err != nil {
		return nil, errors.New("Unable to create analyzer client: " + err.Error())
	}

	return &AWSSizingRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			MetricsDB:         db.NewMetricsDB(config),
			ProfileLog:        log,
			Created:           time.Now(),
		},
		AnalyzerClient: analyzerClient,
		JobManager:     jobManager,
		Config:         config,
	}, nil
}

func (run *AWSSizingRun) Run() error {
	log := run.ProfileLog.Logger
	appName := run.ApplicationConfig.Name
	results := make(map[string]float32)
	instanceTypes, err := run.AnalyzerClient.GetNextInstanceTypes(run.Id, appName, results, log)
	if err != nil {
		return errors.New("Unable to fetch initial instance types: " + err.Error())
	}
	log.Infof("Received initial instance types: %+v", instanceTypes)

	startTime := time.Now()
	for len(instanceTypes) > 0 {
		resultChans := make(map[string]chan SizeRunResults)
		for _, instanceType := range instanceTypes {
			newId := run.GetId() + "-" + instanceType
			newApplicationConfig := &models.ApplicationConfig{}
			deepCopy(run.ApplicationConfig, newApplicationConfig)
			singleRun, err := NewAWSSizingSingleRun(
				newId,
				instanceType,
				newApplicationConfig,
				run.Config)
			if err != nil {
				// TODO: clean up
				return errors.New("Unable to create AWS single run: " + err.Error())
			}
			run.JobManager.AddJob(singleRun)
			resultChans[instanceType] = singleRun.ResultsChan
		}

		for instanceType, resultChan := range resultChans {
			result := <-resultChan
			if result.Error != "" {
				log.Warningf(
					"Failed to aws single run with instance type %s: %s", instanceType, result.Error)
				// TODO: Retry?
			} else {
				qosValue := result.QosValue.Value
				log.Infof("Received sizing run value %0.2f with instance type %s", qosValue, instanceType)
				results[instanceType] = qosValue
			}
		}

		sugggestInstanceTypes, err := run.AnalyzerClient.GetNextInstanceTypes(run.Id, appName, results, log)
		if err != nil {
			return errors.New("Unable to get next instance types from analyzer: " + err.Error())
		}

		log.Infof("Received next instance types to run sizing: %s", sugggestInstanceTypes)
		instanceTypes = sugggestInstanceTypes
	}

	awsSizingRunResults := &AWSSizingRunResults{
		RunId:       run.Id,
		AppName:     appName,
		Duration:    time.Since(startTime).String(),
		TestResults: results,
	}

	log.Infof("Storing aws sizing results for app %s: %+v", appName, awsSizingRunResults)
	if err := run.MetricsDB.WriteMetrics("sizing", awsSizingRunResults); err != nil {
		message := fmt.Sprintf("Unable to store aws sizing results for app %s: %s", appName, err.Error())
		log.Warningf(message)
		return errors.New(message)
	}
	log.Infof("AWS Sizing run finished")

	return nil
}

func NewAWSSizingSingleRun(
	id string,
	instanceType string,
	applicationConfig *models.ApplicationConfig,
	config *viper.Viper) (*AWSSizingSingleRun, error) {
	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	return &AWSSizingSingleRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			DeployerClient:    deployerClient,
			MetricsDB:         db.NewMetricsDB(config),
			ProfileLog:        log,
			Created:           time.Now(),
		},
		InstanceType: instanceType,
		ResultsChan:  make(chan SizeRunResults, 2),
	}, nil
}

func (run *AWSSizingSingleRun) GetJobDeploymentConfig() jobs.JobDeploymentConfig {
	nodes := []deployer.ClusterNode{
		deployer.ClusterNode{
			Id:           2,
			InstanceType: run.InstanceType,
		},
	}
	return jobs.JobDeploymentConfig{
		Nodes: nodes,
	}
}

func (run *AWSSizingSingleRun) GetSummary() jobs.JobSummary {
	return jobs.JobSummary{
		DeploymentId: run.DeploymentId,
		RunId:        run.Id,
		Status:       run.State,
		Create:       run.Created,
	}
}

func (run *AWSSizingSingleRun) Run(deploymentId string) error {
	log := run.ProfileLog.Logger
	run.DeploymentId = deploymentId
	appName := run.ApplicationConfig.Name
	results := SizeRunResults{
		RunId:        run.Id,
		InstanceType: run.InstanceType,
		AppName:      appName,
	}

	log.Infof("Reading calibration results for app %s", appName)
	metric, err := run.MetricsDB.GetMetric("calibration", appName, &models.CalibrationResults{})
	if err != nil {
		message := "Unable to get calibration results for app " + appName + ": " + err.Error()
		results.Error = message
		run.ResultsChan <- results
		return errors.New(message)
	}
	calibration := metric.(*models.CalibrationResults)

	if controller := run.ApplicationConfig.LoadTester.BenchmarkController; controller != nil {
		if err := replaceTargetingServiceAddress(controller, run.DeployerClient, run.DeploymentId, log); err != nil {
			message := fmt.Sprintf("Unable to replace service address [%v]: %s", run.ApplicationConfig.ServiceNames, err.Error())
			results.Error = message
			run.ResultsChan <- results
			return errors.New(message)
		}
	}

	startTime := time.Now()
	runResults, err := run.runApplicationLoadTest(run.Id, calibration.FinalResult.LoadIntensity)
	if err != nil {
		message := "Unable to run app " + appName + ": " + err.Error()
		results.Error = message
		run.ResultsChan <- results
		return errors.New(message)
	}

	// And return data results via ResultChan to AWSSizingRun, for it to report to the analyzer.
	results.QosValue = models.SLO{
		Metric: run.ApplicationConfig.SLO.Metric,
		Value:  float32(runResults[0].QosValue),
		Type:   run.ApplicationConfig.SLO.Type,
	}
	results.Duration = time.Since(startTime).String()

	if b, err := json.MarshalIndent(runResults, "", "  "); err != nil {
		log.Errorf("Unable to indent run results: " + err.Error())
	} else {
		log.Infof("Sizing results: %s", string(b))
	}
	run.ResultsChan <- results

	return nil
}

func (run *AWSSizingSingleRun) runApplicationLoadTest(
	stageId string,
	appIntensity float64) ([]*models.BenchmarkResult, error) {
	loadTester := run.ApplicationConfig.LoadTester
	run.ProfileLog.Logger.Infof("Starting app load test at intensity %.2f", appIntensity)
	if loadTester.BenchmarkController != nil {
		return run.runBenchmarkController(
			stageId,
			appIntensity,
			loadTester.BenchmarkController)
	} else if loadTester.SlowCookerController != nil {
		return run.runSlowCookerController(
			stageId,
			appIntensity,
			loadTester.SlowCookerController)
	}

	return nil, errors.New("No controller found in app load test request")
}

func (run *AWSSizingSingleRun) runBenchmarkController(
	stageId string,
	appIntensity float64,
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
			Intensity: int(appIntensity),
			QosValue:  qosValue,
		}
		results = append(results, result)
	}

	return results, nil
}

func (run *AWSSizingSingleRun) runSlowCookerController(
	stageId string,
	appIntensity float64,
	controller *models.SlowCookerController) ([]*models.BenchmarkResult, error) {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
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
			QosValue: float64(qosValue),
			Failures: runResult.Failures,
		}
		results = append(results, result)
	}

	return results, nil
}
