package runners

import (
	"errors"
	"fmt"
	"math/rand"
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
		ServiceName:          serviceName,
		Benchmark:            benchmark,
		BenchmarkAgentClient: clients.NewBenchmarkAgentClient(),
		BenchmarkIntensity:   benchmarkIntensity,
		Duration:             duration,
		Config:               config,
	}, nil
}

func (run *CaptureMetricsRun) runSlowCookerController(slowCookerController *models.SlowCookerController) error {
	run.ProfileLog.Logger.Infof("Running slow cooker controller")
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
	run.ProfileLog.Logger.Infof("Running demo ui controller")
	loadTesterName := run.LoadTester.Name
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
	// NOTE: We currently have two kinds of benchmarks, one that only has one config which means it's independent, and
	// another one that has two configs that it requires two benchmark agents to cooperate.
	// We assume here that single config benchmarks can be launched on all benchmark agents, and multiple config benchmarks
	// are randomly placed on separate hosts.

	run.ProfileLog.Logger.Infof("Starting to run benchmark config: %+v, service: %s", benchmark.Configs, service)
	benchmarkConfigCount := len(benchmark.Configs)
	colocatedAgentUrls, err := run.ProfileRun.GetColocatedAgentUrls("benchmark-agent", service, "service")
	if benchmarkConfigCount == 1 {
		if err != nil {
			return fmt.Errorf("Unable to get benchmark agent url: " + err.Error())
		} else if len(colocatedAgentUrls) == 0 {
			return errors.New("No benchmark agents found in cluster colocated to service " + service)
		}

		// Single config benchmarks are ran on every benchmark agent.
		config := benchmark.Configs[0]
		for _, agentUrl := range colocatedAgentUrls {
			if err := run.BenchmarkAgentClient.CreateBenchmark(
				agentUrl, &benchmark, &config, intensity, run.ProfileLog.Logger); err != nil {
				return fmt.Errorf("Unable to run benchmark %s with intensity %d: %s",
					benchmark.Name, intensity, err.Error())
			}
		}
		return nil
	}

	agentUrls, err := run.ProfileRun.DeployerClient.GetServiceUrls(run.DeploymentId, "benchmark-agent", run.ProfileLog.Logger)
	if err != nil {
		return errors.New("Unable to get service urls: " + err.Error())
	}

	agentCount := len(agentUrls)
	if agentCount < benchmarkConfigCount {
		return fmt.Errorf("Benchmark agent count (%d) is less than benchmark config count (%d)", agentCount, benchmarkConfigCount)
	}

	config := benchmark.Configs[0]
	colocatedAgentUrl := colocatedAgentUrls[0]
	if err := run.BenchmarkAgentClient.CreateBenchmark(
		colocatedAgentUrl, &benchmark, &config, intensity, run.ProfileLog.Logger); err != nil {
		return fmt.Errorf("Unable to run benchmark %s with intensity %d: %s", benchmark.Name, intensity, err.Error())
	}

	for i, agentUrl := range agentUrls {
		if agentUrl == colocatedAgentUrl {
			agentUrls[i] = agentUrls[len(agentUrls)-1]
			agentUrls = agentUrls[:len(agentUrls)-1]
			break
		}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	// Multiple config benchmarks are placed on random separate benchmark agents.
	for i, config := range benchmark.Configs[1:] {
		nextAgent := r.Intn(len(agentUrls))
		agentUrl := agentUrls[nextAgent]
		// Remove agent from list.
		agentUrls[i] = agentUrls[len(agentUrls)-1]
		agentUrls = agentUrls[:len(agentUrls)-1]
		if err := run.BenchmarkAgentClient.CreateBenchmark(
			agentUrl, &benchmark, &config, intensity, run.ProfileLog.Logger); err != nil {
			return fmt.Errorf("Unable to run benchmark %s with intensity %d: %s", benchmark.Name, intensity, err.Error())
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
	return run.GetId() + "-" + run.LoadTester.Scenario + "-" + benchmarkName + "-" + run.ServiceName
}

func (run *CaptureMetricsRun) snapshotInfluxData() error {
	loadTesterName := run.ApplicationConfig.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, "influxsrv", run.ProfileLog.Logger)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	influxScriptPath := run.Config.GetString("influxScriptPath")
	influxClient, err := clients.NewInfluxClient(influxScriptPath, url, 8086, 8088)
	if err != nil {
		return errors.New("Unable to create influx client: " + err.Error())
	}
	return influxClient.BackupDB(run.getSnapshotId(), run.ProfileLog.Logger)
}

func (run *CaptureMetricsRun) GetResults() <-chan *jobs.JobResults {
	return nil
}

func (run *CaptureMetricsRun) SetFailed(error string) {
	// No-op
}
