package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	deployer "github.com/hyperpilotio/deployer/apis"
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

type cluster struct {
	deploymentTemplate string
	deploymentId       string
	runId              string
	state              clusterState
	failure            string
}

type Clusters struct {
	DeployerClient    *DeployerClient
	mutex             sync.Mutex
	MaxClusters       int
	Deployments       []*cluster
	FailedDeployments []*cluster
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

func NewClusters(deployerClient *DeployerClient) *Clusters {
	return &Clusters{
		DeployerClient: deployerClient,
		Deployments:    []*cluster{},
		MaxClusters:    5,
	}
}

func (clusters *Clusters) ReserveDeployment(
	applicationConfig *ApplicationConfig,
	runId string,
	userId string) <-chan ReserveResult {
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
		}

		clusters.Deployments = append(clusters.Deployments, selectedCluster)

		go func() {
			if deploymentId, err := clusters.createDeployment(applicationConfig, userId, runId); err != nil {
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
		selectedCluster.state = RESERVED
		selectedCluster.runId = runId
	}

	clusters.mutex.Unlock()

	reserveResult <- ReserveResult{
		DeploymentId: selectedCluster.deploymentId,
	}

	return reserveResult
}

func (clusters *Clusters) UnreserveDeployment(runId string) error {
	// TODO: Unreserve a deployment. After certain time also try to delete deployments.
	return nil
}

func (clusters *Clusters) createDeployment(
	applicationConfig *ApplicationConfig,
	userId string,
	runId string) (*string, error) {
	// TODO: We assume there is one service per app and in one region
	// Also we assume Kubernetes only.
	clusterDefinition := &deployer.ClusterDefinition{}
	if err := clusters.convertBsonType(applicationConfig.ClusterDefinition, clusterDefinition); err != nil {
		return nil, errors.New("Unable to convert to clusterDefinition: " + err.Error())
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
		applicationConfig.DeploymentTemplate, deployment, applicationConfig.LoadTester.Name)
	if createErr != nil {
		return nil, errors.New("Unable to create deployment: " + createErr.Error())
	}

	return deploymentId, nil
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

func SetClusterState(deployments []cluster, runId string, state clusterState) error {
	findCluster := false
	for _, deployment := range deployments {
		if deployment.runId == runId {
			deployment.state = state
			findCluster = true
			break
		}
	}

	if !findCluster {
		return fmt.Errorf("Unable to set %s cluster state", runId)
	}

	return nil
}
