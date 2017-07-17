package runners

import (
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
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
