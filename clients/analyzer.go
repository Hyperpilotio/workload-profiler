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

	body, err := json.Marshal(&request)
	if err != nil {
		return nil, errors.New("Unable to marshal request: " + err.Error())
	}

	logger.Infof("Sending get next instance types request to analyzer %s: %s", requestUrl, body)
	response, err := resty.R().SetBody(body).Post(requestUrl)
	if err != nil {
		return nil, errors.New("Unable to send analyzer request: " + err.Error())
	}

	if response.StatusCode() >= 300 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var nextInstanceResponse GetNextInstanceTypesResponse
	funcs.LoopUntil(time.Minute*10, time.Second*10, func() (bool, error) {
		requestUrl := UrlBasePath(client.Url) + path.Join(
			client.Url.Path, "api", "apps", runId, "get-optimizer-status")

		response, err := resty.R().Get(requestUrl)
		if err != nil {
			return false, nil
		}

		logger.Infof("Polled analyzer response: %+v, status: %d", response, response.StatusCode())

		if response.StatusCode() >= 300 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), &nextInstanceResponse); err != nil {
			return false, errors.New("Unable to parse analyzer response: " + err.Error())
		}

		switch strings.ToLower(nextInstanceResponse.Status) {
		case "running":
			return false, nil
		case "done":
			return true, nil
		default:
			return false, errors.New("Unexpected analyzer status: " + nextInstanceResponse.Status)
		}
	})

	return nextInstanceResponse.InstanceTypes, nil
}
