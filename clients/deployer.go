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
	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/op/go-logging"
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
	ServiceMapping *ServiceMappingResponse
	ServiceUrls    map[string]string
	Url            *url.URL
}

type DeployerResponse struct {
	Error bool   `json:"error"`
	Data  string `json:"data`
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
// colocatedService is running. We assume there is only one service with the passed in prefix.
// We don't specify exact service names because services that has multiple copies will be named differently
// but sharing the same prefix (e.g: benchmark-agent, benchmark-agent-2, etc.).
func (client *DeployerClient) GetColocatedServiceUrl(deployment string, colocatedService string, servicePrefix string) (string, error) {
	mappings, err := client.getServiceMappings(deployment)
	if err != nil {
		return "", errors.New("Unable to get service mappings: " + err.Error())
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

func (client *DeployerClient) IsDeploymentReady(deployment string) (bool, error) {
	requestUrl := UrlBasePath(client.Url) + path.Join(client.Url.Path, "v1", "deployments", deployment)

	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return false, errors.New("Unable to send deployment is reday request to deployer: " + err.Error())
	}

	if response.StatusCode() != 200 {
		// TODO: Log response code here
		errorResponse := &DeployerResponse{}
		if err := json.Unmarshal(response.Body(), &errorResponse); err != nil {
			return false, errors.New("Unable to parse failed deployment response: " + err.Error())
		}
		return false, errors.New(errorResponse.Data)
	}

	return true, nil
}

func (client *DeployerClient) CreateDeployment(
	deploymentTemplate string,
	deployment *deployer.Deployment,
	loadTesterName string,
	log *logging.Logger) (*string, error) {
	if deploymentTemplate == "" {
		return nil, errors.New("Empty deployment template found")
	}

	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "templates", deploymentTemplate, "deployments")

	response, err := resty.R().SetBody(deployment).Post(requestUrl)
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != 202 {
		return nil, fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var createResponse struct {
		Error        bool   `json:"error"`
		Data         string `json:"data`
		DeploymentId string `json:"deploymentId`
	}

	if err := json.Unmarshal(response.Body(), &createResponse); err != nil {
		return nil, errors.New("Unable to parse failed create deployment response: " + err.Error())
	}

	log.Debugf("Received deployer response: %+v", createResponse)

	if createResponse.Error {
		return nil, errors.New("Unable to create deployment: " + createResponse.Data)
	}

	deploymentId := createResponse.DeploymentId
	if deploymentId == "" {
		return nil, errors.New("Unable to get deployment id")
	}

	log.Infof("Waiting for deployment to be available...")
	if err := client.waitUntilDeploymentStateAvailable(deploymentId, log); err != nil {
		return nil, errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	log.Infof("Waiting for service urls to be available...")
	if err := client.waitUntilServiceUrlAvailable(deploymentId, loadTesterName, log); err != nil {
		log.Warningf("Unable to waiting for %s url to be available: %s", loadTesterName, err)
	}

	return &deploymentId, nil
}

func (client *DeployerClient) DeleteDeployment(deploymentId string, log *logging.Logger) error {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "deployments", deploymentId)

	response, err := resty.R().Delete(requestUrl)
	if err != nil {
		return errors.New("Unable to send delete deployment request to deployer: " + err.Error())
	}

	if response.StatusCode() != 202 {
		return fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	err = funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
		deploymentStateUrl := UrlBasePath(client.Url) +
			path.Join(client.Url.Path, "v1", "deployments", deploymentId, "state")

		response, err := resty.R().Get(deploymentStateUrl)
		if err != nil {
			return false, errors.New("Unable to send deployment state request to deployer: " + err.Error())
		}

		switch response.StatusCode() {
		case 404:
			log.Infof("Delete %s deployment successfully, deployment state is not found", deploymentId)
			return true, nil
		case 200:
			return false, nil
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("Unable to waiting for %s deployment to be delete: %s", deploymentId, err.Error())
	}

	return nil
}

func (client *DeployerClient) ResetTemplateDeployment(
	deploymentTemplate string, deploymentId string, log *logging.Logger) error {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "templates", deploymentTemplate, "deployments", deploymentId, "reset")

	response, err := resty.R().Put(requestUrl)
	if err != nil {
		return errors.New("Unable to send reset template deployment request to deployer: " + err.Error())
	}

	if response.StatusCode() != 200 {
		errorResponse := &DeployerResponse{}
		if err := json.Unmarshal(response.Body(), &errorResponse); err != nil {
			return errors.New("Unable to parse failed deployment response: " + err.Error())
		}
		log.Errorf("Unable to reset template deployment: %s", errorResponse.Data)
		return errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
	}

	if err := client.waitUntilDeploymentStateAvailable(deploymentId, log); err != nil {
		return errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	return nil
}

func (client *DeployerClient) DeployExtensions(
	deploymentTemplate string,
	deploymentId string,
	deployment *deployer.Deployment,
	loadTesterName string,
	log *logging.Logger) error {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "templates", deploymentTemplate, "deployments", deploymentId, "deploy")

	response, err := resty.R().SetBody(deployment).Put(requestUrl)
	if err != nil {
		return errors.New("Unable to send deploy extensions kubernetes objects request to deployer: " + err.Error())
	}

	if response.StatusCode() != 200 {
		errorResponse := &DeployerResponse{}
		if err := json.Unmarshal(response.Body(), &errorResponse); err != nil {
			return errors.New("Unable to parse failed deployment response: " + err.Error())
		}
		log.Errorf("Unable to deploy extensions kubernetes objects: %s", errorResponse.Data)
		return errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
	}

	if err := client.waitUntilDeploymentStateAvailable(deploymentId, log); err != nil {
		return errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	if err := client.waitUntilServiceUrlAvailable(deploymentId, loadTesterName, log); err != nil {
		log.Warningf("Unable to waiting for %s url to be available: %s", loadTesterName, err)
	}

	return nil
}

func (client *DeployerClient) waitUntilDeploymentStateAvailable(deploymentId string, log *logging.Logger) error {
	return funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
		deploymentStateUrl := UrlBasePath(client.Url) +
			path.Join(client.Url.Path, "v1", "deployments", deploymentId, "state")

		response, err := resty.R().Get(deploymentStateUrl)
		if err != nil {
			return false, errors.New("Unable to send deployment state request to deployer: " + err.Error())
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		deploymentState := string(response.Body())
		switch deploymentState {
		case "Available":
			log.Infof("%s state is available", deploymentId)
			return true, nil
		case "Failed":
			return false, fmt.Errorf("%s state is Failed", deploymentId)
		}

		return false, nil
	})
}

func (client *DeployerClient) waitUntilServiceUrlAvailable(
	deploymentId string, serviceName string, log *logging.Logger) error {
	url, err := client.GetServiceUrl(deploymentId, serviceName)
	if err != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", serviceName, err.Error())
	}

	return funcs.LoopUntil(time.Minute*5, time.Second*10, func() (bool, error) {
		response, err := resty.R().Get(url)
		if err != nil {
			return false, nil
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		log.Infof("%s url is now available", serviceName)
		return true, nil
	})
}
