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

	Config     *viper.Viper
	JobManager *jobs.JobManager
}

// AWSSizingSingleRun represents a single benchmark run for a particular
// AWS instance type.
type AWSSizingSingleRun struct {
	ProfileRun

	InstanceType string
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

	return &AWSSizingRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			ProfileLog:        log,
			Created:           time.Now(),
		},
		JobManager: jobManager,
		Config:     config,
	}, nil
}

func (run *AWSSizingRun) Run() error {
	// Beginning first create a initial AWSSizingSingleRun based on a default aws instance type (c4.xlarge),
	// and submit to the job manager to run.
	// Then it need to be able to wait until the job finishes, and report the results to analyzer.
	// The analyzer should return a set of recommendation AWS instance types, and then each one should be
	// pushed to the job manager to run.

	// TODO: Configure initial aws instance type(s) to start the process
	instanceTypes := []string{"c4.xlarge"}
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
	}

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
	}, nil
}

func (run *AWSSizingSingleRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	// Run load test based on calibration intensity
	// And return data results to AWSSizingRun, for it to report to the analyzer.

	return nil
}
