package main

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"fmt"

	"github.com/golang/glog"
	"github.com/hyperpilotio/blobstore"
	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/deployer/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"

	logging "github.com/op/go-logging"
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
	Store          blobstore.BlobStore
	Config         *viper.Viper
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

func ParseStateString(state string) clusterState {
	switch state {
	case "Deploying":
		return DEPLOYING
	case "Reserved":
		return RESERVED
	case "Available":
		return AVAILABLE
	case "Failed":
		return FAILED
	}

	return -1
}

type storeCluster struct {
	DeploymentTemplate string
	DeploymentId       string
	RunId              string
	State              string
	Created            string
}

func NewClusters(deployerClient *clients.DeployerClient, config *viper.Viper) (*Clusters, error) {
	clusterStore, err := blobstore.NewBlobStore("WorkloadProfilerClusters", config)
	if err != nil {
		return nil, errors.New("Unable to create deployments store: " + err.Error())
	}

	return &Clusters{
		Store:          clusterStore,
		Config:         config,
		DeployerClient: deployerClient,
		Deployments:    []*cluster{},
		MaxClusters:    5,
	}, nil
}

func (clusters *Clusters) ReloadClusterState() error {
	existingClusters, err := clusters.Store.LoadAll(func() interface{} {
		return &storeCluster{}
	})

	if err != nil {
		return fmt.Errorf("Unable to load profiler clusters: %s", err.Error())
	}

	storeClusters := []*cluster{}
	for _, deployment := range existingClusters.([]interface{}) {
		storeCluster := deployment.(*storeCluster)
		deploymentReady, err := clusters.DeployerClient.IsDeploymentReady(storeCluster.DeploymentId)
		if err != nil {
			glog.Warningf("Skip loading deployment, unable to get deployment state: %s", err.Error())
			continue
		}

		if deploymentReady {
			reloadCluster := &cluster{
				deploymentTemplate: storeCluster.DeploymentTemplate,
				deploymentId:       storeCluster.DeploymentId,
				runId:              storeCluster.RunId,
				state:              ParseStateString(storeCluster.State),
			}

			if createdTime, err := time.Parse(time.RFC822, storeCluster.Created); err == nil {
				reloadCluster.created = createdTime
			} else {
				glog.Warningf("Unable to parse created time %s: %s", storeCluster.Created, err.Error())
			}

			storeClusters = append(storeClusters, reloadCluster)
		} else {
			if err := clusters.Store.Delete(storeCluster.RunId); err != nil {
				glog.Errorf("Unable to delete profiler cluster: %s", err.Error())
			}
		}
	}

	for _, deployment := range storeClusters {
		switch deployment.state {
		case RESERVED, FAILED:
			log, logErr := log.NewLogger(clusters.Config, deployment.runId)
			if logErr != nil {
				return errors.New("Error creating deployment logger: " + logErr.Error())
			}

			go func() {
				unreserveResult := <-clusters.UnreserveDeployment(deployment.runId, log.Logger)
				if unreserveResult.Err != "" {
					glog.Warningf("Unable to unreserve %s deployment: %s", deployment.runId, unreserveResult.Err)
				}
			}()
		}

		clusters.Deployments = append(clusters.Deployments, deployment)
	}

	return nil
}

func (clusters *Clusters) newStoreCluster(selectedCluster *cluster) (*storeCluster, error) {
	cluster := &storeCluster{
		DeploymentTemplate: selectedCluster.deploymentTemplate,
		DeploymentId:       selectedCluster.deploymentId,
		RunId:              selectedCluster.runId,
		State:              GetStateString(selectedCluster.state),
		Created:            selectedCluster.created.Format(time.RFC822),
	}

	return cluster, nil
}

