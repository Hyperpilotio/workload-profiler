package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty"
	"github.com/golang/glog"
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/spf13/viper"
)

type BenchmarkAgentResponse struct {
	Status string `json:"status"`
	Data   string `json:"data"`
	Error  bool   `json:"error"`
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

func (client *DeployerClient) CreateDeployment(deploymentTemplate string, deployment *deployer.Deployment) (*string, error) {
	requestUrl := urlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "templates", deploymentTemplate, "deployments")

	response, err := resty.R().SetBody(deployment).Post(requestUrl)
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != 202 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var createResponse struct {
		Error bool   `json:"error"`
		Data  string `json:"data`
	}

	if err := json.Unmarshal(response.Body(), &createResponse); err != nil {
		return nil, errors.New("Unable to parse failed create deployment response: " + err.Error())
	}

	respDescs := strings.Split(createResponse.Data, "Creating deployment ")
	deploymentId := strings.Replace(respDescs[1], ".", "", -1)

	err = funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
		deploymentStateUrl := urlBasePath(client.Url) +
			path.Join(client.Url.Path, "v1", "deployments", deploymentId, "state")

		response, err := resty.R().Get(deploymentStateUrl)
		if err != nil {
			return false, errors.New("Unable to send deployment state request to deployer: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if string(response.Body()) == "Available" {
			glog.Infof("%s state is available", deploymentId)
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	return &deploymentId, nil
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
		return nil, errors.New("Unable to parse benchmark agent url: " + err.Error())
	} else {
		return &BenchmarkAgentClient{Url: u}, nil
	}
}

func (client *BenchmarkAgentClient) CreateBenchmark(benchmark *apis.Benchmark) error {
	benchmarkJson, marshalErr := json.Marshal(benchmark)
	if marshalErr != nil {
		return errors.New("Unable to marshal benchmark to JSON: " + marshalErr.Error())
	}

	glog.V(1).Infof("Sending benchmark %s to benchmark agent %s", benchmark.Name, client.Url)
	url := urlBasePath(client.Url) + path.Join(client.Url.Path, "benchmarks")
	response, err := resty.R().
		SetBody(string(benchmarkJson)).
		Post(url)

	if err != nil {
		return err
	}

	if response.StatusCode() != 202 {
		apiResponse := BenchmarkAgentResponse{}
		if err := json.Unmarshal(response.Body(), &apiResponse); err != nil {
			return errors.New("Unable to parse failed create benchmark response: " + err.Error())
		} else {
			return errors.New(apiResponse.Data)
		}
	}

	return nil

	// Poll to wait for the benchmark to be ready from the agent
	err = funcs.LoopUntil(time.Minute*15, time.Second*10, func() (bool, error) {
		response, err := resty.R().Get(url + "/" + benchmark.Name)
		if err != nil {
			return false, errors.New("Unable to poll benchmark create status: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code when polling benchmark status: " +
				strconv.Itoa(response.StatusCode()))
		}

		pollResponse := BenchmarkAgentResponse{}
		if err := json.Unmarshal(response.Body(), pollResponse); err != nil {
			return false, errors.New("Unable to parse response body when polling benchmark status: " + err.Error())
		}

		if pollResponse.Error {
			glog.Infof("Create benchmark responded with error: %v", pollResponse)
			return false, errors.New("Poll benchmark agent response returned error: " + pollResponse.Data)
		}

		if pollResponse.Status != "CREATING" {
			glog.Infof("Benchmark %s is now in %s state", benchmark.Name, pollResponse.Status)
			return true, nil
		}

		glog.V(1).Infof("Continue to wait for benchmark %s to start, last poll response: %v", benchmark.Name, response)

		return false, nil
	})

	return nil
}

func (client *BenchmarkAgentClient) DeleteBenchmark(benchmarkName string) error {
	requestUrl := urlBasePath(client.Url) + path.Join(client.Url.Path, "benchmarks", benchmarkName)

	glog.V(1).Infof("Deleting benchmark %s from benchmark agent", benchmarkName)
	response, err := resty.R().Delete(requestUrl)
	if err != nil {
		return err
	}

	if response.StatusCode() != 202 {
		apiResponse := BenchmarkAgentResponse{}
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

type RunBenchmarkResponse struct {
	Status  string `json:"status"`
	Error   string `json:"error"`
	Results []struct {
		Results   map[string]interface{} `json:"results"`
		Intensity int                    `json"intensity"`
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

	//TODO: The time duration for looping should be parameterized later
	err = funcs.LoopUntil(time.Minute*60, time.Second*15, func() (bool, error) {
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

func (client *BenchmarkControllerClient) RunBenchmark(
	baseUrl string,
	stageId string,
	intensity float64,
	controller *BenchmarkController) (*RunBenchmarkResponse, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, "/api/benchmarks")
	body := make(map[string]interface{})
	if controller.Initialize != nil {
		body["initialize"] = controller.Initialize
	}

	loadTesterCommand := controller.Command
	args := loadTesterCommand.Args
	// TODO: We assume one intensity args for now
	intensityArg := loadTesterCommand.IntensityArgs[0]
	args = append(args, intensityArg.Arg)
	// TODO: Intensity arguments might be differnet types, we assume it's all int at the moment
	args = append(args, strconv.Itoa(int(intensity)))
	command := Command{
		Path: loadTesterCommand.Path,
		Args: args,
	}
	body["loadTest"] = command
	body["intensity"] = intensity
	body["stageId"] = stageId

	glog.Infof("Sending benchmark request to benchmark controller for stage: " + stageId)
	response, err := resty.R().SetBody(body).Post(u.String())
	if err != nil {
		return nil, errors.New("Unable to send calibrate request to controller: " + err.Error())
	}

	if response.StatusCode() >= 300 {
		return nil, fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
	}

	results := &RunBenchmarkResponse{}

	err = funcs.LoopUntil(time.Minute*90, time.Second*30, func() (bool, error) {
		response, err := resty.R().Get(u.String() + "/" + stageId)
		if err != nil {
			return false, errors.New("Unable to send benchmark results request to controller: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), results); err != nil {
			return false, errors.New("Unable to parse response body: " + err.Error())
		}

		if results.Error != "" {
			glog.Infof("Benchmark failed with error: " + results.Error)
			return false, errors.New("Benchmark failed with error: " + results.Error)
		}

		if results.Status != "running" {
			glog.V(1).Infof("Load test finished with status: %s, response: %v", results.Status, response)
			return true, nil
		}

		glog.V(1).Infof("Continue to wait for benchmark results, last poll response: %v", response)

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to get benchmark results: " + err.Error())
	}

	return results, nil
}
