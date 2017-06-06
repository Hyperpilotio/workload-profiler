package main

import (
	"errors"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"github.com/golang/glog"
	"github.com/spf13/viper"
)

type ConfigDB struct {
	Url                    string
	User                   string
	Password               string
	Database               string
	ApplicationsCollection string
}

type MetricsDB struct {
	Url                   string
	User                  string
	Password              string
	Database              string
	CalibrationCollection string
}

func NewConfigDB(config *viper.Viper) *ConfigDB {
	return &ConfigDB{
		Url:                    config.GetString("database.url"),
		User:                   config.GetString("database.user"),
		Password:               config.GetString("database.password"),
		Database:               config.GetString("database.configDatabase"),
		ApplicationsCollection: config.GetString("database.applicationCollection"),
	}
}

func (configDb *ConfigDB) GetApplicationConfig(name string) (*ApplicationConfig, error) {
	session, sessionErr := mgo.Dial(configDb.Url)
	if sessionErr != nil {
		return nil, errors.New("Unable to create mongo session: " + sessionErr.Error())
	}
	defer session.Close()

	collection := session.DB(configDb.Database).C(configDb.ApplicationsCollection)
	var appConfig ApplicationConfig
	if err := collection.Find(bson.M{"name": name}).One(&appConfig); err != nil {
		return nil, errors.New("Unable to find app config from db: " + err.Error())
	}

	return &appConfig, nil
}

func NewMetricsDB(config *viper.Viper) *MetricsDB {
	return &MetricsDB{
		Url:                   config.GetString("database.url"),
		User:                  config.GetString("database.user"),
		Password:              config.GetString("database.password"),
		Database:              config.GetString("database.metricsDatabase"),
		CalibrationCollection: config.GetString("database.calibrationCollection"),
	}
}

func (metricsDb *MetricsDB) getCollection(dataType string) (string, error) {
	switch dataType {
	case "calibration":
		return metricsDb.CalibrationCollection, nil
	default:
		return "", errors.New("Unable to find collection for: " + dataType)
	}
}

func (metricsDb *MetricsDB) WriteMetrics(dataType string, obj interface{}) error {
	collectionName, collectionErr := metricsDb.getCollection(dataType)
	if collectionErr != nil {
		return collectionErr
	}

	glog.Info("Storing calibration results to database")
	session, sessionErr := mgo.Dial(metricsDb.Url)
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
