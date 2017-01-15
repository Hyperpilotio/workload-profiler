package main

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
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

	server.mutex.Lock()
	defer server.mutex.Unlock()

	if results, err := RunProfile(server.Config, &profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": true,
			"data":  "Unable to run profile: " + err.Error(),
		})
		return
	} else {

		c.JSON(http.StatusAccepted, gin.H{
			"error": false,
			"data":  results,
		})
	}
}
