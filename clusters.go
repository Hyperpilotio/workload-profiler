package main

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"fmt"

	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	logging "github.com/op/go-logging"
	"github.com/spf13/viper"
)

type clusterState int

// Possible deployment states
const (
	DEPLOYING = 0
	AVAILABLE = 1
	RESERVED  = 2
	FAILED    = 3

	ErrMaxClusters = "Max clusters reached"
)

type ReserveResult struct {
	DeploymentId string
	Err          string
}

type UnreserveResult struct {
	RunId string
	Err   string
}

type cluster struct {
	deploymentTemplate string
	deploymentId       string
	runId              string
	state              clusterState
	failure            string
	created            time.Time
}

type Clusters struct {
	DeployerClient *clients.DeployerClient
	mutex          sync.Mutex
	MaxClusters    int
	Deployments    []*cluster
}

func GetStateString(state clusterState) string {
	switch state {
	case DEPLOYING:
		return "Deploying"
	case RESERVED:
		return "Reserved"
	case AVAILABLE:
		return "Available"
	case FAILED:
		return "Failed"
	}

	return ""
}

func NewClusters(deployerClient *clients.DeployerClient) *Clusters {
	return &Clusters{
		DeployerClient: deployerClient,
		Deployments:    []*cluster{},
		MaxClusters:    5,
	}
}

func (clusters *Clusters) ReserveDeployment(
	config *viper.Viper,
	applicationConfig *models.ApplicationConfig,
	runId string,
	userId string,
	log *logging.Logger) <-chan ReserveResult {
	clusters.mutex.Lock()

	// TODO: Find a cluster that has the same deployment template base, and reserve it.
	// If not, launch a new one up to the configured limit.
	var selectedCluster *cluster
	for _, deployment := range clusters.Deployments {
		if deployment.deploymentTemplate == applicationConfig.DeploymentTemplate && deployment.state == AVAILABLE {
			selectedCluster = deployment
			break
		}
	}

	reserveResult := make(chan ReserveResult)

	if selectedCluster == nil {
		if len(clusters.Deployments) == clusters.MaxClusters {
			clusters.mutex.Unlock()
			reserveResult <- ReserveResult{
				Err: ErrMaxClusters,
			}
			return reserveResult
		}

		selectedCluster = &cluster{
			deploymentTemplate: applicationConfig.DeploymentTemplate,
			runId:              runId,
			state:              DEPLOYING,
			created:            time.Now(),
		}

		clusters.Deployments = append(clusters.Deployments, selectedCluster)

		go func() {
			if deploymentId, err := clusters.createDeployment(applicationConfig, userId, runId, log); err != nil {
				selectedCluster.state = FAILED
				selectedCluster.failure = err.Error()
				reserveResult <- ReserveResult{
					Err: err.Error(),
				}
			} else {
				selectedCluster.deploymentId = *deploymentId
				selectedCluster.state = RESERVED
				reserveResult <- ReserveResult{
					DeploymentId: *deploymentId,
				}
			}
		}()
	} else {
		go func() {
			if err := clusters.deployKubernetesObjects(applicationConfig,
				selectedCluster.deploymentId, userId, runId, log); err != nil {
				selectedCluster.state = FAILED
				selectedCluster.failure = err.Error()
				reserveResult <- ReserveResult{
					Err: err.Error(),
				}
			} else {
				selectedCluster.state = RESERVED
				selectedCluster.runId = runId
				reserveResult <- ReserveResult{
					DeploymentId: selectedCluster.deploymentId,
				}
			}
		}()
	}

	clusters.mutex.Unlock()

	return reserveResult
}

func (clusters *Clusters) UnreserveDeployment(runId string, log *logging.Logger) <-chan UnreserveResult {
	// TODO: Unreserve a deployment. After certain time also try to delete deployments.
	clusters.mutex.Lock()

	var selectedCluster *cluster
	for _, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			selectedCluster = deployment
			break
		}
	}

	unreserveResult := make(chan UnreserveResult)

	if selectedCluster == nil {
		unreserveResult <- UnreserveResult{
			Err: fmt.Sprintf("Unable to find %s cluster", runId),
		}
	} else {
		go func() {
			if err := clusters.deleteKubernetesObjects(selectedCluster.deploymentId, log); err != nil {
				unreserveResult <- UnreserveResult{
					Err: err.Error(),
				}
			} else {
				unreserveResult <- UnreserveResult{
					RunId: runId,
				}
			}
		}()
	}

	clusters.mutex.Unlock()

	return unreserveResult
}

