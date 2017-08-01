package main

import (
	"flag"
	"os"
	"time"

	"github.com/go-resty/resty"
	"github.com/golang/glog"
	"github.com/spf13/viper"
)

// Run start the web server
func Run(fileConfig string) error {
	viper := viper.New()
	viper.SetConfigType("json")

	if fileConfig == "" {
		viper.SetConfigName("config")
		viper.AddConfigPath("/etc/workload-profiler")
		viper.Set("awsId", os.Getenv("AWS_ACCESS_KEY_ID"))
		viper.Set("awsSecret", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	} else {
		viper.SetConfigFile(fileConfig)
	}

	viper.SetDefault("port", "7779")

	err := viper.ReadInConfig()
	if err != nil {
		return err
	}

	resty.SetTimeout(time.Duration(3 * time.Minute))

	server := NewServer(viper)
	return server.StartServer()
}

func main() {
	configPath := flag.String("config", "", "The file path to a config file")
	flag.Parse()

	err := Run(*configPath)
	glog.Errorln(err)
}
