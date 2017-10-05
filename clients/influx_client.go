package clients

import (
	"bytes"
	"fmt"
	"os/exec"
)

type InfluxClient struct {
	influxUrl        string
	influxPort       string
	influxBackupPort string
}

func (client *InfluxClient) BackupDB(key string) error {
	// Subprocess to run hyperpilot_influx.sh to backup and
	// and send backup to s3.

	cmd := exec.Command(
		"hyperpilot_influx.sh",
		"backup",
		fmt.Sprintf("--host=%s", client.influxUrl),
		fmt.Sprintf("--port=%s", client.influxPort),
		fmt.Sprintf("--backup-host=%s", fmt.Sprintf("%s:%s", client.influxUrl, client.influxBackupPort)),
		fmt.Sprintf("--name=%s", key),
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	fmt.Println(out.String())

	// /usr/local/bin/hyperpilot_influx.sh backup
	return err
}
