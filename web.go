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
	Name   string
	Status string
	Create time.Time
}

type FileLogs []*FileLog

func (d FileLogs) Len() int { return len(d) }
func (d FileLogs) Less(i, j int) bool {
	return d[i].Create.Before(d[j].Create)
}
func (d FileLogs) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

func (server *Server) logUI(c *gin.Context) {
	FileLogs, _ := server.getFileLogs(c)
	c.HTML(http.StatusOK, "index.html", gin.H{
		"error": false,
		"logs":  FileLogs,
	})
}

func (server *Server) getFileLogContent(c *gin.Context) {
	logFile := c.Param("logFile")
	logPath := path.Join(server.Config.GetString("filesPath"), "log", logFile+".log")
	file, err := os.Open(logPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": true,
			"data":  "Unable to read File log: " + err.Error(),
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
		"error": false,
		"data":  lines,
		"state": GetStateString(server.Clusters.GetState(logFile)),
	})
}

func (server *Server) getFileLogs(c *gin.Context) (FileLogs, error) {
	FileLogs := FileLogs{}

	server.Clusters.mutex.Lock()
	defer server.Clusters.mutex.Unlock()

	for _, cluster := range server.Clusters.Deployments {
		FileLog := &FileLog{
			Name:   cluster.runId,
			Status: GetStateString(cluster.state),
			Create: cluster.created,
		}
		FileLogs = append(FileLogs, FileLog)
	}

	sort.Sort(FileLogs)
	return FileLogs, nil
}
