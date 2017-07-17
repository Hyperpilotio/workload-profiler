package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/db"
	"github.com/hyperpilotio/workload-profiler/runners"
	"github.com/spf13/viper"
)

// Server store the stats / data of every deployment
type Server struct {
	Config   *viper.Viper
	ConfigDB *db.ConfigDB

	Clusters *Clusters
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

	deployerClient, deployerErr := clients.NewDeployerClient(server.Config)
	if deployerErr != nil {
		return errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	if clusters, err := NewClusters(deployerClient, server.Config); err != nil {
		return errors.New("Unable to create clusters object: " + err.Error())
	} else {
		server.Clusters = clusters
	}

	return router.Run(":" + server.Config.GetString("port"))
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

	run, err := runners.NewAWSSizingRun(applicationConfig)
	if err != nil {

	}
}

func (server *Server) runBenchmarks(c *gin.Context) {
	appName := c.Param("appName")

	var request struct {
		DeploymentId      string  `json:"deploymentId" binding:"required"`
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

	if request.DeploymentId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Empty deployment id found",
		})
		return
	}

	glog.V(1).Infof("Target app: %s, deployment Id: %s", appName, request.DeploymentId)

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

	cluster := &cluster{
		deploymentId: request.DeploymentId,
		runId:        run.Id,
		state:        AVAILABLE,
		created:      time.Now(),
	}
	server.Clusters.Deployments = append(server.Clusters.Deployments, cluster)

	log := run.ProfileLog
	defer log.LogFile.Close()

	log.Logger.Infof("Running %s job for app %s...", run.Id, appName)
	if err = run.Run(); err != nil {
		cluster.state = FAILED
		log.Logger.Errorf("Failed to run profiling benchmarks for app %s: %s", appName, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to run profiling benchmarks: " + err.Error(),
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

	run, runErr := runners.NewCalibrationRun(request.DeploymentId, applicationConfig, server.Config)
	if runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create calibration run: " + runErr.Error(),
		})
		return
	}

	cluster := &cluster{
		deploymentId: request.DeploymentId,
		runId:        run.Id,
		state:        AVAILABLE,
		created:      time.Now(),
	}
	server.Clusters.Deployments = append(server.Clusters.Deployments, cluster)

	log := run.ProfileLog
	defer log.LogFile.Close()

	log.Logger.Infof("Running %s job for app %s...", run.Id, appName)
	if err = run.Run(); err != nil {
		cluster.state = FAILED
		log.Logger.Errorf("Failed to run profiling calibrate for app %s: %s", appName, err.Error())
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
