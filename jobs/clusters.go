package jobs

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"fmt"

	"github.com/golang/glog"
	"github.com/hyperpilotio/blobstore"
	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/hyperpilotio/go-utils/log"
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

var clusterStates = map[clusterState]string{
	DEPLOYING:   "Deploying",
	AVAILABLE:   "Available",
	RESERVED:    "Reserved",
	UNRESERVING: "Unreserving",
	FAILED:      "Failed",
}

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
			glog.V(1).Infof("Found recovered cluster %s to be not available, deleting from store", storeCluster.DeploymentId)
			if err := clusters.Store.Delete(storeCluster.RunId); err != nil {
				glog.Errorf("Unable to delete profiler cluster: %s", err.Error())
			}
		}
	}

	for _, deployment := range storeClusters {
		switch deployment.state {
		case RESERVED, FAILED:
			glog.V(1).Infof("Unreserving deployment for cluster %+v", deployment)
			log, logErr := log.NewLogger(clusters.Config.GetString("filesPath"), deployment.runId)
			if logErr != nil {
				return errors.New("Error creating deployment logger: " + logErr.Error())
			}

			go func() {
				unreserveResult := <-clusters.unreserveCluster(deployment, log.Logger)
				if unreserveResult.Err != "" {
					glog.Warningf("Unable to unreserve %s deployment: %s", deployment.runId, unreserveResult.Err)
				}
				log.Logger.Infof("Cluster %s unreserved.", deployment.deploymentId)
			}()
		}

		glog.V(1).Infof("Recovered cluster from store: %+v...", deployment)
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

func (clusters *Clusters) removeDeployment(runId string) bool {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()
	for i, deployment := range clusters.Deployments {
		if deployment.runId == runId {
			// Remove cluster from list
			clusters.Deployments[i] = clusters.Deployments[len(clusters.Deployments)-1]
			clusters.Deployments[len(clusters.Deployments)-1] = nil
			clusters.Deployments = clusters.Deployments[:len(clusters.Deployments)-1]
			return true
		}
	}
	return false
}

func (clusters *Clusters) ReserveDeployment(
	config *viper.Viper,
	applicationConfig *models.ApplicationConfig,
	jobDeploymentConfig JobDeploymentConfig,
	runId string,
	log *logging.Logger) <-chan ReserveResult {
	clusters.mutex.Lock()
	defer clusters.mutex.Unlock()

	// TODO: Find a cluster that has the same deployment template base, and reserve it.
	// If not, launch a new one up to the configured limit.
	var selectedCluster *cluster

	reserveResult := make(chan ReserveResult)

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
			deploymentId, deploymentErr :=
				clusters.createDeployment(applicationConfig, jobDeploymentConfig, runId, log)
			if deploymentErr != nil {
				clusters.removeDeployment(runId)
				reserveResult <- ReserveResult{
					Err: deploymentErr.Error(),
				}
				return
			}

			selectedCluster.deploymentId = *deploymentId
			selectedCluster.state = RESERVED

			if err := clusters.storeCluster(selectedCluster); err != nil {
				log.Errorf("Unable to store %s cluster during reserve deployment: %s", runId, err.Error())
			}

			reserveResult <- ReserveResult{
				DeploymentId: *deploymentId,
			}
		}()
	} else {
		go func() {
			if selectedCluster.state == UNRESERVING {
				glog.Infof("Waiting for deployment %s to be unreserved...")
				for {
					if selectedCluster.state != UNRESERVING {
						break
					}

					time.Sleep(5 * time.Second)
				}

				if selectedCluster.state != AVAILABLE {
					message := "Cluster after unreserving is not in available state, new state: " +
						GetStateString(selectedCluster.state)
					log.Errorf(message)
					reserveResult <- ReserveResult{
						Err: message,
					}
					return
				}
			}

			if err := clusters.deployExtensions(applicationConfig,
				selectedCluster.deploymentId, runId, log); err != nil {
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

func (clusters *Clusters) unreserveCluster(cluster *cluster, log *logging.Logger) <-chan UnreserveResult {
	unreserveResult := make(chan UnreserveResult, 2)

	if cluster == nil {
		unreserveResult <- UnreserveResult{
			Err: fmt.Sprintf("Unable to find cluster"),
		}
		return unreserveResult
	} else if cluster.state == UNRESERVING {
		unreserveResult <- UnreserveResult{
			Err: fmt.Sprintf("Cluster %s is already unreserved"),
		}
		return unreserveResult
	}

	cluster.state = UNRESERVING

	go func() {
		// TODO: Cache deployments and only reset deployment
		if err := clusters.DeployerClient.DeleteDeployment(cluster.deploymentId, log); err != nil {
			unreserveResult <- UnreserveResult{
				Err: err.Error(),
			}
		} else {
			unreserveResult <- UnreserveResult{
				RunId: cluster.runId,
			}
		}

		clusters.removeDeployment(cluster.runId)

		if err := clusters.Store.Delete(cluster.runId); err != nil {
			glog.Errorf("Unable to delete profiler cluster: %s", err.Error())
		}
	}()

	return unreserveResult
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
	clusters.mutex.Unlock()

	return clusters.unreserveCluster(selectedCluster, log)
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
	jobDeploymentConfig JobDeploymentConfig,
	runId string,
	log *logging.Logger) (*string, error) {
	// TODO: We assume region is us-east-1 and we assume Kubernetes only.
	clusterDefinition := &deployer.ClusterDefinition{
		Nodes: jobDeploymentConfig.GetNodes(),
	}

	deployment := &deployer.Deployment{
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

	return nil
}

func (clusters *Clusters) deployExtensions(
	applicationConfig *models.ApplicationConfig,
	deploymentId string,
	runId string,
	log *logging.Logger) error {
	clusterDefinition := &deployer.ClusterDefinition{
		Nodes: []deployer.ClusterNode{},
	}
	deployment := &deployer.Deployment{
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
