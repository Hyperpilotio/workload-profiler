package main

import (
	"bufio"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hyperpilotio/workload-profiler/jobs"
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

	server.mutex.Lock()
	defer server.mutex.Unlock()

	profilerLog, ok := server.ProfilerLogs[fileName]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": true,
			"data":  "",
		})
		return
	}

	scanner := bufio.NewScanner(profilerLog.LogFile)
	scanner.Split(bufio.ScanLines)

	lines := []string{}
	// TODO: Find a way to pass io.reader to repsonse directly, to avoid copying
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	job, err := server.JobManager.FindJob(fileName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"error": false,
			"data":  lines,
			"state": jobs.JOB_FAILED,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"error":      false,
		"data":       lines,
		"deployment": job.GetSummary(),
		"state":      job.GetState(),
	})
}

func (server *Server) getFileLogs(c *gin.Context) (FileLogs, error) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	fileLogs := FileLogs{}

	filterStatus := c.Param("status")
	for runId, profilerLog := range server.ProfilerLogs {
		job, err := server.JobManager.FindJob(runId)
		if err != nil {
			fileInfo, _ := profilerLog.LogFile.Stat()
			fileLogs = append(fileLogs, jobs.JobSummary{
				RunId:  runId,
				Status: jobs.JOB_FAILED,
				Create: fileInfo.ModTime(),
			})
			continue
		}
		fileLog := job.GetSummary()
		if strings.ToUpper(filterStatus) == fileLog.Status {
			fileLogs = append(fileLogs, fileLog)
		}
	}

	sort.Sort(fileLogs)
	return fileLogs, nil
}
