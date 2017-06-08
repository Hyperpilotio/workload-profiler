package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/go-resty/resty"
	"github.com/golang/glog"
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/spf13/viper"
)

type ApiResponse struct {
	Data  string `json:"data"`
	Error bool   `json:"error"`
}

type DeployerClient struct {
	// Cached service urls
	ServiceUrls map[string]string
	Url         *url.URL
}

type BenchmarkControllerClient struct{}

func urlBasePath(u *url.URL) string {
	return u.Scheme + "://" + u.Host + "/"
}

func NewDeployerClient(config *viper.Viper) (*DeployerClient, error) {
	if u, err := url.Parse(config.GetString("deployerUrl")); err != nil {
		return nil, errors.New("Unable to parse deployer url: " + err.Error())
	} else {
		return &DeployerClient{
			Url:         u,
			ServiceUrls: make(map[string]string),
		}, nil
	}
}

func (client *DeployerClient) GetServiceUrl(deployment string, service string) (string, error) {
	if url, ok := client.ServiceUrls[service]; ok {
		return url, nil
	}

	requestUrl := urlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "deployments", deployment, "services", service, "url")

	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return "", err
	}

	if response.StatusCode() != 200 {
		return "", fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	url := "http://" + response.String()
	client.ServiceUrls[service] = url

	return url, nil
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

func (client *BenchmarkAgentClient) CreateBenchmark(benchmark *Benchmark) error {
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

type RunCalibrationResponse struct {
	Status  string `json:"status"`
	Error   string `json:"error"`
	Results struct {
		RunResults []struct {
			Results       map[string]interface{} `json:"results"`
			IntensityArgs map[string]interface{} `json:"intensityArgs"`
		} `json:"runResults"`
		FinalIntensityArgs map[string]interface{} `json:"finalIntensityArgs"`
	} `json:"results"`
}

func (client *BenchmarkControllerClient) RunCalibration(baseUrl string, stageId string, controller *BenchmarkController, slo SLO) (*RunCalibrationResponse, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, "/api/calibrate")
	body := make(map[string]interface{})
	if controller.Initialize != nil {
		body["initialize"] = controller.Initialize
	}

	body["loadTest"] = controller.Command
	body["slo"] = slo
	body["stageId"] = stageId

	glog.Infof("Sending calibration request to benchmark controller for stage: " + stageId)
	response, err := resty.R().SetBody(body).Post(u.String())
	if err != nil {
		return nil, errors.New("Unable to send calibrate request to controller: " + err.Error())
	}

	if response.StatusCode() >= 300 {
		return nil, fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
	}

	results := &RunCalibrationResponse{}

	err = funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
		response, err := resty.R().Get(u.String() + "/" + stageId)
		if err != nil {
			return false, errors.New("Unable to send calibrate results request to controller: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), results); err != nil {
			return false, errors.New("Unable to parse response body: " + err.Error())
		}

		if results.Error != "" {
			glog.Infof("Calibration failed with error: " + results.Error)
			return false, errors.New("Calibration failed with error: " + results.Error)
		}

		if results.Status != "running" {
			glog.Infof("Load test finished with status: " + results.Status)
			glog.Infof("Load test finished response: %v", response)
			return true, nil
		}

		glog.Infof("Continue to wait for calibration results, last poll response: %v", response)

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to get calibration results: " + err.Error())
	}

	return results, nil
}
