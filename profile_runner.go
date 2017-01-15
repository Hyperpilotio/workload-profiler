package main

import (
	"errors"

	"github.com/spf13/viper"
)

type ProfileResults struct {
	StageResults []StageResult
}

type StageResult struct {
}

func setupStage(stage *Stage) error {
	//    - Perform the necessary benchmark agent setup

	return nil
}

func runStageBenchmark(stage *Stage) (*StageResult, error) {
	//    - Run the workload benchmark requests
	//    - Wait until finish then go to next stage

	results := &StageResult{}

	return results, nil
}

func RunProfile(config *viper.Viper, profile *Profile) (*ProfileResults, error) {
	//deployerClient := NewDeployerClient(config)
	//benchmarkAgentClient := NewBenchmarkAgentClient(config)

	// TODO: Verify deployment has been deployed in Deployer
	results := &ProfileResults{}

	for _, stage := range profile.Stages {
		if err := setupStage(&stage); err != nil {
			return nil, errors.New("Unable to setup stage: " + err.Error())
		}

		if _, err := runStageBenchmark(&stage); err != nil {
			return nil, errors.New("Unable to run stage benchmark: " + err.Error())
		}
	}

	return results, nil
}
