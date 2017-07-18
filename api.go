package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/hyperpilotio/go-utils/log"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/hyperpilotio/workload-profiler/runners"
	"github.com/spf13/viper"
)

type Job interface {
	GetId() string
	GetApplicationConfig() *models.ApplicationConfig
	GetLog() *log.FileLog
	Run(deploymentId string) error
	GetState() string
	SetState(state string)
	GetSummary() runners.RunSummary
}

// Server store the stats / data of every deployment
type Server struct {
	Config   *viper.Viper
	ConfigDB *db.ConfigDB

	Clusters       *Clusters
	JobQueue       chan Job
	Jobs           map[string]Job
	UnreserveQueue chan UnreserveResult
	mutex          sync.Mutex
}

func (server *Server) AddJob(job Job) {
	server.Jobs[job.GetId()] = job
	server.JobQueue <- job
}

// NewServer return an instance of Server struct.
func NewServer(config *viper.Viper) *Server {
	return &Server{
		Config:         config,
		ConfigDB:       db.NewConfigDB(config),
		JobQueue:       make(chan Job, 100),
		UnreserveQueue: make(chan UnreserveResult, 100),
		Jobs:           make(map[string]Job),
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

	deployerClient, deployerErr := clients.NewDeployerClient(server.Config)
	if deployerErr != nil {
		return errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	if clusters, err := NewClusters(deployerClient, server.Config); err != nil {
		return errors.New("Unable to create clusters object: " + err.Error())
	} else {
		server.Clusters = clusters
	}

	if err := server.Clusters.ReloadClusterState(); err != nil {
		return errors.New("Unable to reload cluster state: " + err.Error())
	}

	server.RunJobLoop()

	return router.Run(":" + server.Config.GetString("port"))
}

func (server *Server) RunJobLoop() {
	userId := server.Config.GetString("userId")
	go func() {
		for {
			select {
			case job := <-server.JobQueue:
				job.SetState(runners.RESERVING)
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

				job.SetState(runners.RUNNING)
				// TODO: Allow multiple jobs to run
				log.Logger.Infof("Running %s job", job.GetId())
				defer log.LogFile.Close()
				if err := job.Run(deploymentId); err != nil {
					// TODO: Store the error state in a map and display/return job status
					log.Logger.Errorf("Unable to run %s job: %s", runId, err)
					job.SetState(runners.FAILED)
				} else {
					job.SetState(runners.FINISHED)
				}

				unreserveResult := <-server.Clusters.UnreserveDeployment(runId, log.Logger)
				if unreserveResult.Err != "" {
					log.Logger.Errorf("Unable to unreserve %s deployment: %s", runId, unreserveResult.Err)
				}
			}
		}
	}()
}

func (server *Server) runAWSSizing(c *gin.Context) {
	appName := c.Param("appName")

	glog.V(1).Infof("Received request to run aws sizing for app: %s", appName)

	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  fmt.Sprintf("Unable to get application config for %s: %s", appName, err.Error()),
		})
		return
	}

	run, err := runners.NewAWSSizingRun(applicationConfig, server.Config)
	if err != nil {

	}

	log := run.ProfileLog
	log.Logger.Infof("Queueing aws sizing job %s for app %s...", run.Id, appName)
	server.AddJob(run)

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
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

	run, runErr := runners.NewCalibrationRun(applicationConfig, server.Config)
	if runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create calibration run: " + runErr.Error(),
		})
		return
	}

	log := run.ProfileLog
	log.Logger.Infof("Running calibration job %s for app %s...", run.Id, appName)
	server.AddJob(run)

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
		"data":  "",
	})
}
