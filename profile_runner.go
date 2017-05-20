package main

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-resty/resty"
	"github.com/golang/glog"
	"github.com/hyperpilotio/go-utils/funcs"
	"github.com/nu7hatch/gouuid"
	"github.com/spf13/viper"
)

type ProfileRun struct {
	ServiceUrls          map[string]string
	DeployerClient       *DeployerClient
	BenchmarkAgentClient *BenchmarkAgentClient
	DeploymentId         string
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

func NewRun(deploymentId string, config *viper.Viper) (*ProfileRun, error) {
	deployerClient, deployerErr := NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	run := &ProfileRun{
		ServiceUrls:    make(map[string]string),
		DeployerClient: deployerClient,
		DeploymentId:   deploymentId,
	}

	url, urlErr := run.getServiceUrl("benchmark-agent")
	if urlErr != nil {
		return nil, errors.New("Unable to get benchmark-agent url: " + urlErr.Error())
	}

	benchmarkAgentClient, benchmarkAgentErr := NewBenchmarkAgentClient(url)
	if benchmarkAgentErr != nil {
		return nil, errors.New("Unable to create new benchmark agent client: " + benchmarkAgentErr.Error())
	}

	run.BenchmarkAgentClient = benchmarkAgentClient
	return run, nil
}

func (run *ProfileRun) cleanupStage(stage *Stage) error {
	for _, benchmark := range stage.Benchmarks {
		if err := run.BenchmarkAgentClient.DeleteBenchmark(benchmark.Name); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

	return nil
}

func (run *ProfileRun) setupStage(stage *Stage) error {
	for _, benchmark := range stage.Benchmarks {
		if err := run.BenchmarkAgentClient.CreateBenchmark(&benchmark); err != nil {
			return fmt.Errorf("Unable to create benchmark %s: %s",
				benchmark.Name, err.Error())
		}
	}

	return nil
}

func sendHTTPRequest(baseUrl string, request HTTPRequest) (*resty.Response, error) {
	var response *resty.Response
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, request.UrlPath)
	glog.V(1).Infof("Sending HTTP request with URL: %s", u.String())
	switch strings.ToUpper(request.HTTPMethod) {
	case "GET":
		response, err = resty.R().Get(u.String())
	case "POST":
		if len(request.FormData) > 0 {
			response, err = resty.R().
				SetFormData(request.FormData).
				Post(u.String())
		} else {
			response, err = resty.R().
				SetBody(request.Body).
				Post(u.String())
		}
	default:
		return nil, errors.New("Unknown HTTP method: " + request.HTTPMethod)
	}

	if err != nil {
		return nil, err
	}

	return response, nil
}

func (run *ProfileRun) getServiceUrl(name string) (string, error) {
	// We cache the results of asking for the service url, if cache miss go fetch
	// from the deployer.
	if url, ok := run.ServiceUrls[name]; !ok {
		if url, err := run.DeployerClient.GetServiceUrl(run.DeploymentId, name); err != nil {
			return "", errors.New("Unable to get url from deployer: " + err.Error())
		} else {
			run.ServiceUrls[name] = url
			return url, nil
		}
	} else {
		return url, nil
	}
}

func waitUntilLoadTestFinishes(url string, stageId string) error {
	// TODO: Allow timeout to be configurable
	return funcs.LoopUntil(time.Minute*30, time.Second*30, func() (bool, error) {
		request := HTTPRequest{
			HTTPMethod: "GET",
			UrlPath:    "/api/benchmarks/" + stageId,
		}
		response, err := sendHTTPRequest(url, request)
		if err != nil {
			return false, err
		} else if response.StatusCode() == 404 {
			return true, nil
		}

		return false, nil
	})
}

func (run *ProfileRun) runLoadTestController(stageId string, controller *LoadTestController) error {
	url, urlErr := run.getServiceUrl(controller.ServiceName)
	if urlErr != nil {
		return fmt.Errorf("Unable to retrieve service url [%s]: %s",
			controller.ServiceName, urlErr.Error())
	}
	body := make(map[string]interface{})

	if controller.Initialize != nil {
		body["initialize"] = controller.Initialize
	}

	if controller.Cleanup != nil {
		body["cleanup"] = controller.Cleanup
	}

	body["loadTest"] = controller.LoadTest
	body["stageId"] = stageId

	request := HTTPRequest{
		HTTPMethod: "POST",
		UrlPath:    "/api/benchmarks",
		Body:       body,
	}

	if response, err := sendHTTPRequest(url, request); err != nil {
		return fmt.Errorf("Unable to send workload request for load test %v: %s", request, err.Error())
	} else if response.StatusCode() >= 300 {
		return fmt.Errorf("Unexpected response code: %d, body: %s", response.StatusCode(), response.String())
	}

	if err := waitUntilLoadTestFinishes(url, stageId); err != nil {
		return errors.New("Unable to wait until load test to finish: " + err.Error())
	}

	return nil
}

func (run *ProfileRun) runLocustController(stageId string, controller *LocustController) error {
	return nil
}

func (run *ProfileRun) runAppLoadTest(stageId string, controller LoadController) error {
	if controller.LoadTestController != nil {
		return run.runLoadTestController(stageId, controller.LoadTestController)
	} else if controller.LocustController != nil {
		return run.runLocustController(stageId, controller.LocustController)
	}

	return errors.New("No controller found")
}

func (run *ProfileRun) runStageBenchmark(deployment string, stage *Stage) (*StageResult, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return nil, errors.New("Unable to generate stage id: " + err.Error())
	}
	stageId := u4.String()
	st := time.Now()
	startTime := st.Format(time.RFC3339)
	err = run.runAppLoadTest(stageId, stage.AppLoadTest)
	results := &StageResult{
		Id:        stageId,
		StartTime: startTime,
	}

	return results, err
}

func (run *ProfileRun) RunProfile(config *viper.Viper, profile *Profile) (*ProfileResults, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return nil, errors.New("Unable to generate profile id: " + err.Error())
	}

	profileId := u4.String()
	results := &ProfileResults{
		Id: profileId,
	}

	for _, stage := range profile.Stages {
		if err := run.setupStage(&stage); err != nil {
			run.cleanupStage(&stage)
			return nil, errors.New("Unable to setup stage: " + err.Error())
		}

		// TODO: Store stage results
		if result, err := run.runStageBenchmark(run.DeploymentId, &stage); err != nil {
			run.cleanupStage(&stage)
			return nil, errors.New("Unable to run stage benchmark: " + err.Error())
		} else {
			et := time.Now()
			result.EndTime = et.Format(time.RFC3339)
			results.addProfileResult(*result)
		}

		if err := run.cleanupStage(&stage); err != nil {
			return nil, errors.New("Unable to clean stage: " + err.Error())
		}
	}

	if err := PersistData(config, run.DeploymentId, results); err != nil {
		return nil, errors.New("Unable to save profile results to db: " + err.Error())
	}

	return results, nil
}

func (pr *ProfileResults) addProfileResult(sr StageResult) []StageResult {
	pr.StageResults = append(pr.StageResults, sr)
	return pr.StageResults
}
