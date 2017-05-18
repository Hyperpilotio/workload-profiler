package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type LoadController interface {
	GetType() string
}

type Command struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type LoadTestController struct {
	LoadController
	ServiceName string   `json:"serviceName"`
	Initialize  *Command `json:"initialize"`
	LoadTest    *Command `json:"load"`
	Cleanup     *Command `json:"cleanup"`
}

func (controller LoadTestController) GetType() string {
	return "loadtest"
}

type LocustController struct {
	LoadController
	Count     int    `json:"count"`
	HatchRate int    `json:"hatchRate"`
	Duration  string `json:"duration"`
}

func (controller LocustController) GetType() string {
	return "locust"
}

type HTTPRequest struct {
	HTTPMethod string                 `json:"method"`
	Component  string                 `json:"component"`
	UrlPath    string                 `json:"path"`
	Body       map[string]interface{} `json:"body"`
	FormData   map[string]string      `json:"formData"`
	Duration   string                 `json:"duration"`
}

type Stage struct {
	Benchmarks  []apis.Benchmark `json:"benchmarks"`
	AppLoadTest LoadController   `json:"loadTest"`
}

type Profile struct {
	Deployment string  `json:"deployment"`
	Stages     []Stage `json:"stages"`
}
