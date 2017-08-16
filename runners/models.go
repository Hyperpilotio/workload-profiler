package runners

import (
	"time"

	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
)

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
	SkipUnreserveOnFailure    bool
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

func (run *ProfileRun) GetSummary() jobs.JobSummary {
	return jobs.JobSummary{
		DeploymentId: run.DeploymentId,
		RunId:        run.Id,
		Status:       run.State,
		Create:       run.Created,
	}
}

func (run *ProfileRun) GetJobDeploymentConfig() jobs.JobDeploymentConfig {
	return jobs.JobDeploymentConfig{}
}

func (run *ProfileRun) IsSkipUnreserveOnFailure() bool {
	return run.SkipUnreserveOnFailure
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
