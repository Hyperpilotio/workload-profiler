package main

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
	"github.com/spf13/viper"
)

// Server store the stats / data of every deployment
type Server struct {
	Config   *viper.Viper
	ConfigDB *ConfigDB
	mutex    sync.Mutex
}

// NewServer return an instance of Server struct.
func NewServer(config *viper.Viper) *Server {
	return &Server{
		Config:   config,
		ConfigDB: NewConfigDB(config),
	}
}

type BenchmarkSet struct {
	Name       string
	Benchmarks []apis.Benchmark
	AgentMap   map[string]string
}

// StartServer starts a web server
func (server *Server) StartServer() error {
	//gin.SetMode("release")
	router := gin.New()

	// Global middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	calibrateGroup := router.Group("/calibrate")
	{
		calibrateGroup.POST("/:appName", server.runCalibration)
	}

	benchmarkGroup := router.Group("/benchmarks")
	{
		benchmarkGroup.POST("/:appName", server.runBenchmarks)
	}

	return router.Run(":" + server.Config.GetString("port"))
}

func (server *Server) runBenchmarks(c *gin.Context) {
	appName := c.Param("appName")

	var request struct {
		DeploymentId      string  `json:"deploymentId" binding:"required"`
		StartingIntensity int     `json:"startingIntensity" binding:"required"`
		Step              int     `json:"step" binding:"required"`
		SloTolerance      float64 `json:"sloTolerance" binding:"required"`
	}

	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to parse benchmark request: " + err.Error(),
		})
		return
	}

	if request.DeploymentId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Empty deployment id found",
		})
		return
	}

	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to get application config for app " + appName + ": " + err.Error(),
		})
		return
	}

	// TODO: Cache this
	benchmarks, err := server.ConfigDB.GetBenchmarks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to get the collection of benchmarks: " + err.Error(),
		})
		return
	}

	benchmarkMappings, err := server.ConfigDB.GetBenchmarkMappings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to get benchmark-to-agent mappings: " + err.Error(),
		})
		return
	}

	// Construct BenchmarkSets from benchmarks and benchmarkMappings
	var benchmarkSets []BenchmarkSet
	for _, benchmarkMapping := range benchmarkMappings {
		benchmarkSet := BenchmarkSet{
			Name:       benchmarkMapping.Name,
			AgentMap:   make(map[string]string),
			Benchmarks: []apis.Benchmark{},
		}

		for _, agentMapping := range benchmarkMapping.AgentMapping {
			benchmarkSet.AgentMap[agentMapping.BenchmarkName] = agentMapping.AgentId
			for _, benchmark := range benchmarks {
				if benchmark.Name == agentMapping.BenchmarkName {
					if benchmark.HostConfig != nil {
						benchmark.HostConfig.TargetHost = server.Config.GetString("benchmarkTargetHost")
						glog.V(1).Infof("Replaced target host for benchmark %s with %s",
							benchmark.Name, benchmark.HostConfig.TargetHost)
					}
					benchmarkSet.Benchmarks = append(benchmarkSet.Benchmarks, benchmark)
				}
			}
		}
		benchmarkSets = append(benchmarkSets, benchmarkSet)
	}

	run, err := NewBenchmarkRun(
		applicationConfig,
		benchmarkSets,
		request.DeploymentId,
		request.StartingIntensity,
		request.Step,
		request.SloTolerance,
		server.Config)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create benchmarks run: " + err.Error(),
		})
		return
	}

	if err = run.Run(); err != nil {
		glog.Warningf("Failed to run benchmarks for app %s: %s", appName, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to run benchmarks: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
	})
}

func (server *Server) runCalibration(c *gin.Context) {
	appName := c.Param("appName")

	var request struct {
		DeploymentId string `json:"deploymentId" binding:"required"`
	}

	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to parse calibration request: " + err.Error(),
		})
		return
	}

	if request.DeploymentId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Empty request id found",
		})
		return
	}

	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to get application config: " + err.Error(),
		})
		return
	}

	run, runErr := NewCalibrationRun(request.DeploymentId, applicationConfig, server.Config)
	if runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create calibration run: " + runErr.Error(),
		})
		return
	}

	if err = run.Run(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to run calibration: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
	})
}
