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
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/hyperpilotio/workload-profiler/models"
)

type SlowCookerClient struct{}

type SlowCookerCalibrateResult struct {
	Concurrency int    `json:"concurrency"`
	LatencyMs   int64  `json:"latencyMs"`
	Failures    uint64 `json:"failures"`
}

type SlowCookerCalibrateResponse struct {
	Id               string                       `json:"id"`
	Results          []*SlowCookerCalibrateResult `json:"results"`
	FinalResult      *SlowCookerCalibrateResult   `json:"finalResult"`
	FinalConcurrency int                          `json:"finalConcurrency"`
	Error            string                       `json:"error"`
	State            string                       `json:"state"`
}

type SlowCookerSLO struct {
	LatencyMs  int `json:"latencyMs"`
	Percentile int `json:"percentile"`
}

type SlowCookerCalibrateRequest struct {
	Calibrate *models.SlowCookerCalibrate `json:"calibrate"`
	SLO       *SlowCookerSLO              `json:"slo"`
	AppLoad   *models.SlowCookerAppLoad   `json:"appLoad"`
	RunId     string                      `json:"runId"`
}

func (client *SlowCookerClient) RunCalibration(
	runId string,
	baseUrl string,
	slo models.SLO,
	controller *models.SlowCookerController) (*SlowCookerCalibrateResponse, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, "/slowcooker/calibrate/"+runId)

	percentile, err := strconv.Atoi(slo.Metric)
	if err != nil {
		return nil, errors.New("Unable to parse slo Metric to percentile value")
	}

	request := &SlowCookerCalibrateRequest{
		SLO: &SlowCookerSLO{
			LatencyMs:  int(slo.Value),
			Percentile: percentile,
		},
		Calibrate: controller.Calibrate,
		AppLoad:   controller.AppLoad,
	}

	glog.Infof("Sending calibration request to slow cooker for stage: " + runId)
	response, err := resty.R().SetBody(request).Post(u.String())
	if err != nil {
		return nil, errors.New("Unable to send calibrate request to slow cooker: " + err.Error())
	}

	if response.StatusCode() >= 300 {
		return nil, fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
	}

	results := &SlowCookerCalibrateResponse{}

	err = funcs.LoopUntil(time.Minute*90, time.Second*30, func() (bool, error) {
		response, err := resty.R().Get(u.String())
		if err != nil {
			return false, errors.New("Unable to get calibration status from slow cooker: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), results); err != nil {
			return false, errors.New("Unable to parse response body: " + err.Error())
		}

		if results.Error != "" {
			glog.Infof("Slow cooker calibration failed with error: " + results.Error)
			return false, errors.New("Slow cooker calibration failed with error: " + results.Error)
		}

		if results.State != "running" {
			glog.V(1).Infof("Calibration finished with status: %s, response: %v", results.State, response)
			return true, nil
		}

		glog.V(1).Infof("Continue to wait for slow cooker calibration results, last poll response: %v", response)

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to get caliration results from slow cooker: " + err.Error())
	}

	return results, nil
}

type SlowCookerBenchmarkResult struct {
	Failures      uint64 `json:"failures"`
	PercentileMin int64  `json:"percentileMin"`
	Percentile50  int64  `json:"percentile50"`
	Percentile95  int64  `json:"percentile95"`
	Percentile99  int64  `json:"percentile99"`
	PercentileMax int64  `json:"percentileMax"`
}

type SlowCookerBenchmarkResponse struct {
	Id      string                      `json:"id"`
	Error   string                      `json:"error"`
	State   string                      `json:"state"`
	Results []SlowCookerBenchmarkResult `json:"results"`
}

type SlowCookerBenchmarkRequest struct {
	LoadTime         string                    `json:"loadTime"`
	RunsPerIntensity int                       `json:"runsPerIntensity"`
	AppLoad          *models.SlowCookerAppLoad `json:"appLoad"`
	RunId            string                    `json:"runId"`
}

func (client *SlowCookerClient) RunBenchmark(
	baseUrl string,
	runId string,
	appIntensity float64,
	slo *models.SLO,
	controller *models.SlowCookerController) (*SlowCookerBenchmarkResponse, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, "/slowcooker/benchmark/"+runId)

	controller.AppLoad.Qps = int(slo.Value)

	request := &SlowCookerBenchmarkRequest{
		RunId:            runId,
		AppLoad:          controller.AppLoad,
		RunsPerIntensity: controller.Calibrate.RunsPerIntensity,
		LoadTime:         controller.LoadTime,
	}

	glog.Infof("Sending benchmark request to slow cooker for stage: " + runId)
	response, err := resty.R().SetBody(request).Post(u.String())
	if err != nil {
		return nil, errors.New("Unable to send benchmark request to slow cooker: " + err.Error())
	}

	if response.StatusCode() >= 300 {
		return nil, fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
	}

	results := &SlowCookerBenchmarkResponse{}

	err = funcs.LoopUntil(time.Minute*90, time.Second*30, func() (bool, error) {
		response, err := resty.R().Get(u.String())
		if err != nil {
			return false, errors.New("Unable to get benchmark status from slow cooker: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), results); err != nil {
			return false, errors.New("Unable to parse response body: " + err.Error())
		}

		if results.Error != "" {
			glog.Infof("Slow cooker benchmark failed with error: " + results.Error)
			return false, errors.New("Slow cooker benchmark failed with error: " + results.Error)
		}

		if results.State != "running" {
			glog.V(1).Infof("Calibration finished with status: %s, response: %v", results.State, response)
			return true, nil
		}

		glog.V(1).Infof("Continue to wait for slow cooker benchmark results, last poll response: %v", response)

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to get benchmark results from slow cooker: " + err.Error())
	}

	return results, nil
}
