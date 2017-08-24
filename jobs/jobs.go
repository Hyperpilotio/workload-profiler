package jobs

import (
	"errors"
	"regexp"
	"strconv"
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

type JobResults struct {
	Error string
	Data  interface{}
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
	SetFailed(error string)
	GetResults() <-chan *JobResults
	IsSkipUnreserveOnFailure() bool
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

func (config JobDeploymentConfig) GetNodes() []deployer.ClusterNode {
	if config.Nodes == nil {
		return []deployer.ClusterNode{}
	}

	return config.Nodes
}

type Worker struct {
	Id               int
	Jobs             <-chan Job
	FailedJobs       *FailedJobs
	RetryReservation bool
	Config           *viper.Viper
	Clusters         *Clusters
}

func (worker *Worker) Run() {
	go func() {
		for job := range worker.Jobs {
			if err := worker.RunJob(job); err != nil {
				job.SetState(JOB_FAILED)
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
	backOff := time.Duration(60) * time.Second
	maxBackOff := time.Duration(960) * time.Second
	for {
		result := <-worker.Clusters.ReserveDeployment(
			worker.Config,
			job.GetApplicationConfig(),
			job.GetJobDeploymentConfig(),
			runId,
			log.Logger)
		if result.Err != "" {
			log.Logger.Warningf("Unable to reserve deployment for job: %s", result.Err)
			if !worker.RetryReservation {
				message := "Unable to reserve deployment: " + result.Err
				job.SetFailed(message)
				return errors.New(message)
			}

			log.Logger.Warningf("Sleeping %s seconds to retry...", backOff)
			// Try reserving again after sleep
			time.Sleep(backOff)
			backOff *= 2
			if backOff > maxBackOff {
				message := "Unable to reserve deployment after retries: " + result.Err
				job.SetFailed(message)
				return errors.New(message)
			}
		} else {
			deploymentId = result.DeploymentId
			log.Logger.Infof("Deploying job %s with deploymentId is %s", runId, deploymentId)
			break
		}
	}

	job.SetState(JOB_RUNNING)
	log.Logger.Infof("Running %s job", job.GetId())
	jobErr := job.Run(deploymentId)
	if jobErr != nil {
		log.Logger.Errorf(
			"Unable to run %s job: %s, skip unreserve on failure: %s",
			runId,
			jobErr,
			strconv.FormatBool(job.IsSkipUnreserveOnFailure()))
		job.SetState(JOB_FAILED)
	} else {
		job.SetState(JOB_FINISHED)
	}

	deleteCluster := jobErr == nil || !job.IsSkipUnreserveOnFailure()
	unreserveResult := <-worker.Clusters.UnreserveDeployment(runId, deleteCluster, log.Logger)
	if unreserveResult.Err != "" {
		log.Logger.Errorf("Unable to unreserve %s deployment: %s", runId, unreserveResult.Err)
	}

	return jobErr
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
			Id:               i,
			Config:           config,
			Clusters:         clusters,
			RetryReservation: config.GetBool("retryReservation"),
			FailedJobs:       failedJobs,
			Jobs:             queue,
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

func (manager *JobManager) FindJobsMatches(regex string) ([]Job, error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	jobs := make([]Job, 0)
	for id, job := range manager.Jobs {
		result, err := regexp.MatchString(regex, id)
		if err != nil {
			return nil, err
		}
		if result {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil

}

func (manager *JobManager) GetJobs() []Job {
	jobs := make([]Job, len(manager.Jobs))
	for _, job := range manager.Jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

func (manager *JobManager) GetFailedJobs() []Job {
	jobs := make([]Job, len(manager.FailedJobs.Jobs))
	for _, job := range manager.FailedJobs.Jobs {
		jobs = append(jobs, job)
	}

	return jobs
}
