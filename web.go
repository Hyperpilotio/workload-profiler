package main

import (
	"bufio"
	"net/http"
	"os"
	"path"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

type DeploymentLog struct {
	DeploymentId string
	RunId        string
	Status       string
	Create       time.Time
}

type DeploymentLogs []*DeploymentLog

func (d DeploymentLogs) Len() int { return len(d) }
func (d DeploymentLogs) Less(i, j int) bool {
	return d[i].Create.Before(d[j].Create)
}
func (d DeploymentLogs) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

func (server *Server) logUI(c *gin.Context) {
	DeploymentLogs, _ := server.getDeploymentLogs(c)
	c.HTML(http.StatusOK, "index.html", gin.H{
		"error": false,
		"logs":  DeploymentLogs,
	})
}

func (server *Server) getDeploymentLogContent(c *gin.Context) {
	fileName := c.Param("fileName")
	logPath := path.Join(server.Config.GetString("filesPath"), "log", fileName+".log")
	file, err := os.Open(logPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": true,
			"data":  "Unable to read deployment log: " + err.Error(),
		})
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	lines := []string{}
	// TODO: Find a way to pass io.reader to repsonse directly, to avoid copying
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	deployment, err := server.Clusters.GetCluster(fileName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"error": false,
			"data":  lines,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"error":      false,
		"data":       lines,
		"deployment": deployment,
		"state":      GetStateString(deployment.state),
	})
}

func (server *Server) getDeploymentLogs(c *gin.Context) (DeploymentLogs, error) {
	DeploymentLogs := DeploymentLogs{}

	server.Clusters.mutex.Lock()
	defer server.Clusters.mutex.Unlock()

	for _, cluster := range server.Clusters.Deployments {
		DeploymentLog := &DeploymentLog{
			DeploymentId: cluster.deploymentId,
			RunId:        cluster.runId,
			Status:       GetStateString(cluster.state),
			Create:       cluster.created,
		}
		DeploymentLogs = append(DeploymentLogs, DeploymentLog)
	}

	sort.Sort(DeploymentLogs)
	return DeploymentLogs, nil
}
