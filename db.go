package main

import (
	"errors"

	"gopkg.in/mgo.v2"

	"github.com/golang/glog"
	"github.com/spf13/viper"
)

func PersistData(config *viper.Viper, name string, obj interface{}) error {
	if !config.GetBool("writeResults") {
		glog.Info("Skip writing profile results to database")
		return nil
	}

	glog.Info("Storing profile results to database")
	url := config.GetString("database.url")
	databaseName := config.GetString("database.database")
	session, sessionErr := mgo.Dial(url)
	if sessionErr != nil {
		return errors.New("Unable to create mongo session: " + sessionErr.Error())
	}

	defer session.Close()

	collection := session.DB(databaseName).C(name)
	if err := collection.Insert(obj); err != nil {
		return errors.New("Unable to insert into collection: " + err.Error())
	}

	return nil
}
