package jobs

import (
	"errors"
	"sync"
	"time"

	deployer "github.com/hyperpilotio/deployer/apis"
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
	GetJobDeploymentConfig() JobDeploymentConfig
	GetLog() *log.FileLog
	Run(deploymentId string) error
	GetState() string
	SetState(state string)
	GetSummary() JobSummary
}

type FailedJobs struct {
	Jobs  []Job
	mutex sync.Mutex
}

func NewFailedJobs() *FailedJobs {
	return &FailedJobs{
		Jobs: make([]Job, 1),
	}
}

func (jobs *FailedJobs) AddJob(job Job) {
	jobs.mutex.Lock()
	defer jobs.mutex.Unlock()
	jobs.Jobs = append(jobs.Jobs, job)
}

type JobDeploymentConfig struct {
	Nodes []deployer.ClusterNode
}

type Worker struct {
	Id         int
	Jobs       <-chan Job
	FailedJobs *FailedJobs
	Config     *viper.Viper
	Clusters   *Clusters
}

func (worker *Worker) Run() {
	go func() {
		for job := range worker.Jobs {
			if err := worker.RunJob(job); err != nil {
				worker.FailedJobs.AddJob(job)
			}
		}
	}()
}

func (worker *Worker) RunJob(job Job) error {
	job.SetState(JOB_RESERVING)
	log := job.GetLog()
	defer log.LogFile.Close()

	deploymentId := ""
	runId := job.GetId()
	log.Logger.Infof("Waiting until %s job is completed...", runId)
	backOff := time.Duration(60)
	maxBackOff := time.Duration(960)
	for {
		result := <-worker.Clusters.ReserveDeployment(
			worker.Config,
			job.GetApplicationConfig(),
			job.GetJobDeploymentConfig(),
			runId,
			log.Logger)
		if result.Err != "" {
			log.Logger.Warningf("Unable to reserve deployment for job: " + result.Err)
			log.Logger.Warningf("Sleeping %s seconds to retry...", backOff)
			// Try reserving again after sleep
			time.Sleep(backOff * time.Second)
			backOff *= 2
			if backOff > maxBackOff {
				return errors.New("Unable to reserve deployment after retries: " + result.Err)
			}
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
		return errors.New("Unable to run job: " + err.Error())
	} else {
		job.SetState(JOB_FINISHED)
	}

	unreserveResult := <-worker.Clusters.UnreserveDeployment(runId, log.Logger)
	if unreserveResult.Err != "" {
		log.Logger.Errorf("Unable to unreserve %s deployment: %s", runId, unreserveResult.Err)
	}

	return nil
}

type JobManager struct {
	Queue      chan Job
	Jobs       map[string]Job
	Workers    []*Worker
	FailedJobs *FailedJobs
	mutex      sync.Mutex
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

	workerCount := config.GetInt("workerCount")
	if workerCount == 0 {
		workerCount = 2
	}

	failedJobs := NewFailedJobs()

	queue := make(chan Job, 100)
	workers := []*Worker{}
	for i := 1; i <= workerCount; i++ {
		worker := &Worker{
			Id:         i,
			Config:     config,
			Clusters:   clusters,
			FailedJobs: failedJobs,
			Jobs:       queue,
		}
		worker.Run()
		workers = append(workers, worker)
	}

	return &JobManager{
		Queue:      queue,
		Jobs:       make(map[string]Job),
		FailedJobs: failedJobs,
		Workers:    workers,
	}, nil
}

func (manager *JobManager) AddJob(job Job) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	manager.Jobs[job.GetId()] = job
	manager.Queue <- job
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
