package main

import (
	"encoding/json"
	"errors"
	"path"

	"github.com/go-resty/resty"
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
	"github.com/spf13/viper"
)

type ApiResponse struct {
	Data  string `json:"data"`
	Error bool   `json:"error"`
}

type DeployerClient struct {
	Url string
}

func NewDeployerClient(config *viper.Viper) *DeployerClient {
	return &DeployerClient{
		Url: config.GetString("deployerUrl"),
	}
}

func (client *DeployerClient) GetContainerUrl(deployment string, container string) (string, error) {
	response, err := resty.R().
		Get(path.Join(client.Url, "v1", "deployments", deployment, "containers", container, "url"))

	if err != nil {
		return "", err
	}

	if response.StatusCode() != 200 {
		return "", errors.New("Invalid status code returned: " + string(response.StatusCode()))
	}

	return response.String(), nil
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

func (client *BenchmarkAgentClient) CreateBenchmark(benchmark *apis.Benchmark) error {
	benchmarkJson, marshalErr := json.Marshal(benchmark)
	if marshalErr != nil {
		return errors.New("Unable to marshal benchmark to JSON: " + marshalErr.Error())
	}

	response, err := resty.R().
		SetBody(string(benchmarkJson)).
		Post(path.Join(client.Url, "benchmarks"))

	if err != nil {
		return err
	}

	if response.StatusCode() != 202 {
		apiResponse := ApiResponse{}
		if err := json.Unmarshal(response.Body(), &apiResponse); err != nil {
			return errors.New("Unable to parse failed api response: " + err.Error())
		} else {
			return errors.New(apiResponse.Data)
		}
	}

	return nil
}

func (client *BenchmarkAgentClient) UpdateBenchmarkResources(benchmarkName string, resources *apis.Resources) error {
	resourcesJson, marshalErr := json.Marshal(resources)
	if marshalErr != nil {
		return errors.New("Unable to marshal resources to JSON: " + marshalErr.Error())
	}

	response, err := resty.R().
		SetBody(string(resourcesJson)).
		Put(path.Join(client.Url, "benchmarks", benchmarkName, "resources"))

	if err != nil {
		return err
	}

	if response.StatusCode() != 202 {
		apiResponse := ApiResponse{}
		if err := json.Unmarshal(response.Body(), &apiResponse); err != nil {
			return errors.New("Unable to parse failed api response: " + err.Error())
		} else {
			return errors.New(apiResponse.Data)
		}
	}

	return nil
}
