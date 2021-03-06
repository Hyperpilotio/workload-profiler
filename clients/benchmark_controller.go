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
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/hyperpilotio/workload-profiler/models"
	logging "github.com/op/go-logging"
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
	loadTesterName string,
	baseUrl string,
	stageId string,
	controller *models.BenchmarkController,
	slo models.SLO,
	logger *logging.Logger) (*BenchmarkControllerCalibrationResponse, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, "/api/calibrate")
	body := make(map[string]interface{})
	if controller.Initialize != nil {
		body["initialize"] = controller.Initialize
	}

	if controller.InitializeType != "" {
		body["initializeType"] = controller.InitializeType
	}

	body["loadTest"] = controller.Command
	body["slo"] = slo
	body["stageId"] = stageId
	body["parserUrl"] = controller.ParserUrl

	err = funcs.LoopUntil(time.Minute*5, time.Second*5, func() (bool, error) {
		logger.Infof("Sending calibration request to benchmark controller for stage: " + stageId)
		response, err := resty.R().SetBody(body).Post(u.String())
		if err != nil {
			logger.Warningf("Unable to send calibrate request to controller: " + err.Error())
			return false, nil
		}

		if response.StatusCode() >= 300 {
			return false, fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
		}

		return true, nil
	})

	if err != nil {
		return nil, errors.New("Unable to send calibration request to controller: " + err.Error())
	}

	results := &BenchmarkControllerCalibrationResponse{}

	//TODO: The time duration for looping should be parameterized later
	err = funcs.LoopUntil(time.Minute*240, time.Second*60, func() (bool, error) {
		response, err := resty.R().Get(u.String() + "/" + stageId)
		if err != nil {
			logger.Warningf("Unable to send calibrate results request to controller, retrying: " + err.Error())
			return false, nil
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), results); err != nil {
			return false, errors.New("Unable to parse response body: " + err.Error())
		}

		if results.Error != "" {
			logger.Infof("Calibration failed with error: " + results.Error)
			return false, errors.New("Calibration failed with error: " + results.Error)
		}

		if results.Status != "running" {
			logger.Infof("Load test finished with status: " + results.Status)
			logger.Infof("Load test finished response: %v", response)
			return true, nil
		}

		logger.Infof("Continue to wait for calibration results for %s, %s", loadTesterName, stageId)

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to get calibration results: " + err.Error())
	}

	return results, nil
}

func (client *BenchmarkControllerClient) RunBenchmark(
	loadTesterName string,
	baseUrl string,
	stageId string,
	intensity float64,
	controller *models.BenchmarkController,
	logger *logging.Logger) (*BenchmarkControllerBenchmarkResponse, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, "/api/benchmarks")
	body := make(map[string]interface{})
	if controller.Initialize != nil {
		body["initialize"] = controller.Initialize
	}

	if controller.InitializeType != "" {
		body["initializeType"] = controller.InitializeType
	}

	loadTesterCommand := controller.Command
	args := loadTesterCommand.Args
	// TODO: We assume one intensity args for now
	intensityArg := loadTesterCommand.IntensityArgs[0]
	args = append(args, intensityArg.Arg)
	// TODO: Intensity arguments might be differnet types, we assume it's all int at the moment
	args = append(args, strconv.Itoa(int(intensity)))
	command := models.Command{
		Image: loadTesterCommand.Image,
		Path:  loadTesterCommand.Path,
		Args:  args,
	}

	body["loadTest"] = command
	body["intensity"] = intensity
	body["stageId"] = stageId
	body["parserUrl"] = controller.ParserUrl

	err = funcs.LoopUntil(time.Minute*5, time.Second*5, func() (bool, error) {
		logger.Infof("Sending benchmark request to benchmark controller for stage: " + stageId)
		response, err := resty.R().SetBody(body).Post(u.String())
		if err != nil {
			logger.Warningf("Unable to send benchmark request to controller, retrying: " + err.Error())
			return false, nil
		}

		if response.StatusCode() >= 300 {
			return false, fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
		}

		return true, nil
	})

	if err != nil {
		return nil, errors.New("Unable to send benchmark request to controller: " + err.Error())
	}

	results := &BenchmarkControllerBenchmarkResponse{}

	err = funcs.LoopUntil(time.Minute*360, time.Second*30, func() (bool, error) {
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
			logger.Infof("Benchmark failed with error: " + results.Error)
			return false, errors.New("Benchmark failed with error: " + results.Error)
		}

		if results.Status != "running" {
			logger.Infof("Load test finished with status: %s, response: %v", results.Status, response)
			return true, nil
		}

		logger.Infof("Continue to wait for benchmark results for %s, %s", loadTesterName, stageId)

		return false, nil
	})

	if err != nil {
		return nil, errors.New("Unable to get benchmark results: " + err.Error())
	}

	return results, nil
}
