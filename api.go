package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/hyperpilotio/workload-profiler/runners"
	"github.com/spf13/viper"
)

// Server store the stats / data of every deployment
type Server struct {
	Config   *viper.Viper
	ConfigDB *db.ConfigDB

	JobManager *jobs.JobManager
}

// NewServer return an instance of Server struct.
func NewServer(config *viper.Viper) *Server {
	return &Server{
		Config:   config,
		ConfigDB: db.NewConfigDB(config),
	}
}

// StartServer starts a web server
func (server *Server) StartServer() error {
	if server.Config.GetString("filesPath") == "" {
		return errors.New("filesPath is not specified in the configuration file.")
	}

	if err := os.Mkdir(server.Config.GetString("filesPath"), 0755); err != nil {
		if !os.IsExist(err) {
			return errors.New("Unable to create filesPath directory: " + err.Error())
		}
	}

	//gin.SetMode("release")
	router := gin.New()

	// Global middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.LoadHTMLGlob(filepath.Join(os.Getenv("GOPATH"),
		"src/github.com/hyperpilotio/workload-profiler/ui/*.html"))
	router.Static("/static", filepath.Join(os.Getenv("GOPATH"),
		"src/github.com/hyperpilotio/workload-profiler/ui/static"))

	uiGroup := router.Group("/ui")
	{
		uiGroup.GET("", server.logUI)
		uiGroup.GET("/logs/:fileName", server.getFileLogContent)
		uiGroup.GET("/list/:status", server.getFileLogList)
	}

	calibrateGroup := router.Group("/calibrate")
	{
		calibrateGroup.POST("/:appName", server.runCalibration)
	}

	benchmarkGroup := router.Group("/benchmarks")
	{
		benchmarkGroup.POST("/:appName", server.runBenchmarks)
	}

	clusterMetricsGroup := router.Group("/clusterMetrics")
	{
		clusterMetricsGroup.POST("/apps/:appName", server.captureClusterMetrics)
	}

	sizingGroup := router.Group("/sizing")
	{
		sizingGroup.POST("/aws/:appName", server.runAWSSizing)
	}

	router.GET("/state/:runId", server.state)

	jobManager, err := jobs.NewJobManager(server.Config)
	if err != nil {
		return errors.New("Unable to create job manager: " + err.Error())
	}

	server.JobManager = jobManager

	return router.Run(":" + server.Config.GetString("port"))
}

func (server *Server) runAWSSizing(c *gin.Context) {
	appName := c.Param("appName")

	glog.V(1).Infof("Received request to run aws sizing for app: %s", appName)

	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		message := fmt.Sprintf("Unable to get application config for %s: %s", appName, err.Error())
		glog.Infof(message)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  message,
		})
		return
	}

	// TODO: We assume region is us-east-1
	region := "us-east-1"
	skipFlag := c.DefaultQuery("skipUnreserveOnFailure", "false") == "true"
	allInstances := c.DefaultQuery("allInstances", "false") == "true"
	instances := []string{}
	for _, instance := range strings.Split(c.DefaultQuery("instances", ""), ",") {
		if instance != "" {
			instances = append(instances, instance)
		}
	}

	id := ""
	if allInstances {
		var awsRegionNodeTypeConfig *models.AWSRegionNodeTypeConfig
		previousGenerations := []string{}
		nodeTypeConfig, err := server.ConfigDB.GetNodeTypeConfig(region)
		if err != nil {
			message := fmt.Sprintf("Unable to get node type for %s: %s", region, err.Error())
			glog.Infof(message)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  message,
			})
			return
		}
		awsRegionNodeTypeConfig = nodeTypeConfig
		previousGeneration, err := server.ConfigDB.GetPreviousGenerationConfig(region)
		if err != nil {
			message := fmt.Sprintf("Unable to get previous generation for %s: %s", region, err.Error())
			glog.Infof(message)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  message,
			})
			return
		}

		for _, awsNodeType := range previousGeneration.Data {
			previousGenerations = append(previousGenerations, awsNodeType.Name)
		}

		run, err := runners.NewAWSSizingAllInstancesRun(
			server.JobManager,
			applicationConfig,
			server.Config,
			awsRegionNodeTypeConfig,
			previousGenerations,
			allInstances,
			skipFlag)
		if err != nil {
			message := fmt.Sprintf("Unable to create aws sizing all instances run: " + err.Error())
			glog.Infof(message)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  message,
			})
			return
		}
		id = run.GetId()
		server.JobManager.AddJob(run)
	} else if len(instances) > 0 {
		run, err := runners.NewAWSSizingInstancesRun(
			server.JobManager,
			applicationConfig,
			server.Config,
			instances,
			skipFlag)
		if err != nil {
			message := fmt.Sprintf("Unable to create aws sizing instances run: " + err.Error())
			glog.Infof(message)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  message,
			})
			return
		}
		id = run.GetId()
		server.JobManager.AddJob(run)
	} else {
		run, err := runners.NewAWSSizingRun(
			server.JobManager,
			applicationConfig,
			server.Config,
			skipFlag)
		if err != nil {
			message := fmt.Sprintf("Unable to create aws sizing run: " + err.Error())
			glog.Infof(message)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  message,
			})
			return
		}
		id = run.GetId()
		server.JobManager.AddJob(run)

	}

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
		"runId": id,
	})
}

