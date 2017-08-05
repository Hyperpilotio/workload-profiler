package runners

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/golang/glog"
	"github.com/hyperpilotio/workload-profiler/clients"
	"github.com/hyperpilotio/workload-profiler/models"
	"github.com/nu7hatch/gouuid"
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

func replaceTargetingServiceAddress(controller *models.BenchmarkController, deployerClient *clients.DeployerClient, deploymentId string) error {
	var serviceConfigs []models.ServiceConfig
	if controller.Initialize.ServiceConfigs != nil {
		serviceConfigs = *controller.Initialize.ServiceConfigs
	} else if controller.Command.ServiceConfigs != nil {
		serviceConfigs = *controller.Command.ServiceConfigs
	}
	if serviceConfigs != nil {
		for _, targetingService := range serviceConfigs {
			// NOTE we assume the targeting service is an unique one in this deployment process.
			// As a result, we should use GetServiceAddress function instead of GetColocatedServiceUrl
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name)
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
			glog.V(2).Infof("Arguments of Initialize command are %s", controller.Initialize.Args)
		}
	}

	glog.V(3).Infof("func replaceTargetingServiceAddress: Command %+v", controller.Command)
	if controller.Command.ServiceConfigs != nil {
		for _, targetingService := range *controller.Command.ServiceConfigs {
			serviceAddress, err := deployerClient.GetServiceAddress(deploymentId, targetingService.Name)
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

			glog.V(2).Infof("Arguments of load testing command are %s", controller.Command.Args)
		}
	}

	return nil
}
