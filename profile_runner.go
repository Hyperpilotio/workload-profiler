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
	"github.com/nu7hatch/gouuid"
	"github.com/spf13/viper"
)

type ProfileResults struct {
	StageResults []StageResult
}

type StageResult struct {
	Id        string
	StartTime string
	EndTime   string
}

func cleanupStage(stage *Stage, benchmarkAgentClient *BenchmarkAgentClient) error {
	for _, containerBenchmark := range stage.ContainerBenchmarks {
		if err := benchmarkAgentClient.DeleteBenchmark(containerBenchmark.Name); err != nil {
			return fmt.Errorf("Unable to delete last stage's benchmark %s: %s",
				containerBenchmark.Name, err.Error())
		}
	}

	return nil
}

func setupStage(stage *Stage, benchmarkAgentClient *BenchmarkAgentClient) error {
	for _, containerBenchmark := range stage.ContainerBenchmarks {
		if err := benchmarkAgentClient.CreateBenchmark(&containerBenchmark); err != nil {
			return fmt.Errorf("Unable to create benchmark %s: %s",
				containerBenchmark.Name, err.Error())
		}
	}

	return nil
}

func sendWorkloadRequest(urlString string, request WorkloadBenchmarkRequest, stageId string) error {
	var response *resty.Response
	u, err := url.Parse(urlString)
	if err != nil {
		return errors.New("Unable to parse url: " + err.Error())
	}

	u.Path = path.Join(u.Path, request.UrlPath)
	switch strings.ToUpper(request.HTTPMethod) {
	case "GET":
		response, err = resty.R().Get(u.String())
	case "POST":
		if stageId != "" {
			request.Body["stage_id"] = stageId
		}
		response, err = resty.R().
			SetFormData(request.Body).
			Post(u.String())
	default:
		return errors.New("Unknown HTTP method: " + request.HTTPMethod)
	}

	if err != nil {
		return err
	}

	if response.StatusCode() >= 300 {
		return fmt.Errorf(
			"Unexpected response code: %d, body: %s",
			response.StatusCode(),
			response.String())
	}

	return nil
}

func runStageBenchmark(deployment string, stage *Stage, deployerClient *DeployerClient) (*StageResult, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return nil, errors.New("Unable to generate stage id: " + err.Error())
	}
	stageId := u4.String()
	st := time.Now()
	startTime := st.Format(time.RFC3339)
	componentUrls := make(map[string]string)
	benchmark := stage.WorkloadBenchmark
	results := &StageResult{
		Id:        stageId,
		StartTime: startTime,
	}

	for _, benchmarkRequest := range benchmark.Requests {
		if _, ok := componentUrls[benchmarkRequest.Component]; !ok {
			url, urlErr := deployerClient.GetContainerUrl(deployment, benchmarkRequest.Component)
			if urlErr != nil {
				return nil, errors.New("Unable to get container url: " + err.Error())
			}
			componentUrls[benchmarkRequest.Component] = url
		}

		url := componentUrls[benchmarkRequest.Component]
		requestStageId := ""
		if benchmarkRequest.StartBenchmark {
			requestStageId = stageId
		}
		if err := sendWorkloadRequest(url, benchmarkRequest, requestStageId); err != nil {
			return nil, fmt.Errorf("Unable to send workload request %v: %s", benchmarkRequest, err.Error())
		}
		if benchmarkRequest.Duration != "" {
			if duration, err := time.ParseDuration(benchmarkRequest.Duration); err != nil {
				return nil, fmt.Errorf(
					"Unable to parse duration %s: %s", benchmarkRequest.Duration, err.Error())
			} else {
				timer := time.NewTimer(duration)
				glog.Infof("Waiting for %s before moving to next request", duration.String())
				// TODO: We should run these in a go func so we can cancel a timer in flight
				<-timer.C
			}
		}
	}

	return results, nil
}

func RunProfile(config *viper.Viper, profile *Profile) (*ProfileResults, error) {
	deployerClient, deployerErr := NewDeployerClient(config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	url, urlErr := deployerClient.GetContainerUrl(profile.Deployment, "benchmark-agent")
	if urlErr != nil {
		return nil, errors.New("Unable to get benchmark-agent url from deployer: " + urlErr.Error())
	}

	benchmarkAgentClient, benchmarkAgentErr := NewBenchmarkAgentClient(url)
	if benchmarkAgentErr != nil {
		return nil, errors.New("Unable to create new benchmark agent client: " + benchmarkAgentErr.Error())
	}

	// TODO: Verify deployment has been deployed in Deployer
	results := &ProfileResults{}
	for _, stage := range profile.Stages {
		if err := setupStage(&stage, benchmarkAgentClient); err != nil {
			cleanupStage(&stage, benchmarkAgentClient)
			return nil, errors.New("Unable to setup stage: " + err.Error())
		}

		// TODO: Store stage results
		if result, err := runStageBenchmark(profile.Deployment, &stage, deployerClient); err != nil {
			cleanupStage(&stage, benchmarkAgentClient)
			return nil, errors.New("Unable to run stage benchmark: " + err.Error())
		} else {
			et := time.Now()
			result.EndTime = et.Format(time.RFC3339)
			results.addProfileResult(*result)
		}

		if err := cleanupStage(&stage, benchmarkAgentClient); err != nil {
			return nil, errors.New("Unable to clean stage: " + err.Error())
		}
	}

	return results, nil
}

func (pr *ProfileResults) addProfileResult(sr StageResult) []StageResult {
	pr.StageResults = append(pr.StageResults, sr)
	return pr.StageResults
}
