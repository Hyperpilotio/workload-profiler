package main

import (
	"encoding/json"
	"errors"
	"path"
	"strings"

	"github.com/go-resty/resty"
	"github.com/nu7hatch/gouuid"
	"github.com/spf13/viper"
)

type ProfileResults struct {
	StageResults []StageResult
}

type StageResult struct {
	Id string
}

func setupStage(stage *Stage, benchmarkAgentClient *BenchmarkAgentClient) error {
	for _, containerBenchmark := range stage.ContainerBenchmarks {
		// TODO: Add delete benchmark on last stage's setup
		if err := benchmarkAgentClient.CreateBenchmark(&containerBenchmark); err != nil {
			return errors.New("Unable to run create benchmark: " + err.Error())
		}
	}

	return nil
}

func sendWorkloadRequest(url string, request WorkloadBenchmarkRequest, stageId string) error {
	var response *resty.Response
	var err error
	requestUrl := path.Join(url, request.UrlPath)
	switch strings.ToUpper(request.HTTPMethod) {
	case "GET":
		response, err = resty.R().Get(requestUrl)
	case "POST":
		if stageId != "" {
			request.Body["stage_id"] = stageId
		}
		if jsonBody, err := json.Marshal(request.Body); err != nil {
			return errors.New("Unable to marshal request body: " + err.Error())
		} else {
			response, err = resty.R().SetBody(string(jsonBody)).Post(requestUrl)
		}
	default:
		return errors.New("Unknown HTTP method: " + request.HTTPMethod)
	}

	if err != nil {
		return err
	}

	if response.StatusCode() >= 300 {
		return errors.New("Unexpected response code: " + string(response.StatusCode()))
	}

	return nil
}

func runStageBenchmark(deployment string, stage *Stage, deployerClient *DeployerClient) (*StageResult, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return nil, errors.New("Unable to generate stage id: " + err.Error())
	}
	stageId := u4.String()
	componentUrls := make(map[string]string)
	benchmark := stage.WorkloadBenchmark
	results := &StageResult{
		Id: stageId,
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
			return nil, errors.New("Unable to send workload request: " + err.Error())
		}
	}

	return results, nil
}

func RunProfile(config *viper.Viper, profile *Profile) (*ProfileResults, error) {
	deployerClient := NewDeployerClient(config)
	benchmarkAgentClient := NewBenchmarkAgentClient(config)

	// TODO: Verify deployment has been deployed in Deployer
	results := &ProfileResults{}

	for _, stage := range profile.Stages {
		if err := setupStage(&stage, benchmarkAgentClient); err != nil {
			return nil, errors.New("Unable to setup stage: " + err.Error())
		}

		if _, err := runStageBenchmark(profile.Deployment, &stage, deployerClient); err != nil {
			return nil, errors.New("Unable to run stage benchmark: " + err.Error())
		}
	}

	return results, nil
}
