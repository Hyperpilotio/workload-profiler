package runners

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/resource"
)

type instanceRunState int

// Possible all instance run states
const (
	RUNNING  = 0
	FAILED   = 1
	FINISHED = 2
)

var instanceRunStates = map[instanceRunState]string{
	RUNNING:  "Running",
	FAILED:   "Failed",
	FINISHED: "Finished",
}

func GetStateString(state instanceRunState) string {
	if stateString, ok := instanceRunStates[state]; ok {
		return stateString
	}

	return ""
}

func ParseStateString(state string) instanceRunState {
	for instanceState, stateString := range instanceRunStates {
		if stateString == state {
			return instanceState
		}
	}

	return -1
}

type SizeRunResults struct {
	InstanceType string
	RunId        string
	Duration     string
	AppName      string
	QosValue     models.SLO
}

type InstanceResults struct {
	State    string  `bson:"state" json:"state"`
	QosValue float64 `bson:"qosValue" json:"qosValue"`
}

type AllInstanceRunResults struct {
	RunId       string                      `bson:"runId" json:"runId"`
	Duration    string                      `bson:"duration" json:"duration"`
	AppName     string                      `bson:"appName" json:"appName"`
	TestResults map[string]*InstanceResults `bson:"testResult" json:"testResult"`
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

type AWSSizingAllInstancesRun struct {
	AWSSizingRun

	NodeTypeConfig      *models.AWSRegionNodeTypeConfig
	PreviousGenerations []string
}

type AWSSizingInstancesRun struct {
	AWSSizingRun

	Instances []string
}

// AWSSizingSingleRun represents a single benchmark run for a particular
// AWS instance type.
type AWSSizingSingleRun struct {
	ProfileRun

	InstanceType string
	Calibration  *models.CalibrationResults
	ResultsChan  chan *jobs.JobResults
}

func NewAWSSizingAllInstancesRun(
	jobManager *jobs.JobManager,
	applicationConfig *models.ApplicationConfig,
	config *viper.Viper,
	nodeTypeConfig *models.AWSRegionNodeTypeConfig,
	previousGenerations []string,
	allInstances bool,
	skipUnreserveOnFailure bool) (*AWSSizingAllInstancesRun, error) {
	id, err := generateId("awssizingall")
	if err != nil {
		return nil, errors.New("Unable to generate id: " + err.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	analyzerClient, err := clients.NewAnalyzerClient(config)
	if err != nil {
		return nil, errors.New("Unable to create analyzer client: " + err.Error())
	}

	return &AWSSizingAllInstancesRun{
		AWSSizingRun: AWSSizingRun{
			ProfileRun: ProfileRun{
				Id:                     id,
				ApplicationConfig:      applicationConfig,
				DeployerClient:         deployerClient,
				MetricsDB:              db.NewMetricsDB(config),
				ProfileLog:             log,
				Created:                time.Now(),
				SkipUnreserveOnFailure: skipUnreserveOnFailure,
				DirectJob:              true,
			},

			AnalyzerClient: analyzerClient,
			JobManager:     jobManager,
			Config:         config,
		},
		NodeTypeConfig:      nodeTypeConfig,
		PreviousGenerations: previousGenerations,
	}, nil
}

func instanceTypeDbName(instanceType string) string {
	return strings.Replace(instanceType, ".", "-", -1)
}

func (run *AWSSizingRun) SetFailed(error string) {}

func (run *AWSSizingRun) GetResults() <-chan *jobs.JobResults {
	return nil
}

func (run *AWSSizingAllInstancesRun) Run(deploymentId string) error {
	log := run.ProfileLog.Logger
	appName := run.ApplicationConfig.Name

	log.Infof("Reading calibration results for app %s", appName)
	metric, err := run.MetricsDB.GetMetric("calibration", appName, &models.CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + appName + ": " + err.Error())
	}

	calibration := metric.(*models.CalibrationResults)

	log.Infof("Running through all instances for this sizing run " + run.GetId())

	metadataSvc := ec2metadata.New(session.New())
	identity, err := metadataSvc.GetInstanceIdentityDocument()
	if err != nil {
		return errors.New("Unable to get identity document from ec2 metadata: " + err.Error())
	}

	region := identity.Region
	availabilityZone := identity.AvailabilityZone
	log.Infof("Detected region %s and az %s", region, availabilityZone)
	supportedInstanceTypes, err := run.DeployerClient.GetSupportedAWSInstances(region, availabilityZone)
	if err != nil {
		return errors.New("Unable to fetch initial instance types: " + err.Error())
	}

	var allInstanceRunResults *AllInstanceRunResults
	results, err := run.MetricsDB.GetMetric("allInstance", run.ApplicationConfig.Name, &AllInstanceRunResults{})
	if err != nil {
		allInstanceRunResults = &AllInstanceRunResults{
			RunId:       run.GetId(),
			AppName:     run.ApplicationConfig.Name,
			TestResults: make(map[string]*InstanceResults),
		}
	} else {
		allInstanceRunResults = results.(*AllInstanceRunResults)
	}

	log.Infof("Supported %s EC2 instance types: %+v", availabilityZone, supportedInstanceTypes)
	jobs := map[string]*AWSSizingSingleRun{}
	for _, instanceType := range supportedInstanceTypes {
		if !run.isInstanceTypeSupported(instanceType) {
			continue
		}

		existingResults, ok := allInstanceRunResults.TestResults[instanceTypeDbName(instanceType)]
		if ok && existingResults.State == GetStateString(FINISHED) {
			log.Infof("Skipping to run instance %s as we already have finished results", instanceType)
			continue
		}

		instanceResults := &InstanceResults{
			State: GetStateString(RUNNING),
		}

		newId := run.GetId() + "-" + instanceType
		newApplicationConfig := &models.ApplicationConfig{}
		deepCopy(run.ApplicationConfig, newApplicationConfig)
		singleRun, err := NewAWSSizingSingleRun(
			newId,
			instanceType,
			calibration,
			newApplicationConfig,
			run.Config,
			run.IsSkipUnreserveOnFailure())
		if err != nil {
			log.Warningf("Unable to create AWS single run: " + err.Error())
			continue
		}

		allInstanceRunResults.TestResults[instanceTypeDbName(instanceType)] = instanceResults
		run.JobManager.AddJob(singleRun)
		jobs[instanceType] = singleRun
	}

	startTime := time.Now()
	for instanceType, job := range jobs {
		result := <-job.GetResults()
		instanceResults := allInstanceRunResults.TestResults[instanceTypeDbName(instanceType)]
		if result.Error != "" {
			log.Warningf(
				"Failed to run aws single size run with id %s: %s",
				job.GetId(),
				result.Error)
			instanceResults.State = GetStateString(FAILED)
			instanceResults.QosValue = 0.0
		} else {
			instanceResults.State = GetStateString(FINISHED)
			sizeRunResults := result.Data.(SizeRunResults)
			qosValue := sizeRunResults.QosValue.Value
			log.Infof("Received sizing run value %0.2f with instance type %s", qosValue, instanceType)
			instanceResults.QosValue = qosValue
			allInstanceRunResults.Duration = time.Since(startTime).String()

			// Store each successful run metric
			log.Infof("Storing sizing all instance results for app %s", allInstanceRunResults.AppName)
			if err := run.MetricsDB.UpsertMetrics("allInstance", allInstanceRunResults.AppName, allInstanceRunResults); err != nil {
				message := "Unable to store sizing results for app " + allInstanceRunResults.AppName + ": " + err.Error()
				log.Warningf(message)
			}
		}
	}

	return nil
}

func NewAWSSizingInstancesRun(
	jobManager *jobs.JobManager,
	applicationConfig *models.ApplicationConfig,
	config *viper.Viper,
	instances []string,
	skipUnreserveOnFailure bool) (*AWSSizingInstancesRun, error) {
	if len(instances) == 0 {
		return nil, errors.New("Empty instances found")
	}

	id, err := generateId("awssizinginstances")
	if err != nil {
		return nil, errors.New("Unable to generate id: " + err.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	analyzerClient, err := clients.NewAnalyzerClient(config)
	if err != nil {
		return nil, errors.New("Unable to create analyzer client: " + err.Error())
	}

	return &AWSSizingInstancesRun{
		AWSSizingRun: AWSSizingRun{
			ProfileRun: ProfileRun{
				Id:                     id,
				ApplicationConfig:      applicationConfig,
				DeployerClient:         deployerClient,
				MetricsDB:              db.NewMetricsDB(config),
				ProfileLog:             log,
				Created:                time.Now(),
				SkipUnreserveOnFailure: skipUnreserveOnFailure,
				DirectJob:              true,
			},
			AnalyzerClient: analyzerClient,
			JobManager:     jobManager,
			Config:         config,
		},
		Instances: instances,
	}, nil
}

func (run *AWSSizingInstancesRun) Run(deploymentId string) error {
	log := run.ProfileLog.Logger
	appName := run.ApplicationConfig.Name

	log.Infof("Reading calibration results for app %s", appName)
	metric, err := run.MetricsDB.GetMetric("calibration", appName, &models.CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + appName + ": " + err.Error())
	}

	calibration := metric.(*models.CalibrationResults)
	results := make(map[string]float64)
	log.Infof("Instance types to run: %+v", run.Instances)

	results = make(map[string]float64)
	jobs := map[string]*AWSSizingSingleRun{}
	for _, instanceType := range run.Instances {
		newId := run.GetId() + "-" + instanceType
		newApplicationConfig := &models.ApplicationConfig{}
		deepCopy(run.ApplicationConfig, newApplicationConfig)
		singleRun, err := NewAWSSizingSingleRun(
			newId,
			instanceType,
			calibration,
			newApplicationConfig,
			run.Config,
			run.IsSkipUnreserveOnFailure())
		if err != nil {
			return errors.New("Unable to create AWS single run: " + err.Error())
		}

		run.JobManager.AddJob(singleRun)
		jobs[instanceType] = singleRun
	}

	for instanceType, job := range jobs {
		result := <-job.GetResults()
		if result.Error != "" {
			log.Warningf(
				"Failed to run aws single size run with id %s: %s",
				job.GetId(),
				result.Error)
			results[instanceType] = 0.0
		} else {
			sizeRunResults := result.Data.(SizeRunResults)
			qosValue := sizeRunResults.QosValue.Value
			log.Infof("Received sizing run value %0.2f with instance type %s", qosValue, instanceType)
			results[instanceType] = qosValue
		}
	}

	log.Infof("AWS Sizing instances run finished for %s, results: %+v", run.Id, results)

	return nil
}

func NewAWSSizingRun(
	jobManager *jobs.JobManager,
	applicationConfig *models.ApplicationConfig,
	config *viper.Viper,
	skipUnreserveOnFailure bool) (*AWSSizingRun, error) {
	id, err := generateId("awssizing")
	if err != nil {
		return nil, errors.New("Unable to generate id: " + err.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	analyzerClient, err := clients.NewAnalyzerClient(config)
	if err != nil {
		return nil, errors.New("Unable to create analyzer client: " + err.Error())
	}

	return &AWSSizingRun{
		ProfileRun: ProfileRun{
			Id:                     id,
			ApplicationConfig:      applicationConfig,
			DeployerClient:         deployerClient,
			MetricsDB:              db.NewMetricsDB(config),
			ProfileLog:             log,
			Created:                time.Now(),
			SkipUnreserveOnFailure: skipUnreserveOnFailure,
			DirectJob:              true,
		},
		AnalyzerClient: analyzerClient,
		JobManager:     jobManager,
		Config:         config,
	}, nil
}

func (run *AWSSizingAllInstancesRun) isInstanceTypeSupported(instanceType string) bool {
	log := run.ProfileLog.Logger

	for _, previousInstanceTypeName := range run.PreviousGenerations {
		if instanceType == previousInstanceTypeName {
			log.Infof("Skipping previous generation %s", instanceType)
			return false
		}
	}

	// Filter lower resource
	serviceName := run.ApplicationConfig.ServiceNames[0]
	var memoryRequirement int64
	var cpuRequirement int64
	for _, task := range run.ApplicationConfig.TaskDefinitions {
		kubernetesTask := &deployer.KubernetesTask{}
		if err := deepCopy(task.TaskDefinition, kubernetesTask); err != nil {
			log.Warningf("Unable to convert to kubernetesTask: " + err.Error())
			return false
		}

		if kubernetesTask.Family == serviceName {
			for _, containerSpec := range kubernetesTask.Deployment.Spec.Template.Spec.Containers {
				cpuRequirement += containerSpec.Resources.Requests.Cpu().MilliValue()
				memoryRequirement += containerSpec.Resources.Requests.Memory().MilliValue()
			}
		}
	}

	memoryConfig := ""
	cpuConfig := ""
	for _, node := range run.NodeTypeConfig.Data {
		if node.Name == instanceType && node.MemoryConfig.Size.Unit == "GiB" {
			if node.MemoryConfig.Size.Unit == "GiB" {
				memoryConfig = fmt.Sprintf("%0.0fMi", node.MemoryConfig.Size.Value*1024)
			} else {
				log.Warningf("Unsupport memory config format %s: ", node.MemoryConfig.Size.Unit)
				return false
			}
			cpuConfig = fmt.Sprintf("%dm", node.CpuConfig.VCPU*1024)
		}
	}

	log.Infof("%s memoryConfig: %s", instanceType, memoryConfig)
	log.Infof("%s cpuConfig: %s", instanceType, cpuConfig)

	maxMemory, err := resource.ParseQuantity(memoryConfig)
	if err != nil {
		log.Warningf("Unable to parse memory quantity: " + err.Error())
		return false
	}

	maxCpu, err := resource.ParseQuantity(cpuConfig)
	if err != nil {
		log.Warningf("Unable to parse cpu quantity: " + err.Error())
		return false
	}

	if memoryRequirement > maxMemory.MilliValue() {
		log.Infof("Skip sizing run on instance type %s: Low memory", instanceType)
		return false
	}
	if cpuRequirement > maxCpu.MilliValue() {
		log.Infof("Skip sizing run on instance type %s: Low Cpu", instanceType)
		return false
	}

	return true
}

func (run *AWSSizingRun) Run(deploymentId string) error {
	log := run.ProfileLog.Logger
	appName := run.ApplicationConfig.Name

	log.Infof("Reading calibration results for app %s", appName)
	metric, err := run.MetricsDB.GetMetric("calibration", appName, &models.CalibrationResults{})
	if err != nil {
		return errors.New("Unable to get calibration results for app " + appName + ": " + err.Error())
	}

	calibration := metric.(*models.CalibrationResults)
	results := make(map[string]float64)
	instanceTypes, err := run.AnalyzerClient.GetNextInstanceTypes(run.Id, appName, results, log)
	if err != nil {
		return errors.New("Unable to fetch initial instance types: " + err.Error())
	}

	log.Infof("Received initial instance types: %+v", instanceTypes)
	for len(instanceTypes) > 0 {
		results = make(map[string]float64)
		jobs := map[string]*AWSSizingSingleRun{}
		for _, instanceType := range instanceTypes {
			newId := run.GetId() + "-" + instanceType
			newApplicationConfig := &models.ApplicationConfig{}
			deepCopy(run.ApplicationConfig, newApplicationConfig)
			singleRun, err := NewAWSSizingSingleRun(
				newId,
				instanceType,
				calibration,
				newApplicationConfig,
				run.Config,
				run.IsSkipUnreserveOnFailure())
			if err != nil {
				return errors.New("Unable to create AWS single run: " + err.Error())
			}

			run.JobManager.AddJob(singleRun)
			jobs[instanceType] = singleRun
		}

		for instanceType, job := range jobs {
			result := <-job.GetResults()
			if result.Error != "" {
				log.Warningf(
					"Failed to run aws single size run with id %s: %s",
					job.GetId(),
					result.Error)
				if !clients.IsAWSDeploymentError(result.Error) {
					// TODO: Report analyzer that we have a critical error and cannot move on
					log.Warningf("Stopping aws sizing run as we hit a non-aws error")
					return errors.New(result.Error)
				}

				results[instanceType] = 0.0
			} else {
				sizeRunResults := result.Data.(SizeRunResults)
				qosValue := sizeRunResults.QosValue.Value
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

	log.Infof("AWS Sizing run finished for " + run.Id)

	return nil
}

func NewAWSSizingSingleRun(
	id string,
	instanceType string,
	calibration *models.CalibrationResults,
	applicationConfig *models.ApplicationConfig,
	config *viper.Viper,
	SkipUnreserveOnFailure bool) (*AWSSizingSingleRun, error) {
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
			Id:                     id,
			ApplicationConfig:      applicationConfig,
			DeployerClient:         deployerClient,
			MetricsDB:              db.NewMetricsDB(config),
			ProfileLog:             log,
			Created:                time.Now(),
			SkipUnreserveOnFailure: SkipUnreserveOnFailure,
			DirectJob:              false,
		},
		InstanceType: instanceType,
		Calibration:  calibration,
		ResultsChan:  make(chan *jobs.JobResults, 2),
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

func (run *AWSSizingSingleRun) GetResults() <-chan *jobs.JobResults {
	return run.ResultsChan
}

func (run *AWSSizingSingleRun) SetFailed(error string) {
	run.ResultsChan <- &jobs.JobResults{
		Error: error,
	}
}

func (run *AWSSizingSingleRun) Run(deploymentId string) error {
	log := run.ProfileLog.Logger
	run.DeploymentId = deploymentId
	appName := run.ApplicationConfig.Name
	sizeResults := SizeRunResults{
		RunId:        run.Id,
		InstanceType: run.InstanceType,
		AppName:      appName,
	}

	if controller := run.ApplicationConfig.LoadTester.BenchmarkController; controller != nil {
		if err := replaceTargetingServiceAddress(controller, run.DeployerClient, run.DeploymentId, log); err != nil {
			message := fmt.Sprintf("Unable to replace service address [%v]: %s", run.ApplicationConfig.ServiceNames, err.Error())
			run.SetFailed(message)
			return errors.New(message)
		}
	}

	startTime := time.Now()
	runResults, err := run.runApplicationLoadTest(run.Id, run.Calibration.FinalResult.LoadIntensity)
	if err != nil {
		message := "Unable to run app " + appName + ": " + err.Error()
		run.SetFailed(message)
		return errors.New(message)
	}

	// Report the average of the run results
	var total float64
	var min float64 = math.MaxFloat64
	var max float64 = math.SmallestNonzeroFloat64
	for _, result := range runResults {
		total += result.QosValue
		if result.QosValue < min {
			min = result.QosValue
		}
		if result.QosValue > max {
			max = result.QosValue
		}
	}

	result := total / float64(len(runResults))

	diff := (max - min) / min
	log.Infof("Run results min: %f, max: %f, avg: %f, max to min diff ratio: %f", min, max, result, diff)

	// And return data results via ResultChan to AWSSizingRun, for it to report to the analyzer.
	sizeResults.QosValue = models.SLO{
		Metric: run.ApplicationConfig.SLO.Metric,
		Value:  result,
		Type:   run.ApplicationConfig.SLO.Type,
	}
	sizeResults.Duration = time.Since(startTime).String()

	if b, err := json.MarshalIndent(runResults, "", "  "); err != nil {
		log.Errorf("Unable to indent run results: " + err.Error())
	} else {
		log.Infof("Sizing results: %s", string(b))
	}

	results := &jobs.JobResults{
		Data: sizeResults,
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
		url, stageId, appIntensity, controller.Calibrate.InitialConcurrency, controller, run.ProfileLog.Logger, true)
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