func (clusters *Clusters) ReserveDeployment(
	config *viper.Viper,
	applicationConfig *models.ApplicationConfig,
	runId string,
	userId string,
	log *logging.Logger) <-chan ReserveResult {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	// TODO: Find a cluster that has the same deployment template base, and reserve it.
	// If not, launch a new one up to the configured limit.
	var selectedCluster *cluster
	for _, deployment := range clusters.Deployments {
		if deployment.deploymentTemplate == applicationConfig.DeploymentTemplate && deployment.state == AVAILABLE {
			selectedCluster = deployment
			break
		}
	}

	reserveResult := make(chan ReserveResult, 2)

	if selectedCluster == nil {
		if len(clusters.Deployments) == clusters.MaxClusters {
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

				if err := clusters.storeCluster(selectedCluster); err != nil {
					log.Errorf("Unable to store %s cluster during reserve deployment: %s", runId, err.Error())
				}

				reserveResult <- ReserveResult{
					DeploymentId: *deploymentId,
				}
			}
		}()
	} else {
		go func() {
			if err := clusters.deployExtensions(applicationConfig,
				selectedCluster.deploymentId, userId, runId, log); err != nil {
				selectedCluster.state = FAILED
				selectedCluster.failure = err.Error()
				reserveResult <- ReserveResult{
					Err: err.Error(),
				}
			} else {
				originRunId := selectedCluster.runId
				selectedCluster.state = RESERVED
				selectedCluster.runId = runId

				if originRunId != "" {
					if err := clusters.Store.Delete(originRunId); err != nil {
						log.Errorf("Unable to delete profiler cluster: %s", err.Error())
					}
				}

				if err := clusters.storeCluster(selectedCluster); err != nil {
					log.Errorf("Unable to store %s cluster during reserve deployment: %s", runId, err.Error())
				}

				reserveResult <- ReserveResult{
					DeploymentId: selectedCluster.deploymentId,
				}
			}
		}()
	}

	return reserveResult
}

func (clusters *Clusters) UnreserveDeployment(runId string, log *logging.Logger) <-chan UnreserveResult {
	// TODO: Unreserve a deployment. After certain time also try to delete deployments.
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	var selectedCluster *cluster
	for _, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			selectedCluster = deployment
			break
		}
	}

	unreserveResult := make(chan UnreserveResult, 2)

	if selectedCluster == nil {
		unreserveResult <- UnreserveResult{
			Err: fmt.Sprintf("Unable to find %s cluster", runId),
		}
		return unreserveResult
	} else if selectedCluster.state == UNRESERVING {
		unreserveResult <- UnreserveResult{
			Err: fmt.Sprintf("Cluster %s is already unreserved"),
		}
		return unreserveResult
	}

	selectedCluster.state = UNRESERVING

	go func() {
		err := clusters.resetTemplateDeployment(
			selectedCluster.deploymentTemplate,
			selectedCluster.deploymentId,
			log)
		selectedCluster.state = AVAILABLE

		if err != nil {
			unreserveResult <- UnreserveResult{
				Err: err.Error(),
			}
		} else {
			if err := clusters.storeCluster(selectedCluster); err != nil {
				glog.Warningf(
					"Unable to store %s cluster during unreserve deployment: %s",
					runId,
					err.Error())
			}

			unreserveResult <- UnreserveResult{
				RunId: runId,
			}
		}

	}()

	clusters.mutex.Unlock()

	return unreserveResult
}

func (clusters *Clusters) storeCluster(cluster *cluster) error {
	storeCluster, err := clusters.newStoreCluster(cluster)
	if err != nil {
		return fmt.Errorf("Unable to create store cluster for run %s: %s", cluster.runId, err)
	}

	if err := clusters.Store.Store(storeCluster.RunId, storeCluster); err != nil {
		return fmt.Errorf("Unable to store %s cluster: %s", cluster.runId, err.Error())
	}

	return nil
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

func (clusters *Clusters) resetTemplateDeployment(
	deploymentTemplate string,
	deploymentId string,
	log *logging.Logger) error {
	if err := clusters.DeployerClient.ResetTemplateDeployment(deploymentTemplate, deploymentId, log); err != nil {
		return errors.New("Unable to reset template deployment: " + err.Error())
	}

	return nil
}

func (clusters *Clusters) deployExtensions(
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

	if err := clusters.DeployerClient.DeployExtensions(applicationConfig.DeploymentTemplate,
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
