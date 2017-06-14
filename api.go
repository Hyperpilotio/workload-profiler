package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/spf13/viper"
)

// Server store the stats / data of every deployment
type Server struct {
	Config   *viper.Viper
	ConfigDB *ConfigDB

	// Maps appName to deployment name
	LoadTestApps map[string]string

	mutex sync.Mutex
}

// NewServer return an instance of Server struct.
func NewServer(config *viper.Viper) *Server {
	return &Server{
		Config:       config,
		ConfigDB:     NewConfigDB(config),
		LoadTestApps: make(map[string]string),
	}
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
		DeploymentId      string `json:"deploymentId" binding:"required"`
		StartingIntensity int    `json:"startingIntensity" binding:"required"`
		Step              int    `json:"step" binding:"required"`
	}

	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to parse benchmark request: " + err.Error(),
		})
		return
	}

	server.mutex.Lock()
	deploymentId, ok := server.LoadTestApps[appName]
	server.mutex.Unlock()

	if !ok {
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

	run, err := NewBenchmarkRun(
		applicationConfig,
		benchmarks,
		request.DeploymentId,
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

	server.mutex.Lock()
	deploymentId, ok := server.LoadTestApps[appName]
	server.mutex.Unlock()

	if !ok {
		newDeploymentId, err := server.createDeployment(appName)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  "Unable to create deployment: " + err.Error(),
			})
			return
		}
		deploymentId = *newDeploymentId
	}

	if deploymentId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Empty deployment id found",
		})
		return
	}

	server.mutex.Lock()
	server.LoadTestApps[appName] = deploymentId
	server.mutex.Unlock()

	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to get application config: " + err.Error(),
		})
		return
	}

	run, runErr := NewCalibrationRun(deploymentId, applicationConfig, server.Config)
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

func (server *Server) createDeployment(appName string) (*string, error) {
	applicationConfig, err := server.ConfigDB.GetApplicationConfig(appName)
	if err != nil {
		return nil, errors.New("Unable to get application config: " + err.Error())
	}

	deployJSON = strings.Replace(deployJSON, "#USER_ID#", server.Config.GetString("userId"), 1)
	deployJSON = strings.Replace(deployJSON, "#NAME#", appName, 1)
	deployJSON = strings.Replace(deployJSON, "#REGION#", "us-east-1", 1)
	deployJSON = strings.Replace(deployJSON, "#NODE_MAPPING#", "[]", 1)
	deployJSON = strings.Replace(deployJSON, "#NODES#", "[]", 1)
	deployJSON = strings.Replace(deployJSON, "#BASE#", applicationConfig.Base, 1)

	taskDefinitions := make([]interface{}, 0)
	for _, loadTests := range applicationConfig.TaskDefinitions["loadTests"].([]interface{}) {
		taskDefinitions = append(taskDefinitions, loadTests)
	}
	for _, applications := range applicationConfig.TaskDefinitions["applications"].([]interface{}) {
		taskDefinitions = append(taskDefinitions, applications)
	}

	b, jsonErr := json.Marshal(taskDefinitions)
	if jsonErr != nil {
		return nil, errors.New("Unable to marshal taskDefinitions to json: " + jsonErr.Error())
	}
	deployJSON = strings.Replace(deployJSON, "#TASK_DEFINITIONS#", string(b), 1)

	deployerClient, deployerErr := NewDeployerClient(server.Config)
	if deployerErr != nil {
		return nil, errors.New("Unable to create new deployer client: " + deployerErr.Error())
	}

	deploymentId, createErr := deployerClient.CreateDeployment(deployJSON)
	if createErr != nil {
		return nil, errors.New("Unable to create deployment: " + createErr.Error())
	}

	return deploymentId, nil
}
