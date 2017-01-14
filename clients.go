package main

import (
	"path"

	"github.com/go-resty/resty"
	"github.com/spf13/viper"
)

type DeployerClient struct {
	Url string
}

func NewDeployerClient(config *viper.Viper) *DeployerClient {
	return &DeployerClient{
		Url: config.GetString("deployerUrl"),
	}
}

func (client *DeployerClient) IsDeploymentReady(deployment string) (bool, error) {
	response, err := resty.R().
		Get(path.Join(client.Url, "v1", "deployments", deployment))

	if err != nil {
		return false, err
	}

	if response.StatusCode() != 200 {
		// TODO: Log response code here
		return false, nil
	}

	return true, nil
}

type BenchmarkAgentClient struct {
	Url string
}

func NewBenchmarkAgentClient(config *viper.Viper) *BenchmarkAgentClient {
	return &BenchmarkAgentClient{
		Url: config.GetString("benchmarkAgentUrl"),
	}
}
