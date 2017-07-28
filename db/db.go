package db

import (
	"errors"
	"fmt"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"github.com/golang/glog"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/spf13/viper"
)

type ConfigDB struct {
	Url                    string
	User                   string
	Password               string
	Database               string
	ApplicationsCollection string
	BenchmarksCollection   string
}

type MetricsDB struct {
	Url                   string
	User                  string
	Password              string
	Database              string
	CalibrationCollection string
	ProfilingCollection   string
	SizingCollection      string
}

func NewConfigDB(config *viper.Viper) *ConfigDB {
	return &ConfigDB{
		Url:                    config.GetString("database.url"),
		User:                   config.GetString("database.user"),
		Password:               config.GetString("database.password"),
		Database:               config.GetString("database.configDatabase"),
		ApplicationsCollection: config.GetString("database.applicationCollection"),
		BenchmarksCollection:   config.GetString("database.benchmarkCollection"),
	}
}

func connectMongo(url string, database string, user string, password string) (*mgo.Session, error) {
	dialInfo := &mgo.DialInfo{
		Addrs:    []string{url},
		Database: database,
		Username: user,
		Password: password,
	}
	session, sessionErr := mgo.DialWithInfo(dialInfo)
	if sessionErr != nil {
		return nil, errors.New("Unable to create mongo session: " + sessionErr.Error())
	}

	return session, nil
}

func (configDb *ConfigDB) GetApplicationConfig(name string) (*models.ApplicationConfig, error) {
	session, sessionErr := connectMongo(configDb.Url, configDb.Database, configDb.User, configDb.Password)
	if sessionErr != nil {
		return nil, errors.New("Unable to create mongo session: " + sessionErr.Error())
	}
	glog.V(1).Infof("Successfully connected to the config DB for app %s", name)
	defer session.Close()

	collection := session.DB(configDb.Database).C(configDb.ApplicationsCollection)
	var appConfig models.ApplicationConfig
	if err := collection.Find(bson.M{"name": name}).One(&appConfig); err != nil {
		return nil, errors.New("Unable to find app config from db: " + err.Error())
	}

	return &appConfig, nil
}

func (configDb *ConfigDB) GetBenchmarks() ([]models.Benchmark, error) {
	session, sessionErr := connectMongo(configDb.Url, configDb.Database, configDb.User, configDb.Password)
	if sessionErr != nil {
		return nil, errors.New("Unable to create mongo session: " + sessionErr.Error())
	}
	defer session.Close()

	var benchmarks []models.Benchmark
	collection := session.DB(configDb.Database).C(configDb.BenchmarksCollection)
	if err := collection.Find(nil).All(&benchmarks); err != nil {
		return nil, errors.New("Unable to read benchmarks from config db: " + err.Error())
	}

	return benchmarks, nil
}

func NewMetricsDB(config *viper.Viper) *MetricsDB {
	return &MetricsDB{
		Url:                   config.GetString("database.url"),
		User:                  config.GetString("database.user"),
		Password:              config.GetString("database.password"),
		Database:              config.GetString("database.metricDatabase"),
		CalibrationCollection: config.GetString("database.calibrationCollection"),
		ProfilingCollection:   config.GetString("database.profilingCollection"),
		SizingCollection:      config.GetString("database.sizingCollection"),
	}
}

func (metricsDb *MetricsDB) getCollection(dataType string) (string, error) {
	switch dataType {
	case "calibration":
		return metricsDb.CalibrationCollection, nil
	case "profiling":
		return metricsDb.ProfilingCollection, nil
	case "sizing":
		return metricsDb.SizingCollection, nil
	default:
		return "", errors.New("Unable to find collection for: " + dataType)
	}
}

func (metricsDb *MetricsDB) WriteMetrics(dataType string, obj interface{}) error {
	collectionName, collectionErr := metricsDb.getCollection(dataType)
	if collectionErr != nil {
		return collectionErr
	}

	session, sessionErr := connectMongo(metricsDb.Url, metricsDb.Database, metricsDb.User, metricsDb.Password)
	if sessionErr != nil {
		return errors.New("Unable to create mongo session: " + sessionErr.Error())
	}

	defer session.Close()

	collection := session.DB(metricsDb.Database).C(collectionName)
	if err := collection.Insert(obj); err != nil {
		return errors.New("Unable to insert into collection: " + err.Error())
	}

	return nil
}

// TODO: Need to support use of filter when one collection contains multiple documents
func (metricsDb *MetricsDB) GetMetric(dataType string, appName string, metric interface{}) (interface{}, error) {
	collectionName, collectionErr := metricsDb.getCollection(dataType)
	if collectionErr != nil {
		return nil, collectionErr
	}

	session, sessionErr := connectMongo(metricsDb.Url, metricsDb.Database, metricsDb.User, metricsDb.Password)
	if sessionErr != nil {
		return nil, errors.New("Unable to create mongo session: " + sessionErr.Error())
	}

	defer session.Close()

	collection := session.DB(metricsDb.Database).C(collectionName)
	if err := collection.Find(bson.M{"appName": appName}).One(metric); err != nil {
		return nil, fmt.Errorf("Unable to read %s from metrics db: %s", dataType, err.Error())
	}

	return metric, nil
}
