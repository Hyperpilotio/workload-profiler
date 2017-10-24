package clients

import (
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
	err := cmd.Run()
	return err
}
