package runners

import (
	"errors"
	"time"

	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type SizeRunResults struct {
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
			ProfileLog:        log,
			Created:           time.Now(),
		},
		AnalyzerClient: analyzerClient,
		JobManager:     jobManager,
		Config:         config,
	}, nil
}

func (run *AWSSizingRun) Run() error {
	// TODO: Configure initial aws instance type(s) to start the process
	instanceTypes := []string{"c4.xlarge"}
	for len(instanceTypes) > 0 {
		resultChans := []chan SizeRunResults{}
		results := []SizeRunResults{}
		for _, instanceType := range instanceTypes {
			newId := run.GetId() + instanceType
			singleRun, err := NewAWSSizingSingleRun(
				newId,
				instanceType,
				run.ApplicationConfig,
				run.Config,
				run.ProfileLog)
			if err != nil {
				// TODO: clean up
				return errors.New("Unable to create AWS single run: " + err.Error())
			}
			run.JobManager.AddJob(singleRun)
			resultChans = append(resultChans, singleRun.ResultsChan)
		}

		for _, resultChan := range results {
			results = append(results, <-resultChan)
		}

		instanceTypes, err = run.AnalyzerClient.GetNextInstanceTypes(run.ApplicationConfig.Name, results)
		if err != nil {
			return errors.New("Unable to get next instance types from analyzer: " + err.Error())
		}
		run.ProfileLog.Logger.Infof("Received next instance types to run sizing: %s", instanceTypes)
	}

	run.ProfileLog.Logger.Infof("AWS Sizing run finished")

	return nil
}

func NewAWSSizingSingleRun(
	id string,
	instanceType string,
	applicationConfig *models.ApplicationConfig,
	config *viper.Viper,
	log *log.FileLog) (*AWSSizingSingleRun, error) {
	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	return &AWSSizingSingleRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			DeployerClient:    deployerClient,
			ProfileLog:        log,
			Created:           time.Now(),
		},
		InstanceType: instanceType,
		ResultsChan:  make(chan SizeRunResults, 1),
	}, nil
}

func (run *AWSSizingSingleRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	// Run load test based on calibration intensity
	// And return data results via ResultChan to AWSSizingRun, for it to report to the analyzer.

	return nil
}
