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

type FileLog struct {
	DeploymentId string
	RunId        string
	Status       string
	Create       time.Time
}

type FileLogs []*FileLog

func (d FileLogs) Len() int { return len(d) }
func (d FileLogs) Less(i, j int) bool {
	return d[i].Create.Before(d[j].Create)
}
func (d FileLogs) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

func (server *Server) logUI(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"error": false,
	})
}

func (server *Server) getFileLogList(c *gin.Context) {
	fileLogs, err := server.getFileLogs(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"error": true,
			"data":  "",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"error": false,
		"data":  fileLogs,
	})
}

func (server *Server) getFileLogContent(c *gin.Context) {
	fileName := c.Param("fileName")

	server.mutex.Lock()
	run, ok := server.Jobs[fileName]
	server.mutex.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": true,
			"data":  "Unable to find run: " + fileName,
		})
		return
	}

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

	c.JSON(http.StatusOK, gin.H{
		"error":      false,
		"data":       lines,
		"deployment": run.GetSummary(),
		"state":      run.GetState(),
	})
}

func (server *Server) getFileLogs(c *gin.Context) (FileLogs, error) {
	fileLogs := FileLogs{}

	server.Clusters.mutex.Lock()
	defer server.Clusters.mutex.Unlock()

	filterStatus := c.Param("status")
	for _, cluster := range server.Clusters.Deployments {
		fileLog := &FileLog{
			DeploymentId: cluster.deploymentId,
			RunId:        cluster.runId,
			Status:       GetStateString(cluster.state),
			Create:       cluster.created,
		}

		switch filterStatus {
		case "Failed":
			if fileLog.Status == "Failed" {
				fileLogs = append(fileLogs, fileLog)
			}
		case "Running":
			if fileLog.Status != "Failed" {
				fileLogs = append(fileLogs, fileLog)
			}
		}
	}

	sort.Sort(fileLogs)
	return fileLogs, nil
}
