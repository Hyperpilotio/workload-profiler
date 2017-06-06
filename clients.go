package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"

	"github.com/go-resty/resty"
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/model"
	"github.com/spf13/viper"
)

type ApiResponse struct {
	Data  string `json:"data"`
	Error bool   `json:"error"`
}

type DeployerClient struct {
	Url *url.URL
}

func urlBasePath(u *url.URL) string {
	return u.Scheme + "://" + u.Host + "/"
}

func NewDeployerClient(config *viper.Viper) (*DeployerClient, error) {
	if u, err := url.Parse(config.GetString("deployerUrl")); err != nil {
		return nil, errors.New("Unable to parse deployer url: " + err.Error())
	} else {
		return &DeployerClient{Url: u}, nil
	}
}

func (client *DeployerClient) GetServiceUrl(deployment string, service string) (string, error) {
	requestUrl := urlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "deployments", deployment, "services", service, "url")

	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return "", err
	}

	if response.StatusCode() != 200 {
		return "", fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	return "http://" + response.String(), nil
}

func (client *DeployerClient) IsDeploymentReady(deployment string) (bool, error) {
	requestUrl := urlBasePath(client.Url) + path.Join(client.Url.Path, "v1", "deployments", deployment)

	response, err := resty.R().Get(requestUrl)
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
	Url *url.URL
}

func NewBenchmarkAgentClient(urlString string) (*BenchmarkAgentClient, error) {
	if u, err := url.Parse(urlString); err != nil {
		return nil, errors.New("Unable to parse deployer url: " + err.Error())
	} else {
		return &BenchmarkAgentClient{Url: u}, nil
	}
}

func (client *BenchmarkAgentClient) CreateBenchmark(benchmark *model.Benchmark) error {
	benchmarkJson, marshalErr := json.Marshal(benchmark)
	if marshalErr != nil {
		return errors.New("Unable to marshal benchmark to JSON: " + marshalErr.Error())
	}

	response, err := resty.R().
		SetBody(string(benchmarkJson)).
		Post(urlBasePath(client.Url) + path.Join(client.Url.Path, "benchmarks"))

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

func (client *BenchmarkAgentClient) DeleteBenchmark(benchmarkName string) error {
	requestUrl := urlBasePath(client.Url) + path.Join(client.Url.Path, "benchmarks", benchmarkName)

	response, err := resty.R().Delete(requestUrl)
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

func (client *BenchmarkAgentClient) UpdateBenchmarkResources(benchmarkName string, resources *model.Resources) error {
	resourcesJson, marshalErr := json.Marshal(resources)
	if marshalErr != nil {
		return errors.New("Unable to marshal resources to JSON: " + marshalErr.Error())
	}

	response, err := resty.R().
		SetBody(string(resourcesJson)).
		Put(urlBasePath(client.Url) + path.Join(client.Url.Path, "benchmarks", benchmarkName, "resources"))

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
