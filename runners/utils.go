package runners

import (
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

func replaceTargetingServiceAddress(
	controller *models.BenchmarkController,
	deployerClient *clients.DeployerClient,
	deploymentId string,
	log *logging.Logger) error {
	if controller.Initialize != nil && controller.Initialize.ServiceConfigs != nil {
		for _, targetingService := range *controller.Initialize.ServiceConfigs {
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
				controller.Initialize.Args = append(
					[]string{
						targetingService.PortConfig.Arg,
						strconv.FormatInt(serviceAddress.Port, 10),
					},
					controller.Initialize.Args...)
			}
			if targetingService.HostConfig != nil {
				controller.Initialize.Args = append(
					[]string{
						targetingService.HostConfig.Arg,
						serviceAddress.Host,
					},
					controller.Initialize.Args...)
			}
			log.Infof("Arguments of Initialize command are %s", controller.Initialize.Args)
		}
	}

	if controller.Command.ServiceConfigs != nil {
		for _, targetingService := range *controller.Command.ServiceConfigs {
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name, log)
			if err != nil {
				return fmt.Errorf(
					"Unable to get service %s address: %s",
					targetingService.Name,
					err.Error())
			}

			// LoadTesterCommand
			if targetingService.PortConfig != nil {
				controller.Command.Args = append(
					[]string{
						targetingService.PortConfig.Arg,
						strconv.FormatInt(serviceAddress.Port, 10),
					},
					controller.Command.Args...)
			}
			if targetingService.HostConfig != nil {
				controller.Command.Args = append(
					[]string{
						targetingService.HostConfig.Arg,
						serviceAddress.Host,
					},
					controller.Command.Args...)
			}

			log.Infof("Arguments of load testing command are %s", controller.Command.Args)
		}
	}

	return nil
}
