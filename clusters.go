package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/hyperpilotio/workload-profiler/clients"

	"github.com/spf13/viper"
)

type clusterState int

// Possible deployment states
const (
	DEPLOYING   = 0
	AVAILABLE   = 1
	RESERVED    = 2
	UNRESERVING = 3
	FAILED      = 4

	ErrMaxClusters = "Max clusters reached"
)

var clusterStates = map[clusterState]string{
	DEPLOYING:   "Deploying",
	AVAILABLE:   "Available",
	RESERVED:    "Reserved",
	UNRESERVING: "Unreserving",
	FAILED:      "Failed",
}

type cluster struct {
	deploymentId string
	runId        string
	state        clusterState
	failure      string
	created      time.Time
}

type Clusters struct {
	Config         *viper.Viper
	DeployerClient *clients.DeployerClient
	mutex          sync.Mutex
	Deployments    []*cluster
}

func GetStateString(state clusterState) string {
	if stateString, ok := clusterStates[state]; ok {
		return stateString
	}

	return ""
}

func ParseStateString(state string) clusterState {
	for clusterState, stateString := range clusterStates {
		if stateString == state {
			return clusterState
		}
	}

	return -1
}

func NewClusters(deployerClient *clients.DeployerClient, config *viper.Viper) (*Clusters, error) {
	return &Clusters{
		Config:         config,
		DeployerClient: deployerClient,
		Deployments:    []*cluster{},
	}, nil
}

func (clusters *Clusters) GetCluster(runId string) (*cluster, error) {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	for _, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			return deployment, nil
		}
	}

	return nil, fmt.Errorf("Unable find %s cluster", runId)
}
