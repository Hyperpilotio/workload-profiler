package runners

import (
	"time"

	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/models"
)

const (
	QUEUED    = "QUEUED"
	RESERVING = "RESERVING"
	RUNNING   = "RUNNING"
	FINISHED  = "FINISHED"
	FAILED    = "FAILED"
)

type RunSummary struct {
	DeploymentId string    `json:"deploymentId"`
	RunId        string    `json:"runId"`
	State        string    `json:"state"`
	Created      time.Time `json:"created"`
}

type ProfileRun struct {
	Id                        string
	DeployerClient            *clients.DeployerClient
	BenchmarkControllerClient *clients.BenchmarkControllerClient
	SlowCookerClient          *clients.SlowCookerClient
	DeploymentId              string
	MetricsDB                 *db.MetricsDB
	ApplicationConfig         *models.ApplicationConfig
	ProfileLog                *log.FileLog
	State                     string
	Created                   time.Time
}

func (run *ProfileRun) GetId() string {
	return run.Id
}

func (run *ProfileRun) GetApplicationConfig() *models.ApplicationConfig {
	return run.ApplicationConfig
}

func (run *ProfileRun) GetLog() *log.FileLog {
	return run.ProfileLog
}

func (run *ProfileRun) GetState() string {
	return run.State
}

func (run *ProfileRun) SetState(state string) {
	run.State = state
}

func (run *ProfileRun) GetSummary() RunSummary {
	return RunSummary{
		DeploymentId: run.DeploymentId,
		RunId:        run.Id,
		State:        run.State,
		Created:      run.Created,
	}
}

type ProfileResults struct {
	Id           string
	StageResults []StageResult
}

type StageResult struct {
	Id        string
	StartTime string
	EndTime   string
}
