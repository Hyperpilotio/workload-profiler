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
	InstanceTypes []string `json:"instanceTypes"`
}

// GetNextInstanceTypes asks the analyzer if we should run more benchmark runs on different
// vm instance types or not. If the return array is empty, then the analyzer has found the
// optimal choice.
func (client *AnalyzerClient) GetNextInstanceTypes(
	appName string,
	results map[string]float32,
	logger *logging.Logger) ([]string, error) {
	instanceTypes := []string{}
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "get-next-instance-type", appName)

	instanceResults := []InstanceResult{}
	for instanceType, result := range results {
		instanceResults = append(instanceResults, InstanceResult{InstanceType: instanceType, QosValue: result})
	}

	request := GetNextInstanceTypesRequest{
		AppName: appName,
		Data:    instanceResults,
	}

	logger.Infof("Sending get next instance types request to analyzer %+v: ", request)
	response, err := resty.R().SetBody(request).Post(requestUrl)
	if err != nil {
		return instanceTypes, errors.New("Unable to send analyzer request: " + err.Error())
	}
	logger.Infof("Received analyzer response: %+v", response)

	if response.StatusCode() != 202 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var nextInstanceResponse GetNextInstanceTypesResponse
	funcs.LoopUntil(time.Minute*10, time.Second*10, func() (bool, error) {
		response, err := resty.R().Get(requestUrl)
		if err != nil {
			return false, nil
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), &nextInstanceResponse); err != nil {
			return false, errors.New("Unable to parse analyzer response: " + err.Error())
		}

		if nextInstanceResponse.Status != "running" {
			return false, nil
		}

		return true, nil
	})

	return nextInstanceResponse.InstanceTypes, nil
}
