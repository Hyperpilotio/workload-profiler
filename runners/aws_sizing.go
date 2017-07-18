package runners

import (
	"errors"
	"time"

	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type AWSSizingRun struct {
	ProfileRun
}

func NewAWSSizingRun(applicationConfig *models.ApplicationConfig, config *viper.Viper) (*AWSSizingRun, error) {
	id, err := generateId("awssizing")
	if err != nil {
		return nil, errors.New("Unable to generate aws sizing Id: " + err.Error())
	}

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	return &AWSSizingRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			DeployerClient:    deployerClient,
			ProfileLog:        log,
			Created:           time.Now(),
		},
	}, nil
}

func (run *AWSSizingRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	return nil
}
