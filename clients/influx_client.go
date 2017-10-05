package clients

import ()

type InfluxClient struct {
}

func (client *InfluxClient) BackupDB(influxUrl string) error {
	// Subprocess to run hyperpilot_influx.sh to backup and
	// and send backup to s3.
	// /usr/local/bin/hyperpilot_influx.sh backup
	return nil
}
