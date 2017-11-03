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
	logging "github.com/op/go-logging"
	"github.com/spf13/viper"
)

const (
	ErrAWSError = "Unable to run ec2"
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

type DeployerResponse struct {
	Error bool   `json:"error"`
	Data  string `json:"data`
}

type DeploymentCache struct {
	ServiceMapping   *ServiceMappingResponse
	ServiceUrls      map[string]string
	ServiceAddresses map[string]*ServiceAddress
}

type DeployerClient struct {
	Cache map[string]*DeploymentCache

	Url *url.URL
}

func IsAWSDeploymentError(error string) bool {
	return strings.Contains(error, ErrAWSError)
}

func (client *DeployerClient) getCache(deployment string) *DeploymentCache {
	cache, ok := client.Cache[deployment]
	if ok {
		return cache
	}

	cache = &DeploymentCache{
		ServiceUrls:      make(map[string]string),
		ServiceAddresses: make(map[string]*ServiceAddress),
	}

	client.Cache[deployment] = cache

	return cache
}

func NewDeployerClient(config *viper.Viper) (*DeployerClient, error) {
	u, err := url.Parse(config.GetString("deployerUrl"))
	if err != nil {
		return nil, errors.New("Unable to parse deployer url: " + err.Error())
	}

	return &DeployerClient{
		Url:   u,
		Cache: make(map[string]*DeploymentCache),
	}, nil
}

func (client *DeployerClient) getServiceMappings(deployment string) (*ServiceMappingResponse, error) {
	cache := client.getCache(deployment)
	if cache.ServiceMapping != nil {
		return cache.ServiceMapping, nil
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

	cache.ServiceMapping = &mappingResponse
	return cache.ServiceMapping, nil
}

// GetColocatedServiceUrls finds the service's urls that's running on the same nodes where
// colocatedService is running.
// We don't specify exact service names because services that has multiple copies will be named differently
// but sharing the same prefix (e.g: benchmark-agent, benchmark-agent-2, etc.).
func (client *DeployerClient) GetColocatedServiceUrls(
	deployment string,
	colocatedServicePrefix string,
	targetServicePrefix string,
	log *logging.Logger) ([]string, error) {
	mappings, err := client.getServiceMappings(deployment)
	if err != nil {
		return nil, errors.New("Unable to get service mappings for deployment " + deployment + ": " + err.Error())
	}

	log.Infof("Service mappings in colocated service urls: %+v", mappings)

	urls := []string{}
	for colocatedServiceName, colocatedMapping := range mappings.Data {
		if strings.HasPrefix(colocatedServiceName, colocatedServicePrefix) {
			for targetServiceName, targetMapping := range mappings.Data {
				if strings.HasPrefix(targetServiceName, targetServicePrefix) &&
					targetMapping.NodeId == colocatedMapping.NodeId &&
					targetMapping.PublicUrl != "" {
					urls = append(urls, "http://"+targetMapping.PublicUrl)
				}
			}
		}
	}

	if len(urls) == 0 {
		return nil, errors.New("Unable to find any service colocated")
	}

	return urls, nil
}

func (client *DeployerClient) GetServiceUrls(deployment string, servicePrefix, log *logging.Logger) ([]string, error) {
	mappings, err := client.getServiceMappings(deployment)
	if err != nil {
		return nil, errors.New("Unable to get service mappings for deployment " + deployment + ": " + err.Error())
	}

	urls = []string{}
	for serviceName, mapping := range mappings.Data {
		if strings.HasPrefix(serviceName, servicePrefix) && targetMapping.PublicUrl != "" {
			urls = append(urls, "http://"+mapping.PublicUrl)
		}
	}

	return urls
}

func (client *DeployerClient) GetServiceUrl(deployment string, service string, log *logging.Logger) (string, error) {
	cache := client.getCache(deployment)
	if url, ok := cache.ServiceUrls[service]; ok {
		return url, nil
	}

	requestUrl := UrlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "deployments", deployment, "services", service, "url")

	log.Infof("Requesting service %s url with deployment %s to deployer %s", service, deployment, requestUrl)
	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return "", err
	}

	if response.StatusCode() != 200 {
		return "", fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	url := "http://" + response.String()
	cache.ServiceUrls[service] = url

	log.Infof("Service url for service %s and deployment %s is %s", service, deployment, url)

	return url, nil
}

// ServiceAddress GetServiceUrl return this object
type ServiceAddress struct {
	Host string `bson:"host,omitempty" json:"host,omitempty"`
	Port int64  `bson:"port,omitempty" json:"port,omitempty"`
}

// GetServiceAddress return the address object of service container
func (client *DeployerClient) GetServiceAddress(deployment string, service string, log *logging.Logger) (*ServiceAddress, error) {
	cache := client.getCache(deployment)
	if address, ok := cache.ServiceAddresses[service]; ok {
		return address, nil
	}

	requestUrl := UrlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "deployments", deployment, "services", service, "address")

	log.Infof("Getting service address from deployer for deployment %s, service %s with url %s", deployment, service, requestUrl)
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

	log.Infof("Service address returned from deployer for deployment %s, service %s: %+v", deployment, service, address)
	cache.ServiceAddresses[service] = &address

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

type DeploymentStateResponse struct {
	State string `json:"state"`
	Data  string `json:"data"`
}

func (client *DeployerClient) waitUntilDeploymentStateAvailable(deploymentId string, log *logging.Logger) error {
	var stateResponse DeploymentStateResponse
	return funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
		deploymentStateUrl := UrlBasePath(client.Url) +
			path.Join(client.Url.Path, "v1", "deployments", deploymentId, "state")

		response, err := resty.R().Get(deploymentStateUrl)
		if err != nil {
			log.Infof("Unable to send deployment state request to deployer, retrying: " + err.Error())
			return false, nil
		}

		if response.StatusCode() != 200 {
			return false, errors.New("Unexpected response code: " + strconv.Itoa(response.StatusCode()))
		}

		if err := json.Unmarshal(response.Body(), &stateResponse); err != nil {
			return false, errors.New("Unable to serialize deployment state response: " + err.Error())
		}

		switch stateResponse.State {
		case "Available":
			log.Infof("Deployment %s is now available", deploymentId)
			return true, nil
		case "Failed":
			log.Infof("Deployment %s failed with reason: %s", deploymentId, stateResponse.Data)
			return false, fmt.Errorf("Deployment failed: ", stateResponse.Data)
		}

		return false, nil
	})
}

