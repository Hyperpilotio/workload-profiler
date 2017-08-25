package main

import (
	"bufio"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hyperpilotio/workload-profiler/jobs"
	"fmt"
	"time"
)

type FileLogs []jobs.JobSummary

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

	run, err := server.JobManager.FindJob(fileName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": true,
			"data":  err,
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

	filterStatus := strings.ToUpper(c.Param("status"))
	fmt.Println("filterStatus:", filterStatus)

	switch filterStatus {
	case jobs.JOB_QUEUED:
		for _, job := range server.JobManager.GetJobs() {
			if job == nil {
				continue
			}
			fileLog := job.GetSummary()
			if fileLog.Status == jobs.JOB_QUEUED {
				fileLogs = append(fileLogs, job.GetSummary())
			}
		}
	case jobs.JOB_FINISHED:
		for _, job := range server.JobManager.GetJobs() {
			if job == nil {
				continue
			}
			fileLog := job.GetSummary()
			if fileLog.Status == jobs.JOB_FINISHED{
				fileLogs = append(fileLogs, job.GetSummary())
			}
		}
	case jobs.JOB_RESERVING, jobs.JOB_RUNNING:
		for _, job := range server.JobManager.GetJobs() {
			if job == nil {
				continue
			}
			fileLog := job.GetSummary()
			switch fileLog.Status {
			case jobs.JOB_RESERVING, jobs.JOB_RUNNING:
				fileLogs = append(fileLogs, job.GetSummary())
			}
		}
	case jobs.JOB_FAILED:
		for _, job := range server.JobManager.GetFailedJobs() {
			if job == nil {
				continue
			}
			fileLogs = append(fileLogs, job.GetSummary())
		}
	}

	sort.Sort(fileLogs)
	return fileLogs, nil
}
