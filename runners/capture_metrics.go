package runners

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-resty/resty"
	"github.com/golang/glog"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type CaptureMetricsRun struct {
	ProfileRun

	Config               *viper.Viper
	ServiceName          string
	LoadTester           models.LoadTester
	Benchmark            *models.Benchmark
	BenchmarkAgentClient *clients.BenchmarkAgentClient
	BenchmarkIntensity   int
	Duration             time.Duration
}

func NewCaptureMetricsRun(
	applicationConfig *models.ApplicationConfig,
	serviceName string,
	loadTester models.LoadTester,
	benchmark *models.Benchmark,
	benchmarkIntensity int,
	duration time.Duration,
	skipUnreserveOnFailure bool,
	config *viper.Viper) (*CaptureMetricsRun, error) {
	id, err := generateId("cm")
	if err != nil {
		return nil, errors.New("Unable to generate Id for capture metrics run: " + err.Error())
	}

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}
	glog.V(1).Infof("Created new capture metrics run with id: %s", id)

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	return &CaptureMetricsRun{
		ProfileRun: ProfileRun{
			Id:                     id,
			ApplicationConfig:      applicationConfig,
			DeployerClient:         deployerClient,
			ProfileLog:             log,
			Created:                time.Now(),
			DirectJob:              false,
			SkipUnreserveOnFailure: skipUnreserveOnFailure,
		},
		LoadTester:           loadTester,
		Benchmark:            benchmark,
		BenchmarkAgentClient: clients.NewBenchmarkAgentClient(),
		BenchmarkIntensity:   benchmarkIntensity,
		Duration:             duration,
		Config:               config,
	}, nil
}

func (run *CaptureMetricsRun) runSlowCookerController(slowCookerController *models.SlowCookerController) error {
	loadTesterName := run.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	client := clients.SlowCookerClient{}
	_, err := client.RunBenchmark(
		url,
		run.Id,
		float64(slowCookerController.AppLoad.Concurrency),
		1,
		slowCookerController,
		run.ProfileLog.Logger,
		false)
	if err != nil {
		return fmt.Errorf("Unable to run load test from slow cooker: " + err.Error())
	}

	return nil
}

func (run *CaptureMetricsRun) runDemoUiController(DemoUiController *models.DemoUiController) error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	if _, err := resty.R().Get(url + "/actions/run_load_controller"); err != nil {
		return fmt.Errorf("Unable to run load controller: " + err.Error())
	}

	return nil
}

func (run *CaptureMetricsRun) runApplicationLoadTest() error {
	loadTester := run.LoadTester
	if loadTester.SlowCookerController != nil {
		return run.runSlowCookerController(
			loadTester.SlowCookerController)
	} else if loadTester.DemoUiController != nil {
		return run.runDemoUiController(loadTester.DemoUiController)
	}

	return errors.New("No supported load controller found")
}

func (run *CaptureMetricsRun) runBenchmark(id string, service string, benchmark models.Benchmark, intensity int) error {
	for _, config := range benchmark.Configs {
		run.ProfileLog.Logger.Infof("Starting to run benchmark config: %+v", config)

		agentUrls, err := run.ProfileRun.GetColocatedAgentUrls("benchmark-agent", service, config.PlacementHost)
		if err != nil {
			return fmt.Errorf(
				"Unable to get benchmark agent url: " + err.Error())
		}

		if len(agentUrls) == 0 {
			return errors.New("No benchmark agents found in cluster")
		}

		for _, agentUrl := range agentUrls {
			if err := run.BenchmarkAgentClient.CreateBenchmark(
				agentUrl, &benchmark, &config, intensity, run.ProfileLog.Logger); err != nil {
				return fmt.Errorf("Unable to run benchmark %s with intensity %d: %s",
					benchmark.Name, intensity, err.Error())
			}
		}
	}

	return nil
}

func (run *CaptureMetricsRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	if run.Benchmark != nil {
		if err := run.runBenchmark("single", run.ServiceName, *run.Benchmark, run.BenchmarkIntensity); err != nil {
			return errors.New("Unable to run benchmark " + run.Benchmark.Name + ": " + err.Error())
		}
	}

	if err := run.runApplicationLoadTest(); err != nil {
		return fmt.Errorf("Unable to run load controller: " + err.Error())
	}

	run.ProfileLog.Logger.Infof("Waiting for %s to capture metrics run", run.Duration)
	time.Sleep(run.Duration)
	run.ProfileLog.Logger.Infof("Waiting completed, snapshotting influx..")
	if err := run.snapshotInfluxData(); err != nil {
		return errors.New("Unable to snapshot influx: " + err.Error())
	}

	return nil
}

func (run *CaptureMetricsRun) getSnapshotId() string {
	benchmarkName := "None"
	if run.Benchmark != nil {
		benchmarkName = run.Benchmark.Name
	}
	return run.GetId() + "-" + run.LoadTester.Scenario + "-" + benchmarkName
}

func (run *CaptureMetricsRun) snapshotInfluxData() error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, "influxsrv", run.ProfileLog.Logger)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	influxScriptPath := run.Config.GetString("influxScriptPath")
	influxClient := clients.NewInfluxClient(influxScriptPath, url, 8088, 8086)
	return influxClient.BackupDB(run.getSnapshotId())
}

func (run *CaptureMetricsRun) GetResults() <-chan *jobs.JobResults {
	return nil
}

func (run *CaptureMetricsRun) SetFailed(error string) {
	// No-op
}
