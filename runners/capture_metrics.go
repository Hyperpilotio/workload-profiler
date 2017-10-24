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
	config *viper.Viper) *CaptureMetricsRun {

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}
	glog.V(1).Infof("Created new capture metrics run with id: %s", id)

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	id, err := generateId("capturemetrics-" + run.applicationConfig.Name)
	if err != nil {
		return nil, errors.New("Unable to generate Id for capture metrics run: " + err.Error())
	}

	return &CaptureMetricsRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			DeployerClient:    deployerClient,
			ProfileLog:        log,
			Created:           time.Now(),
			DirectJob:         false,
		},
		LoadTester:         loadTester,
		Benchmark:          benchmark,
		BenchmarkIntensity: benchmarkIntensity,
		Duration:           duration,
	}
}

func (run *CaptureMetricsRun) runSlowCookerController(slowCookerController *models.SlowCookerController) error {
	loadTesterName := run.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	client := clients.SlowCookerClient{}
	_, err := client.RunBenchmark(url,
		run.Id,
		float64(slowCookerController.Calibrate.InitialConcurrency),
		slowCookerController.Calibrate.RunsPerIntensity,
		slowCookerController,
		run.ProfileLog.Logger)
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

func (run *CaptureMetricsRun) Run(deploymentId string) error {
	run.DeploymentId = deploymentId

	if run.Benchmark != nil {
		// TODO: Run benchmark with benchmark intensity
	}

	if err := run.runApplicationLoadTest(); err != nil {
		return fmt.Errorf("Unable to run load controller: " + err.Error())
	}

	time.Sleep(run.Duration)

	run.snapshotInfluxData()
	return nil
}

func (run *CaptureMetricsRun) getSnapshotId() string {
	benchmarkName := "None"
	if run.Benchmark != nil {
		benchmarkName = run.Benchmark.Name
	}
	return run.GetId() + "-" + run.ApplicationConfig.Name + "-" + benchmarkName
}

func (run *CaptureMetricsRun) snapshotInfluxData() error {
	url, err := run.DeployerClient.GetServiceUrl(run.DeploymentId, "influxsrv", run.ProfileLog.Logger)
	if err != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	influxClient := clients.NewInfluxClient(url, 8088, 8086)
	if err = influxClient.BackupDB(run.getSnapshotId()); err != nil {
		return errors.New("Unable to snapshot influx: " + err.Error())
	}

	return nil
}

func (run *CaptureMetricsRun) GetResults() <-chan *jobs.JobResults {
	return nil
}

func (run *CaptureMetricsRun) SetFailed(error string) {
}