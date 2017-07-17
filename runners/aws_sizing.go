package runners

import (
	"github.com/hyperpilotio/workload-profiler/models"
)

type AWSSizingRun struct {
	ProfileRun
}

func NewAWSSizingRun(applicationConfig *models.ApplicationConfig) *AWSSizingRun {
	return &AWSSizingRun{
		ProfileRun: &ProfileRun{
			ApplicationConfig: applicationConfig,
		},
	}
}

func (run *AWSSizingRun) Run() error {

}
