package clients

import (
	"bytes"
	"fmt"
	"os/exec"
)

type InfluxClient struct {
	Url        string
	Port       int
	BackupPort int
}

func NewInfluxClient(url string, port int, backupPort int) *InfluxClient {
	return &InfluxClient{
		Url:        url,
		Port:       port,
		BackupPort: backupPort,
	}
}

func (client *InfluxClient) BackupDB(key string) error {
	// Subprocess to run hyperpilot_influx.sh to backup and
	// and send backup to s3.
	var stderrBuf bytes.Buffer
	cmd := exec.Command(
		"/opt/workload-profiler/hyperpilot_influx.sh",
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
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Unable to run backup command, err: %s, stderr: %s", err.Error(), stderrBuf.String())
	}

	return nil
}