func (clusters *Clusters) createDeployment(
	applicationConfig *models.ApplicationConfig,
	userId string,
	runId string,
	log *logging.Logger) (*string, error) {
	// TODO: We assume there is one service per app and in one region
	// Also we assume Kubernetes only.
	emptyNodesJSON := `{ "nodes": [] }`
	clusterDefinition := &deployer.ClusterDefinition{}
	if err := json.Unmarshal([]byte(emptyNodesJSON), clusterDefinition); err != nil {
		return nil, errors.New("Unable to deserializing empty clusterDefinition: " + err.Error())
	}

	deployment := &deployer.Deployment{
		UserId:            userId,
		Region:            "us-east-1",
		Name:              "workload-profiler-" + applicationConfig.Name,
		NodeMapping:       []deployer.NodeMapping{},
		ClusterDefinition: *clusterDefinition,
		KubernetesDeployment: &deployer.KubernetesDeployment{
			Kubernetes: []deployer.KubernetesTask{},
		},
	}

	for _, appTask := range applicationConfig.TaskDefinitions {
		nodeMapping := &deployer.NodeMapping{}
		if err := clusters.convertBsonType(appTask.NodeMapping, nodeMapping); err != nil {
			return nil, errors.New("Unable to convert to nodeMapping: " + err.Error())
		}
		kubernetesTask := &deployer.KubernetesTask{}
		if err := clusters.convertBsonType(appTask.TaskDefinition, kubernetesTask); err != nil {
			return nil, errors.New("Unable to convert to nodeMapping: " + err.Error())
		}

		deployment.NodeMapping = append(deployment.NodeMapping, *nodeMapping)
		deployment.KubernetesDeployment.Kubernetes =
			append(deployment.KubernetesDeployment.Kubernetes, *kubernetesTask)
	}

	deploymentId, createErr := clusters.DeployerClient.CreateDeployment(
		applicationConfig.DeploymentTemplate, deployment, applicationConfig.LoadTester.Name, log)
	if createErr != nil {
		return nil, errors.New("Unable to create deployment: " + createErr.Error())
	}

	return deploymentId, nil
}

func (clusters *Clusters) deleteDeployment(deploymentId string, log *logging.Logger) error {
	if err := clusters.DeployerClient.DeleteDeployment(deploymentId, log); err != nil {
		return errors.New("Unable to delete deployment: " + err.Error())
	}

	return nil
}

func (clusters *Clusters) deleteKubernetesObjects(deploymentId string, log *logging.Logger) error {
	if err := clusters.DeployerClient.DeleteKubernetesObjects(deploymentId, log); err != nil {
		return errors.New("Unable to delete kubernetes objects: " + err.Error())
	}

	return nil
}

func (clusters *Clusters) deployKubernetesObjects(
	applicationConfig *models.ApplicationConfig,
	deploymentId string,
	userId string,
	runId string,
	log *logging.Logger) error {
	emptyNodesJSON := `{ "nodes": [] }`
	clusterDefinition := &deployer.ClusterDefinition{}
	if err := json.Unmarshal([]byte(emptyNodesJSON), clusterDefinition); err != nil {
		return errors.New("Unable to deserializing empty clusterDefinition: " + err.Error())
	}

	deployment := &deployer.Deployment{
		UserId:            userId,
		Region:            "us-east-1",
		Name:              "workload-profiler-" + applicationConfig.Name,
		NodeMapping:       []deployer.NodeMapping{},
		ClusterDefinition: *clusterDefinition,
		KubernetesDeployment: &deployer.KubernetesDeployment{
			Kubernetes: []deployer.KubernetesTask{},
		},
	}

	for _, appTask := range applicationConfig.TaskDefinitions {
		nodeMapping := &deployer.NodeMapping{}
		if err := clusters.convertBsonType(appTask.NodeMapping, nodeMapping); err != nil {
			return errors.New("Unable to convert to nodeMapping: " + err.Error())
		}
		kubernetesTask := &deployer.KubernetesTask{}
		if err := clusters.convertBsonType(appTask.TaskDefinition, kubernetesTask); err != nil {
			return errors.New("Unable to convert to nodeMapping: " + err.Error())
		}

		deployment.NodeMapping = append(deployment.NodeMapping, *nodeMapping)
		deployment.KubernetesDeployment.Kubernetes =
			append(deployment.KubernetesDeployment.Kubernetes, *kubernetesTask)
	}

	if err := clusters.DeployerClient.DeployKubernetesObjects(applicationConfig.DeploymentTemplate,
		deploymentId, deployment, applicationConfig.LoadTester.Name, log); err != nil {
		return errors.New("Unable to deploy kubernetes objects: " + err.Error())
	}

	return nil
}

func (clusters *Clusters) convertBsonType(bson interface{}, convert interface{}) error {
	b, marshalErr := json.Marshal(bson)
	if marshalErr != nil {
		return errors.New("Unable to marshal bson interface to json: " + marshalErr.Error())
	}

	unmarshalErr := json.Unmarshal(b, convert)
	if unmarshalErr != nil {
		return errors.New("Unable to convert bson interface: " + unmarshalErr.Error())
	}

	return nil
}

func (clusters *Clusters) DeleteCluster(runId string) {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	newDeployments := []*cluster{}
	for _, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			continue
		}
		newDeployments = append(newDeployments, deployment)
	}
	clusters.Deployments = newDeployments
}

func (clusters *Clusters) SetState(runId string, state clusterState) {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	for _, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			deployment.state = state
			break
		}
	}
}

func (clusters *Clusters) GetState(runId string) clusterState {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	for _, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			return deployment.state
		}
	}

	return -1
}