func (client *DeployerClient) ResetTemplateDeployment(
	deploymentTemplate string,
	deploymentId string,
	log *logging.Logger) error {
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

func (client *DeployerClient) DeleteDeployment(deploymentId string, log *logging.Logger) error {
	delete(client.Cache, deploymentId)

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

func (client *DeployerClient) CreateDeployment(
	deployment *deployer.Deployment,
	loadTesterName string,
	log *logging.Logger) (string, error) {
	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "deployments")

	log.Infof("Sending deployment to deployer: %+v", deployment)
	response, err := resty.R().SetBody(deployment).Post(requestUrl)
	if err != nil {
		return "", err
	}

	if response.StatusCode() != 202 {
		return "", fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var createResponse struct {
		Error        bool   `json:"error"`
		Data         string `json:"data`
		DeploymentId string `json:"deploymentId`
	}

	if err := json.Unmarshal(response.Body(), &createResponse); err != nil {
		return "", errors.New("Unable to parse failed create deployment response: " + err.Error())
	}

	log.Debugf("Received deployer response: %+v", createResponse)
	if createResponse.Error {
		return "", errors.New("Unable to create deployment: " + createResponse.Data)
	}

	deploymentId := createResponse.DeploymentId
	if deploymentId == "" {
		return "", errors.New("Unable to get deployment id")
	}

	log.Infof("Waiting for deployment %s to be available...", deploymentId)
	if err := client.waitUntilDeploymentStateAvailable(deploymentId, log); err != nil {
		return "", errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	log.Infof("Waiting for load tester %s service url to be available...", loadTesterName)
	if err := client.waitUntilServiceUrlAvailable(deploymentId, loadTesterName, log); err != nil {
		return "", fmt.Errorf("Unable to waiting for %s url to be available: %s", loadTesterName, err)
	}

	return deploymentId, nil
}

func (client *DeployerClient) CreateDeploymentWithTemplate(
	deploymentTemplate string,
	deployment *deployer.Deployment,
	loadTesterName string,
	log *logging.Logger) (string, error) {
	if deploymentTemplate == "" {
		return "", errors.New("Empty deployment template found")
	}

	requestUrl := UrlBasePath(client.Url) + path.Join(
		client.Url.Path, "v1", "templates", deploymentTemplate, "deployments")

	response, err := resty.R().SetBody(deployment).Post(requestUrl)
	if err != nil {
		return "", err
	}

	if response.StatusCode() != 202 {
		return "", fmt.Errorf("Invalid status code returned %d: %s", response.StatusCode(), response.String())
	}

	var createResponse struct {
		Error        bool   `json:"error"`
		Data         string `json:"data`
		DeploymentId string `json:"deploymentId`
	}

	if err := json.Unmarshal(response.Body(), &createResponse); err != nil {
		return "", errors.New("Unable to parse failed create deployment response: " + err.Error())
	}

	log.Debugf("Received deployer response: %+v", createResponse)
	if createResponse.Error {
		return "", errors.New("Unable to create deployment: " + createResponse.Data)
	}

	deploymentId := createResponse.DeploymentId
	if deploymentId == "" {
		return "", errors.New("Unable to get deployment id")
	}

	log.Infof("Waiting for deployment %s to be available...", deploymentId)
	if err := client.waitUntilDeploymentStateAvailable(deploymentId, log); err != nil {
		return "", errors.New("Unable to waiting for deployment state to be available: " + err.Error())
	}

	log.Infof("Waiting for load tester %s service url to be available...", loadTesterName)
	if err := client.waitUntilServiceUrlAvailable(deploymentId, loadTesterName, log); err != nil {
		return "", fmt.Errorf("Unable to waiting for %s url to be available: %s", loadTesterName, err)
	}

	return deploymentId, nil
}

func (client *DeployerClient) waitUntilServiceUrlAvailable(
	deploymentId string,
	serviceName string,
	log *logging.Logger) error {
	url, err := client.GetServiceUrl(deploymentId, serviceName, log)
	if err != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", serviceName, err.Error())
	}

	restClient := resty.New()
	log.Infof("Waiting for service url %s to be available...", url)
	return funcs.LoopUntil(time.Minute*30, time.Second*10, func() (bool, error) {
		_, err := restClient.R().Get(url)
		if err != nil {
			return false, nil
		}

		log.Infof("%s url is now available", serviceName)
		return true, nil
	})
}

type GetSupportedAWSInstancesResponse struct {
	Instances []string `json:"instances"`
	Error     bool     `json:"error"`
	Data      string   `json:"data"`
}

func (client *DeployerClient) GetSupportedAWSInstances(region string, availabilityZone string) ([]string, error) {
	requestUrl := UrlBasePath(client.Url) +
		path.Join(client.Url.Path, "v1", "aws", "regions", region, "availabilityZones", availabilityZone, "instances")

	response, err := resty.R().Get(requestUrl)
	if err != nil {
		return nil, err
	}

	instanceResponse := GetSupportedAWSInstancesResponse{}
	if err := json.Unmarshal(response.Body(), &instanceResponse); err != nil {
		return nil, errors.New("Unable to parse aws instances response: " + err.Error())
	}

	if instanceResponse.Error {
		return nil, errors.New("Unable to get supported aws instances: " + err.Error())
	}

	return instanceResponse.Instances, nil
}
