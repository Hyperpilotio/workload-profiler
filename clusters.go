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
	AVAILABLE = 0
	WAITTING  = 1
	RUNNING   = 2
	FINISHED  = 3
	FAILED    = 4
)

type cluster struct {
	deploymentTemplate string
	deploymentId       string
	runId              string
	state              clusterState
}

type Clusters struct {
	DeployerClient *DeployerClient
	mutex          sync.Mutex
	MaxClusters    int
	Deployments    []cluster
}

func GetStateString(state clusterState) string {
	switch state {
	case AVAILABLE:
		return "Available"
	case WAITTING:
		return "Waitting"
	case RUNNING:
		return "Running"
	case FINISHED:
		return "Finished"
	case FAILED:
		return "Failed"
	}

	return ""
}

func NewClusters(deployerClient *DeployerClient) *Clusters {
	return &Clusters{
		DeployerClient: deployerClient,
		Deployments:    []cluster{},
		MaxClusters:    5,
	}
}

func (clusters *Clusters) ReserveDeployment(applicationConfig *ApplicationConfig,
	runId string, userId string) (string, error) {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()
	// TODO: Find a cluster that has the same deployment template base, and reserve it.
	// If not, launch a new one up to the configured limit.

	deploymentResources := []string{}
	for _, deployment := range clusters.Deployments {
		if deployment.deploymentTemplate == applicationConfig.DeploymentTemplate {
			if deployment.state == AVAILABLE {
				deploymentResources = append(deploymentResources, deployment.deploymentId)
			}
		}
	}

	cluster := cluster{
		deploymentTemplate: applicationConfig.DeploymentTemplate,
		runId:              runId,
		state:              AVAILABLE,
	}

	if len(deploymentResources) == 0 {
		deploymentId, createErr := clusters.createDeployment(applicationConfig, userId, runId)
		if createErr != nil {
			return "", errors.New("Unable to launch deployment id: " + createErr.Error())
		}
		cluster.deploymentId = *deploymentId
	} else {
		cluster.deploymentId = deploymentResources[0]
	}

	if err := clusters.appendDeployments(cluster); err != nil {
		return "", errors.New("Unable to reserve cluster: " + err.Error())
	}

	return cluster.deploymentId, nil
}

func (clusters *Clusters) appendDeployments(deployment cluster) error {
	if len(clusters.Deployments) == clusters.MaxClusters {
		deployment.state = WAITTING
		// TODO: waitting cluster queue, call UnreserveDeployment retry relase
		return errors.New("Unable to append deployment to the cluster, because the limit is exceeded")
	} else {
		clusters.Deployments = append(clusters.Deployments, deployment)
	}

	return nil
}

func (clusters *Clusters) UnreserveDeployment(runId string) error {
	// TODO: Unreserve a deployment. After certain time also try to delete deployments.
	return nil
}

func (clusters *Clusters) createDeployment(applicationConfig *ApplicationConfig,
	userId string, runId string) (*string, error) {
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
