package clients

import (
	"fmt"
	"testing"
)

func TestBackupDB(t *testing.T) {
	fmt.Println("start testing backup")
	client := &InfluxClient{
		influxUrl:        "localhost",
		influxPort:       "8086",
		influxBackupPort: "8088",
	}

	if err := client.BackupDB("Test"); err != nil {
		fmt.Println(err)
		t.Error(err)
	}

}
