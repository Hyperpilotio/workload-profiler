package clients

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type InfluxClient struct {
	ScriptPath string
	Url        string
	Port       int
	BackupPort int
}

func NewInfluxClient(scriptPath string, url string, port int, backupPort int) (*InfluxClient, error) {
	if scriptPath == "" {
		scriptPath = "/usr/local/bin/hyperpilot_influx.sh"
	}

	// Remove scheme and port from url
	parts := strings.Split(strings.TrimPrefix(url, "http://"), ":")
	if len(parts) > 2 || len(parts) == 0 {
		return nil, errors.New("Unexpected url format: " + url)
	}

	url = parts[0]

	return &InfluxClient{
		ScriptPath: scriptPath,
		Url:        url,
		Port:       port,
		BackupPort: backupPort,
	}, nil
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
		fmt.Sprintf("%s", client.Url),
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