func (server *Server) runBenchmarks(c *gin.Context) {
	appName := c.Param("appName")

	var request struct {
		StartingIntensity int     `json:"startingIntensity" binding:"required"`
		Step              int     `json:"step" binding:"required"`
		SloTolerance      float64 `json:"sloTolerance"`
	}

	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to parse benchmark request: " + err.Error(),
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

	glog.V(1).Infof("Obtained the app config: %+v", applicationConfig)

	// TODO: Cache this
	benchmarks, err := server.ConfigDB.GetBenchmarks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to get the collection of benchmarks: " + err.Error(),
		})
		return
	}

	run, err := runners.NewBenchmarkRun(
		applicationConfig,
		benchmarks,
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

	log := run.ProfileLog
	log.Logger.Infof("Queueing benchmark job %s for app %s...", run.Id, appName)
	server.JobManager.AddJob(run)

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
		"runId": run.Id,
	})
}

func (server *Server) captureClusterMetrics(c *gin.Context) {
	appName := c.Param("appName")

	var request struct {
		LoadTesters []models.LoadTester `json:"loadTesters"`
		Benchmarks  []*struct {
			Name      string `json:"name"`
			Intensity int    `json:"intensity"`
		} `json:"benchmarks"`
		WaitTime string `json:"duration" binding:"required"`
	}

	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to parse capture cluster metrics request: " + err.Error(),
		})
		return
	}

	waitTime, err := time.ParseDuration(request.WaitTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  fmt.Errorf("Unable to parse request waitTime %s: %s", request.WaitTime, err.Error()),
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

	glog.V(1).Infof("Obtained the app config: %+v", applicationConfig)

	benchmarks, err := server.ConfigDB.GetBenchmarks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to get the collection of benchmarks: " + err.Error(),
		})
		return
	}

	if len(request.Benchmarks) == 0 {
		// Create a null array to assign to metrics run
		request.Benchmarks = append(request.Benchmarks, nil)
	}

	runs := []*runners.CaptureMetricsRun{}
	for _, loadTester := range request.LoadTesters {
		for _, benchmark := range request.Benchmarks {
			var foundBenchmark *models.Benchmark
			var benchmarkIntensity int

			if benchmark != nil {
				benchmarkIntensity = benchmark.Intensity
				for _, existingBenchmark := range benchmarks {
					if existingBenchmark.Name == benchmark.Name {
						foundBenchmark = &existingBenchmark
						break
					}
				}
				if foundBenchmark == nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": true,
						"data":  "Unable to find benchmark: " + benchmark.Name,
					})
					return
				}
			}

			run, err := runners.NewCaptureMetricsRun(
				applicationConfig,
				applicationConfig.ServiceNames[0],
				loadTester,
				foundBenchmark,
				benchmarkIntensity,
				waitTime,
				server.Config)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": true,
					"data":  "Unable to create capture metrics run: " + err.Error(),
				})
				return
			}
			runs = append(runs, run)
		}
	}

	for _, run := range runs {
		log := run.ProfileLog
		log.Logger.Infof("Queueing capture metrics job %s for app %s...", run.Id, appName)
		server.JobManager.AddJob(run)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
	})
}

func (server *Server) runCalibration(c *gin.Context) {
	appName := c.Param("appName")

	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to get application config: " + err.Error(),
		})
		return
	}

	skipFlag := c.DefaultQuery("skipUnreserveOnFailure", "false") == "true"
	run, runErr := runners.NewCalibrationRun(applicationConfig, server.Config, skipFlag)
	if runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create calibration run: " + runErr.Error(),
		})
		return
	}

	log := run.ProfileLog
	log.Logger.Infof("Running calibration job %s for app %s...", run.Id, appName)
	server.JobManager.AddJob(run)

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
		"runId": run.Id,
	})
}

func (server *Server) state(c *gin.Context) {
	runId := c.Param("runId")
	result, err := server.JobManager.FindJob(runId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable find job state: " + err.Error(),
		})
		return
	}

	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": true,
			"data":  fmt.Sprintf("Job %s not found", runId),
		})
	} else {
		c.JSON(http.StatusAccepted, gin.H{
			"error": false,
			"data":  "",
			"state": result.GetState(),
		})
	}

}
