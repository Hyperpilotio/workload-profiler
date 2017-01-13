package main

import (
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
)

type ContainerBenchmark struct {
	Name      string         `json:"name"`
	Count     int            `json:"count"`
	Resources apis.Resources `json:"resources"`
	Image     string         `json:"image"`
	Command   []string       `json:"command"`
}

type Setup struct {
	Deployer string `json:"deployer"`
}

type Locust struct {
	Hatch int `json:"count"`
	Max   int `json:"max"`
}

type Stages []struct {
	ContainerBenchmarks []ContainerBenchmark `json:"containerBenchmarks"`
	Locust              `json:"locust"`
	Duration            string `json:"duration"`
}

type Profile struct {
	Setup  `json:"setup"`
	Stages `json:"stages"`
}
