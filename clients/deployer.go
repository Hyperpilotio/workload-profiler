package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/go-resty/resty"
	"github.com/spf13/viper"
)

type ServiceMapping struct {
	NodeId    int    `json:"nodeId"`
	NodeName  string `json:"nodeName"`
	PublicUrl string `json:"publicUrl"`
}

type ServiceMappingResponse struct {
	Data  map[string]ServiceMapping
	Error bool
}

type DeployerClient struct {
	ServiceMapping   *ServiceMappingResponse
	ServiceUrls      map[string]string
	ServiceAddresses map[string]*ServiceAddress
	Url              *url.URL
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

func (client *DeployerClient) getServiceMappings(deployment string) (*ServiceMappingResponse, error) {
	if client.ServiceMapping != nil {
		return client.ServiceMapping, nil
	}

	requestUrl := UrlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "deployments", deployment, "services")

	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var mappingResponse ServiceMappingResponse
	if err := json.Unmarshal(response.Body(), &mappingResponse); err != nil {
		return nil, errors.New("Unable to parse service mapping response: " + err.Error())
	}

	client.ServiceMapping = &mappingResponse
	return client.ServiceMapping, nil
}

// GetColocatedServiceUrl finds the service's url that's running on the same node where
// colocatedService is running. We assume there is only one such service with the passed in prefix.
// We don't specify exact service names because services that has multiple copies will be named differently
// but sharing the same prefix (e.g: benchmark-agent, benchmark-agent-2, etc.).
func (client *DeployerClient) GetColocatedServiceUrl(deployment string, colocatedService string, servicePrefix string) (string, error) {
	mappings, err := client.getServiceMappings(deployment)
	if err != nil {
		return "", errors.New("Unable to get service mappings for deployment " + deployment + ": " + err.Error())
	}

	mapping, ok := mappings.Data[colocatedService]
	if !ok {
		return "", errors.New("Unable to find colocated service in mappings: " + colocatedService)
	}

	nodeId := mapping.NodeId
	for serviceName, mapping := range mappings.Data {
		if strings.HasPrefix(serviceName, servicePrefix) && mapping.NodeId == nodeId {
			return "http://" + mapping.PublicUrl, nil
		}
	}

	return "", fmt.Errorf("Unable to find service with prefix %s located at node id %d", servicePrefix, nodeId)
}

func (client *DeployerClient) GetServiceUrl(deployment string, service string) (string, error) {
	if url, ok := client.ServiceUrls[service]; ok {
		return url, nil
	}

	requestUrl := UrlBasePath(client.Url) +
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

// ServiceAddress GetServiceUrl return this object
type ServiceAddress struct {
	Host string `bson:"host,omitempty" json:"host,omitempty"`
	Port int64  `bson:"port,omitempty" json:"port,omitempty"`
}

// GetServiceAddress return the address object of service container
func (client *DeployerClient) GetServiceAddress(deployment string, service string) (*ServiceAddress, error) {
	if address, ok := client.ServiceAddresses[service]; ok {
		return address, nil
	}

	requestUrl := UrlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "deployments", deployment, "services", service, "address")

	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var address ServiceAddress
	if err = json.Unmarshal(response.Body(), &address); err != nil {
		return nil, err
	}

	return &address, nil
}

func (client *DeployerClient) IsDeploymentReady(deployment string) (bool, error) {
	requestUrl := UrlBasePath(client.Url) + path.Join(client.Url.Path, "v1", "deployments", deployment)

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
