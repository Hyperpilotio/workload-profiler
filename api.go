package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/hyperpilotio/container-benchmarks/benchmark-agent/apis"
	"github.com/hyperpilotio/deployer/log"
	"github.com/spf13/viper"
)

type Job interface {
	GetId() string
	GetApplicationConfig() *ApplicationConfig
	GetLog() *log.DeploymentLog
	Run(deploymentId string) error
}

func (server *Server) AddJob(job Job) {
	server.JobQueue <- job
}

// Server store the stats / data of every deployment
type Server struct {
	Config   *viper.Viper
	ConfigDB *ConfigDB

	Clusters       *Clusters
	JobQueue       chan Job
	UnreserveQueue chan UnreserveResult
}

// NewServer return an instance of Server struct.
func NewServer(config *viper.Viper) *Server {
	return &Server{
		Config:         config,
		JobQueue:       make(chan Job, 100),
		UnreserveQueue: make(chan UnreserveResult, 100),
		ConfigDB:       NewConfigDB(config),
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
		uiGroup.GET("/logs/:logFile", server.getDeploymentLogContent)
		// uiGroup.GET("/list/:status", server.refreshUI)
	}

	calibrateGroup := router.Group("/calibrate")
	{
		calibrateGroup.POST("/:appName", server.runCalibration)
	}

	benchmarkGroup := router.Group("/benchmarks")
	{
		benchmarkGroup.POST("/:appName", server.runBenchmarks)
	}

	deployerClient, deployerErr := NewDeployerClient(server.Config)
	if deployerErr != nil {
		return errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	server.Clusters = NewClusters(deployerClient)
	server.RunJobLoop()

	return router.Run(":" + server.Config.GetString("port"))
}

func (server *Server) RunJobLoop() {
	userId := server.Config.GetString("userId")
	go func() {
		for {
			select {
			case job := <-server.JobQueue:
				log := job.GetLog()
				defer log.LogFile.Close()

				deploymentId := ""
				runId := job.GetId()
				log.Logger.Infof("Waiting until %s job is completed...", runId)
				for {
					result := <-server.Clusters.ReserveDeployment(server.Config,
						job.GetApplicationConfig(), runId, userId, log.Logger)
					if result.Err != "" {
						log.Logger.Warningf("Unable to reserve deployment for job: " + result.Err)
						// Try reserving again after sleep
						time.Sleep(60 * time.Second)
					} else {
						deploymentId = result.DeploymentId
						log.Logger.Infof("Deploying job %s with deploymentId is %s", runId, deploymentId)
						break
					}
				}

				// TODO: Allow multiple workers to process job
				log.Logger.Infof("Running %s job", job.GetId())
				if err := job.Run(deploymentId); err != nil {
					// TODO: Store the error state in a map and display/return job status
					log.Logger.Errorf("Unable to run %s job: %s", runId, err)
					server.Clusters.SetState(runId, FAILED)
				} else {
					server.Clusters.SetState(runId, AVAILABLE)
				}

				switch server.Clusters.GetState(runId) {
				case AVAILABLE:
					unreserveResult := <-server.Clusters.UnreserveDeployment(runId, log.Logger)
					if unreserveResult.Err != "" {
						log.Logger.Errorf("Unable to unreserve %s deployment: %s", runId, unreserveResult.Err)
					} else {
						server.UnreserveQueue <- unreserveResult
					}
				}
			case unreserveResult := <-server.UnreserveQueue:
				if unreserveResult.RunId != "" {
					server.Clusters.DeleteCluster(unreserveResult.RunId)
				}
			}
		}
	}()
}

func (server *Server) runBenchmarks(c *gin.Context) {
	appName := c.Param("appName")

	var request struct {
		StartingIntensity int `json:"startingIntensity" binding:"required"`
		Step              int `json:"step" binding:"required"`
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

	runId, err := generateId(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to generate run Id: " + err.Error(),
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

	run, err := NewBenchmarkRun(
		applicationConfig,
		benchmarks,
		request.StartingIntensity,
		request.Step,
		0, // TODO: Replace with some real value when needed
		server.Config)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create benchmarks run: " + err.Error(),
		})
		return
	}

	server.AddJob(run)

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

	runId, err := generateId(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to generate run Id: " + err.Error(),
		})
		return
	}

	run, runErr := NewCalibrationRun(runId, applicationConfig, server.Config)
	if runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create calibration run: " + runErr.Error(),
		})
		return
	}

	run.ProfileRun.DeploymentLog.Logger.Infof("Running %s job...", runId)
	server.AddJob(run)

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
	})
}
