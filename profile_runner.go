package main

import (
	"github.com/spf13/viper"
)

func RunProfile(config *viper.Viper, profile *Profile) error {
	//deployerClient := NewDeployerClient(config)
	//benchmarkAgentClient := NewBenchmarkAgentClient(config)

	// Rundown:
	// 1. Verify deployment has been deployed in Deployer
	// 2. Run through each stage:
	//    - Perform the necessary benchmark agent setup
	//    - Run the workload benchmark requests
	//    - Wait until finish then go to next stage

	return nil
}
