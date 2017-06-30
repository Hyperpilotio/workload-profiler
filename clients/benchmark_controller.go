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

type BenchmarkControllerClient struct{}

type BenchmarkControllerCalibrationResponse struct {
	Status  string `json:"status"`
	Error   string `json:"error"`
	Results struct {
		RunResults []struct {
			Results       map[string]interface{} `json:"results"`
			IntensityArgs map[string]interface{} `json:"intensityArgs"`
		} `json:"runResults"`
		FinalResults struct {
			IntensityArgs map[string]interface{} `json:"intensityArgs"`
			Qos           float64                `json:"qos"`
		} `json:"finalResults"`
	} `json:"results"`
}

type BenchmarkControllerBenchmarkResponse struct {
	Status  string `json:"status"`
	Error   string `json:"error"`
	Results []struct {
		Results   map[string]interface{} `json:"results"`
		Intensity int                    `json"intensity"`
	} `json:"results"`
}

func (client *BenchmarkControllerClient) RunCalibration(
	baseUrl string,
	stageId string,
	controller *models.BenchmarkController,
	slo models.SLO) (*BenchmarkControllerCalibrationResponse, error) {
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

	results := &BenchmarkControllerCalibrationResponse{}

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
	controller *models.BenchmarkController) (*BenchmarkControllerBenchmarkResponse, error) {
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
	command := models.Command{
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

	results := &BenchmarkControllerBenchmarkResponse{}

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