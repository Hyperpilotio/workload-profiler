package clients

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
	"github.com/hyperpilotio/workload-profiler/models"
)

type BenchmarkAgentResponse struct {
	Status string `json:"status"`
	Data   string `json:"data"`
	Error  bool   `json:"error"`
}

type BenchmarkAgentClient struct{}

func NewBenchmarkAgentClient() *BenchmarkAgentClient {
	return &BenchmarkAgentClient{}
}

func (client *BenchmarkAgentClient) CreateBenchmark(
	baseUrl string,
	benchmark *models.Benchmark,
	config *models.BenchmarkConfig,
	intensity int) error {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return fmt.Errorf("Unable to parse url %s: %s", baseUrl, err.Error())
	}

	benchmarkRequest := apis.Benchmark{
		Name:         config.Name,
		ResourceType: benchmark.ResourceType,
		Image:        benchmark.Image,
		Command: apis.Command{
			Args: config.Command.Args,
			Path: config.Command.Path,
		},
		Intensity:      intensity,
		DurationConfig: config.DurationConfig,
		CgroupConfig:   config.CgroupConfig,
		HostConfig:     config.HostConfig,
		NetConfig:      config.NetConfig,
		IOConfig:       config.IOConfig,
		Count:          1,
	}

	glog.V(1).Infof("Sending benchmark %s to benchmark agent %s", benchmark.Name, u)
	url := UrlBasePath(u) + path.Join(u.Path, "benchmarks")
	response, err := resty.R().SetBody(benchmarkRequest).Post(url)
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

func (client *BenchmarkAgentClient) DeleteBenchmark(baseUrl string, benchmarkName string) error {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return fmt.Errorf("Unable to parse url %s: %s", baseUrl, err.Error())
	}

	requestUrl := UrlBasePath(u) + path.Join(u.Path, "benchmarks", benchmarkName)

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
