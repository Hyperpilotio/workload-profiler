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
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/op/go-logging"
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
	intensity int,
	logger *logging.Logger) error {
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

	logger.Infof("Sending benchmark %s to benchmark agent %s", benchmark.Name, u)
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
			logger.Infof("Create benchmark responded with error: %v", pollResponse)
			return false, errors.New("Poll benchmark agent response returned error: " + pollResponse.Data)
		}

		if pollResponse.Status != "CREATING" {
			logger.Infof("Benchmark %s is now in %s state", benchmark.Name, pollResponse.Status)
			return true, nil
		}

		logger.Infof("Continue to wait for benchmark %s to start, last poll response: %v", benchmark.Name, response)

		return false, nil
	})

	return nil
}

func (client *BenchmarkAgentClient) DeleteBenchmark(baseUrl string, benchmarkName string, logger *logging.Logger) error {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return fmt.Errorf("Unable to parse url %s: %s", baseUrl, err.Error())
	}

	requestUrl := UrlBasePath(u) + path.Join(u.Path, "benchmarks", benchmarkName)

	logger.Infof("Deleting benchmark %s from benchmark agent", benchmarkName)
	for i := 0; i < 5; i++ {
		response, err := resty.R().Delete(requestUrl)
		if err != nil {
			if i == 5 {
				break
			}

			logger.Warningf("Deleting benchmark failed with error: %s, retrying...", err.Error())
			time.Sleep(3 * time.Second)
			continue
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

	return errors.New("Unable to delete benchmark after retries")
}
