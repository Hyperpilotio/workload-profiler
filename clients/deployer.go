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
		return false, err
	}

	if response.StatusCode() != 200 {
		// TODO: Log response code here
		return false, nil
	}

	return true, nil
}

func (client *DeployerClient) CreateDeployment(
	deploymentTemplate string,
	deployment *deployer.Deployment,
	loadTesterName string,
	log *logging.Logger) (*string, error) {
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
		Error bool   `json:"error"`
		Data  string `json:"data`
	}

	if err := json.Unmarshal(response.Body(), &createResponse); err != nil {
		return nil, errors.New("Unable to parse failed create deployment response: " + err.Error())
	}

	if createResponse.Error {
		return nil, errors.New("Unable to create deployment: " + createResponse.Data)
	}

	respDescs := strings.Split(createResponse.Data, "Creating deployment ")
	deploymentId := strings.Replace(respDescs[1], ".", "", -1)

	err = funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
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

	if err != nil {
		return nil, errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	// Poll to wait for the elb dns is svailable
	url, urlErr := client.GetServiceUrl(deploymentId, loadTesterName)
	if urlErr != nil {
		log.Warningf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	} else {
		err = funcs.LoopUntil(time.Minute*5, time.Second*10, func() (bool, error) {
			response, err := resty.R().Get(url)
			if err != nil {
				return false, nil
			}

			if response.StatusCode() != 200 {
				return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
			}

			log.Infof("%s url to be available", loadTesterName)
			return true, nil
		})

		if err != nil {
			log.Warningf("Unable to waiting for %s url to be available: %s", loadTesterName, err)
		}
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

func (client *DeployerClient) DeleteKubernetesObjects(deploymentId string, log *logging.Logger) error {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "deployments", deploymentId, "skipDeleteCluster")

	response, err := resty.R().Delete(requestUrl)
	if err != nil {
		return errors.New("Unable to send delete kubernetes objects request to deployer: " + err.Error())
	}

	if response.StatusCode() != 202 {
		return fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	return nil
}

func (client *DeployerClient) DeployKubernetesObjects(
	deploymentTemplate string,
	deploymentId string,
	deployment *deployer.Deployment,
	loadTesterName string,
	log *logging.Logger) error {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "templates", deploymentTemplate, "deployments", deploymentId, "skipDeployCluster")

	response, err := resty.R().SetBody(deployment).Put(requestUrl)
	if err != nil {
		return errors.New("Unable to send delete kubernetes objects request to deployer: " + err.Error())
	}

	if response.StatusCode() != 202 {
		return fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	// Poll to wait for the elb dns is svailable
	url, urlErr := client.GetServiceUrl(deploymentId, loadTesterName)
	if urlErr != nil {
		log.Warningf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	} else {
		err = funcs.LoopUntil(time.Minute*5, time.Second*10, func() (bool, error) {
			response, err := resty.R().Get(url)
			if err != nil {
				return false, nil
			}

			if response.StatusCode() != 200 {
				return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
			}

			log.Infof("%s url to be available", loadTesterName)
			return true, nil
		})

		if err != nil {
			log.Warningf("Unable to waiting for %s url to be available: %s", loadTesterName, err)
		}
	}

	return nil
}
