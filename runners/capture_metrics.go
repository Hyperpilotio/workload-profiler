package runners

import (
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type CaptureMetricsRun struct {
	ProfileRun

	LoadTester models.LoadTester
}

func NewCaptureMetricsRun(
	applicationConfig *models.ApplicationConfig,
	loadTester models.LoadTester,
	config *viper.Viper) *CaptureMetricsRun {

	deployerClient, deployerErr := clients.NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	log, logErr := log.NewLogger(config.GetString("filesPath"), id)
	if logErr != nil {
		return nil, errors.New("Error creating deployment logger: " + logErr.Error())
	}

	id, err := generateId("capturemetrics")
	if err != nil {
		return nil, errors.New("Unable to generate Id for capture metrics run: " + err.Error())
	}
	glog.V(1).Infof("Created new capture metrics run with id: %s", id)

	return &CaptureMetricsRun{
		ProfileRun: ProfileRun{
			Id:                id,
			ApplicationConfig: applicationConfig,
			DeployerClient:    deployerClient,
			ProfileLog:        log,
			Created:           time.Now(),
			DirectJob:         false,
		},
		LoadTester: loadTester,
	}
}

func (run *CaptureMetricsRun) runSlowCookerController(slowCookerController *models.SlowCookerController) error {
	loadTesterName := run.LoadTester.Name
	url, urlErr := run.DeployerClient.GetServiceUrl(run.DeploymentId, loadTesterName, run.ProfileLog.Logger)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s", loadTesterName, urlErr.Error())
	}

	client := clients.SlowCookerClient{}
	_, err := client.RunBenchmark(url, run.Id, slowCookerController.AppLoad.Concurrency, slowCookerController, run.ProfileLog.logger)
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

	if err := run.runApplicationLoadTest(); err != nil {
		return fmt.Errorf("Unable to run load controller: " + err.Error())
	}

	//run.snapshotInfluxData()
	return nil
}
