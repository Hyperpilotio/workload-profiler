package runners

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/nu7hatch/gouuid"
	logging "github.com/op/go-logging"
)

func generateId(prefix string) (string, error) {
	u4, err := uuid.NewV4()
	if err != nil {
		return "", errors.New("Unable to generate stage id: " + err.Error())
	}
	return prefix + "-" + u4.String(), nil
}

func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
	}
}

func deepCopy(from interface{}, to interface{}) error {
	if from == nil {
		return errors.New("Unable to find 'from' interface")
	}
	if to == nil {
		return errors.New("Unable to find 'to' interface")
	}
	bytes, err := json.Marshal(from)
	if err != nil {
		return fmt.Errorf("Unable to marshal src: %s", err)
	}
	err = json.Unmarshal(bytes, to)
	if err != nil {
		return fmt.Errorf("Unable to unmarshal into dst: %s", err)
	}
	return nil
}

func addCommandParameter(parameter *models.CommandParameter, args []string, value string) []string {
	if parameter.Arg != "" {
		args = append([]string{parameter.Arg, value}, args...)
	} else {
		args = append(args, "")
		i := parameter.Position
		copy(args[i+1:], args[i:])
		args[i] = value
	}

	return args
}

func replaceTargetingServiceAddress(
	newController *models.BenchmarkController,
	deployerClient *clients.DeployerClient,
	deploymentId string,
	log *logging.Logger) error {
	if newController.Initialize != nil && newController.Initialize.ServiceConfigs != nil {
		for _, targetingService := range *newController.Initialize.ServiceConfigs {
			// NOTE we assume the targeting service is an unique one in this deployment process.
			// As a result, we should use GetServiceAddress function instead of GetColocatedServiceUrl
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name, log)
			if err != nil {
				return fmt.Errorf(
					"Unable to get service %s address: %s",
					targetingService.Name,
					err.Error())
			}
			// Initialize
			if targetingService.PortConfig != nil {
				newController.Initialize.Args = addCommandParameter(
					targetingService.PortConfig,
					newController.Initialize.Args,
					strconv.FormatInt(serviceAddress.Port, 10))
			}

			if targetingService.HostConfig != nil {
				newController.Initialize.Args = addCommandParameter(
					targetingService.HostConfig,
					newController.Initialize.Args,
					serviceAddress.Host)
			}
			log.Infof("Arguments of Initialize command are %s", newController.Initialize.Args)
		}
	}

	if newController.Command.ServiceConfigs != nil {
		for _, targetingService := range *newController.Command.ServiceConfigs {
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name, log)
			if err != nil {
				return fmt.Errorf(
					"Unable to get service %s address: %s",
					targetingService.Name,
					err.Error())
			}

			// LoadTesterCommand
			if targetingService.PortConfig != nil {
				newController.Command.Args = addCommandParameter(
					targetingService.PortConfig,
					newController.Command.Args,
					strconv.FormatInt(serviceAddress.Port, 10))
			}

			if targetingService.HostConfig != nil {
				newController.Command.Args = addCommandParameter(
					targetingService.HostConfig,
					newController.Command.Args,
					serviceAddress.Host)
			}

			log.Infof("Arguments of load testing command are %s", newController.Command.Args)
		}
	}

	return nil
}
