package main

import (
	"bytes"
	"errors"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"encoding/json"
	"net/http"
)

// Server store the stats / data of every deployment
type Server struct {
	Config *viper.Viper
	mutex  sync.Mutex
}

// NewServer return an instance of Server struct.
func NewServer(config *viper.Viper) *Server {
	return &Server{
		Config: config,
	}
}

// StartServer start a web server
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

	profilerGroup := router.Group("/profilers")
	{
		profilerGroup.POST("", server.createProfileRun)
	}

	return router.Run(":" + server.Config.GetString("port"))
}

func (server *Server) createProfileRun(c *gin.Context) {
	var profile Profile
	if err := c.BindJSON(&profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Error deserializing benchmark: " + string(err.Error()),
		})
		return
	}

	if profile.Stages[0].ContainerBenchmarks[0].Name == "busycpu" {
		url := server.Config.GetString("benchmark-agent")

		b, err := json.Marshal(profile.Stages[0].ContainerBenchmarks)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": true,
				"data":  "Unable to Marshal Benchmarks: " + err.Error(),
			})
			return
		}

		var jsonStr = []byte(string(b))
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
	}

	c.JSON(http.StatusAccepted, gin.H{
		"error": false,
	})
}
