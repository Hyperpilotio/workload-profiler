package clients

import (
	"bytes"
	"fmt"
	"os/exec"
)

type InfluxClient struct {
	ScriptPath string
	Url        string
	Port       int
	BackupPort int
}

func NewInfluxClient(scriptPath string, url string, port int, backupPort int) *InfluxClient {
	if scriptPath == "" {
		scriptPath = "/opt/workload-profiler/hyperpilot_influx.sh"
	}

	return &InfluxClient{
		ScriptPath: scriptPath,
		Url:        url,
		Port:       port,
		BackupPort: backupPort,
	}
}

func (client *InfluxClient) BackupDB(key string) error {
	// Subprocess to run hyperpilot_influx.sh to backup and
	// and send backup to s3.
	var stderrBuf bytes.Buffer
	var stdoutBuf bytes.Buffer
	cmd := exec.Command(
		client.ScriptPath,
		"-o",
		"backup",
		"-h",
		fmt.Sprintf("%s:%d", client.Url, client.Port),
		"-b",
		fmt.Sprintf("%s:%d", client.Url, client.BackupPort),
		"-n",
		key,
		"-p",
		"8086",
	)
	cmd.Stderr = &stderrBuf
	cmd.Stdout = &stdoutBuf
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Unable to run backup command, err: %s, stderr: %s", err.Error(), stderrBuf.String())
	}

	return nil
}
