package clients

import (
	"errors"
	"fmt"
	"net/url"
	"path"

	"github.com/go-resty/resty"
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

type GetNextInstanceTypesRequest struct {
}

// GetNextInstanceTypes asks the analyzer if we should run more benchmark runs on different
// vm instance types or not. If the return array is empty, then the analyzer has found the
// optimal choice.
func (client *AnalyzerClient) GetNextInstanceTypes(appName string, results []interface{}) ([]string, error) {
	results := []string{}
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "get-next-instance-type", appName)

	request := GetNextInstanceTypesRequest{}
	response, err := resty.R().SetBody(request).Post(requestUrl)
	if err != nil {
		return results, errors.New("Unable to send analyzer request: " + err.Error())
	}

	if response.StatusCode() != 202 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	// TODO: Parse the response

	return results, nil
}
