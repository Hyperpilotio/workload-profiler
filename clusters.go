package main

import (
	"sync"
)

type cluster struct {
	deploymentTemplate string
	deploymentId string
	runId string
}

type Clusters struct {
	DeployerClient *DeployerClient
	mutex sync.Mutex
	MaxClusters int
	Deployments []cluster
}

func NewClusters(deployerClient *DeployerClient) *Clusters {
	return &Clusters{
		DeployerClient: deployerClient,
		Deployments: []cluster{},
		MaxClusters: 5,
	}
}

func (clusters *Clusters) ReserveDeployment(applicationConfig ApplicationConfig, runId string) (string, error){
	cluster.mutex.Lock()
	defer cluster.mutex.Unlock()
	// TODO: Find a cluster that has the same deployment template base, and reserve it.
	// If not, launch a new one up to the configured limit.
	return "", nil
}

func (clusters *Clusters) UnreserveDeployment(runId string) error {
	// TODO: Unreserve a deployment. After certain time also try to delete deployments.
	return nil
}

func (clusters *Clusters) createDeployment(applicationConfig ApplicationConfig, userId string, runId string) (*string, error) {
	// TODO: We assume there is one service per app and in one region
	// Also we assume Kubernetes only.
	deployment := deployer.Deployment{
		UserId: userId,
		Name: "workload-profiler-" + appName,
		NodeMapping: []deployer.NodeMapping{},
		KubernetesDeployment: &deployer.KubernetesDeployment{
			Kubernetes: []deployer.KubernetesTask{},
		},
	}

	for _, appTask := range applicationConfig.TaskDefinitions {
		deployment.NodeMapping = append(deployment.NodeMapping, appTask.NodeMapping)
		deployment.KubernetesDeployment.Kubernetes =
			append(deployment.KubernetesDeployment.Kubernetes, appTask.TaskDefinition)
	}

	deploymentId, createErr := clusters.DeployerClient.CreateDeployment(deployment)
	if createErr != nil {
		return nil, errors.New("Unable to create deployment: " + createErr.Error())
	}


}
