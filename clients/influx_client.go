package clients

import (
	"bytes"
	"fmt"
	"os/exec"
)

type InfluxClient struct {
	influxUrl        string
	influxPort       string
	influxBackupUrl  string
	influxBackupPort string
}

func (client *InfluxClient) BackupDB(key string) error {
	// Subprocess to run hyperpilot_influx.sh to backup and
	// and send backup to s3.

	cmd := exec.Command(
		"hyperpilot_influx.sh",
		"-o",
		"backup",
		"-h",
		fmt.Sprintf("%s:%s", client.influxUrl, client.influxPort),
		"-b",
		fmt.Sprintf("%s:%s", client.influxBackupUrl, client.influxBackupPort),
		"-n",
		key,
	)
	err := cmd.Run()
	return err
}
