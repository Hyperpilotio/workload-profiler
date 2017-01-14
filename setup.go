package main

/*
import (
	"encoding/json"
	"github.com/go-resty/resty"
	"github.com/spf13/viper"
	"io/ioutil"
)
*/

// call deployer for deployment Result
type SetupResult struct {
	Data  string `json:"data"`
	Error bool   `json:"error"`
}

/*
func SetupDeployer(config *viper.Viper, profile Profile) SetupResult {
	deployData, fileErr := ioutil.ReadFile("config/" + profile.Setup.Deployer + "-deploy.json")
	if fileErr != nil {
		return SetupResult{"Error deserializing deployer deploy.json: " + string(fileErr.Error()), true}
	}

	resp, postErr := resty.R().
		SetHeader("Content-Type", "application/json").
		SetBody(string(deployData)).
		Post(config.GetString("deployerUrl"))

	if postErr != nil {
		return SetupResult{"Error deserializing deployer: " + string(postErr.Error()), true}
	}

	var sr SetupResult
	unmarshalErr := json.Unmarshal(resp.Body(), &sr)

	if unmarshalErr != nil {
		return SetupResult{"Error Unmarshal SetupResult: " + string(unmarshalErr.Error()), true}
	}
	return sr
}
*/
