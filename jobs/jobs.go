package jobs

import (
	"errors"
	"sync"
	"time"

	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

const (
	JOB_QUEUED    = "QUEUED"
	JOB_RESERVING = "RESERVING"
	JOB_RUNNING   = "RUNNING"
	JOB_FINISHED  = "FINISHED"
	JOB_FAILED    = "FAILED"
)

type JobSummary struct {
	DeploymentId string    `json:"deploymentId"`
	RunId        string    `json:"runId"`
	Status       string    `json:"status"`
	Create       time.Time `json:"create"`
}

type Job interface {
	GetId() string
	GetApplicationConfig() *models.ApplicationConfig
	GetLog() *log.FileLog
	Run(deploymentId string) error
	GetState() string
	SetState(state string)
	GetSummary() JobSummary
}

type JobManager struct {
	Queue    chan Job
	Jobs     map[string]Job
	Config   *viper.Viper
	Clusters *Clusters
	mutex    sync.Mutex
}

func NewJobManager(config *viper.Viper) (*JobManager, error) {
	deployerClient, err := clients.NewDeployerClient(config)
	if err != nil {
		return nil, errors.New("Unable to create new deployer client: " + err.Error())
	}

	clusters, err := NewClusters(deployerClient, config)
	if err != nil {
		return nil, errors.New("Unable to create clusters object: " + err.Error())
	}

	if err := clusters.ReloadClusterState(); err != nil {
		return nil, errors.New("Unable to reload cluster state: " + err.Error())
	}

	return &JobManager{
		Queue:    make(chan Job, 100),
		Jobs:     make(map[string]Job),
		Clusters: clusters,
		Config:   config,
	}, nil
}

func (manager *JobManager) AddJob(job Job) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	manager.Jobs[job.GetId()] = job
	manager.Queue <- job
}

func (manager *JobManager) Loop(userId string) {
	go func() {
		for {
			select {
			case job := <-manager.Queue:
				job.SetState(JOB_RESERVING)
				log := job.GetLog()
				defer log.LogFile.Close()

				deploymentId := ""
				runId := job.GetId()
				log.Logger.Infof("Waiting until %s job is completed...", runId)
				for {
					result := <-manager.Clusters.ReserveDeployment(
						manager.Config,
						job.GetApplicationConfig(),
						runId,
						userId,
						log.Logger)
					if result.Err != "" {
						log.Logger.Warningf("Unable to reserve deployment for job: " + result.Err)
						// Try reserving again after sleep
						time.Sleep(60 * time.Second)
					} else {
						deploymentId = result.DeploymentId
						log.Logger.Infof("Deploying job %s with deploymentId is %s", runId, deploymentId)
						break
					}
				}

				job.SetState(JOB_RUNNING)
				// TODO: Allow multiple jobs to run
				log.Logger.Infof("Running %s job", job.GetId())
				defer log.LogFile.Close()
				if err := job.Run(deploymentId); err != nil {
					// TODO: Store the error state in a map and display/return job status
					log.Logger.Errorf("Unable to run %s job: %s", runId, err)
					job.SetState(JOB_FAILED)
				} else {
					job.SetState(JOB_FINISHED)
				}

				unreserveResult := <-manager.Clusters.UnreserveDeployment(runId, log.Logger)
				if unreserveResult.Err != "" {
					log.Logger.Errorf("Unable to unreserve %s deployment: %s", runId, unreserveResult.Err)
				}
			}
		}
	}()
}

func (manager *JobManager) FindJob(id string) (Job, error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if job, ok := manager.Jobs[id]; !ok {
		return nil, errors.New("Unable to find job: " + id)
	} else {
		return job, nil
	}
}

func (manager *JobManager) GetJobs() []Job {
	jobs := make([]Job, len(manager.Jobs))
	for _, job := range manager.Jobs {
		jobs = append(jobs, job)
	}

	return jobs
}
