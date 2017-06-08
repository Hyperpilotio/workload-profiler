package main

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
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

// StartServer start a web server
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
		DeploymentId      string `json:"deploymentId" binding:"required"`
		StartingIntensity int    `json:"startingIntensity"`
		Step              int    `json:"step"`
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

	// TODO: Cache this
	benchmarks, err := server.ConfigDB.GetBenchmarks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to get benchmarks: " + err.Error(),
		})
		return
	}

	run, runErr := NewBenchmarkRun(
		applicationConfig,
		benchmarks,
		request.DeploymentId,
		request.StartingIntensity,
		request.Step,
		server.Config)

	if runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to create benchmarks run: " + err.Error(),
		})
		return
	}

	if err = run.Run(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": true,
			"data":  "Unable to run benchmarks: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
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
	})
}
