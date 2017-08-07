package clients

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
	"github.com/hyperpilotio/go-utils/funcs"
	logging "github.com/op/go-logging"
	"github.com/spf13/viper"
)

type AnalyzerClient struct {
	Url *url.URL
}

func NewAnalyzerClient(config *viper.Viper) (*AnalyzerClient, error) {
	if u, err := url.Parse(config.GetString("analyzerUrl")); err != nil {
		return nil, errors.New("Unable to parse analyzer url: " + err.Error())
	} else {
		return &AnalyzerClient{Url: u}, nil
	}
}

type InstanceResult struct {
	InstanceType string  `json:"instanceType"`
	QosValue     float32 `json:"qosValue"`
}

type GetNextInstanceTypesRequest struct {
	AppName string           `json:"appName"`
	Data    []InstanceResult `json:"data"`
}

type GetNextInstanceTypesResponse struct {
	Status        string   `json:"status"`
	InstanceTypes []string `json:"data"`
	Error         string   `json:"error"`
}

// GetNextInstanceTypes asks the analyzer if we should run more benchmark runs on different
// vm instance types or not. If the return array is empty, then the analyzer has found the
// optimal choice.
func (client *AnalyzerClient) GetNextInstanceTypes(
	runId string,
	appName string,
	results map[string]float32,
	logger *logging.Logger) ([]string, error) {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "api", "apps", runId, "suggest-instance-types")

	instanceResults := []InstanceResult{}
	for instanceType, result := range results {
		instanceResults = append(instanceResults, InstanceResult{InstanceType: instanceType, QosValue: result})
	}

	request := GetNextInstanceTypesRequest{
		AppName: appName,
		Data:    instanceResults,
	}

	err := funcs.LoopUntil(time.Minute*5, time.Second*5, func() (bool, error) {
		logger.Infof("Sending get next instance types request to analyzer %s: %s", requestUrl, request)
		response, err := resty.R().SetBody(request).Post(requestUrl)
		if err != nil {
			logger.Warningf("Unable to send analyzer request, retrying: " + err.Error())
			return false, nil
		}

		if response.StatusCode() >= 300 {
			return false, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
		}

		return true, nil
	})

	if err != nil {
		return nil, errors.New("Unable to send instance types request to analyzer: " + err.Error())
	}

	var nextInstanceResponse GetNextInstanceTypesResponse
	err = funcs.LoopUntil(time.Minute*10, time.Second*10, func() (bool, error) {
		requestUrl := UrlBasePath(client.Url) + path.Join(
			client.Url.Path, "api", "apps", runId, "get-optimizer-status")

		logger.Infof("Sending analyzer poll request to %s", requestUrl)
		pollResponse, err := resty.R().Get(requestUrl)
		if err != nil {
			logger.Infof("Retrying after error when polling analyzer: %s", err.Error())
			return false, nil
		}

		logger.Infof("Polled analyzer response: %s, status: %d", pollResponse.Body(), pollResponse.StatusCode())

		if pollResponse.StatusCode() >= 300 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(pollResponse.StatusCode()))
		}

		if err := json.Unmarshal(pollResponse.Body(), &nextInstanceResponse); err != nil {
			return false, errors.New("Unable to parse analyzer response: " + err.Error())
		}

		logger.Infof("Found analyzer job status: %s", nextInstanceResponse.Status)

		switch strings.ToLower(nextInstanceResponse.Status) {
		case "running":
			return false, nil
		case "done":
			return true, nil
		case "":
			return false, nil
		case "bad_request":
			return false, errors.New("Bad request sent to analyzer: " + nextInstanceResponse.Error)
		case "server_error":
			return false, errors.New("Internal server error in analyzer: " + nextInstanceResponse.Error)
		default:
			return false, errors.New("Unexpected analyzer status: " + nextInstanceResponse.Status)
		}
	})

	if err != nil {
		return nil, errors.New("Unable to wait for analyzer results: " + err.Error())
	}

	return nextInstanceResponse.InstanceTypes, nil
}
